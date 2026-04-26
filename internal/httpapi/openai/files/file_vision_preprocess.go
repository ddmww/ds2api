package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/httpapi/openai/shared"
)

const defaultVisionPrompt = "Please describe the attached images in detail. If they contain code, UI elements, or error messages, explicitly write them out."

type visionImageInput struct {
	URL      string
	Fallback string
}

func (h *Handler) preprocessVisionInputs(ctx context.Context, req map[string]any) error {
	if h == nil || h.Store == nil || len(req) == 0 {
		return nil
	}
	visionCfg := h.Store.VisionConfig()
	if !visionCfg.Enabled {
		return nil
	}
	for _, key := range []string{"messages", "input"} {
		raw, ok := req[key]
		if !ok {
			continue
		}
		updated, changed, err := h.applyVisionToLastUserMessage(ctx, raw, visionCfg)
		if err != nil {
			return err
		}
		if changed {
			req[key] = updated
		}
	}
	delete(req, "ref_file_ids")
	return nil
}

func (h *Handler) applyVisionToLastUserMessage(ctx context.Context, raw any, visionCfg config.VisionConfig) (any, bool, error) {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return raw, false, nil
	}
	lastUserIdx := -1
	for i := len(items) - 1; i >= 0; i-- {
		msg, ok := items[i].(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(shared.AsString(msg["role"])), "user") {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		return raw, false, nil
	}
	msg, _ := items[lastUserIdx].(map[string]any)
	content, ok := msg["content"].([]any)
	if !ok || len(content) == 0 {
		return raw, false, nil
	}
	textBlocks := make([]any, 0, len(content)+1)
	images := make([]visionImageInput, 0, 4)
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			textBlocks = append(textBlocks, item)
			continue
		}
		img, isImage, fallback, err := decodeVisionImageBlock(block)
		if err != nil {
			return raw, false, err
		}
		if !isImage {
			textBlocks = append(textBlocks, item)
			continue
		}
		if fallback != "" {
			textBlocks = append(textBlocks, map[string]any{
				"type": "input_text",
				"text": fallback,
			})
			continue
		}
		images = append(images, img)
	}
	if len(images) == 0 {
		return raw, false, nil
	}
	description, err := callVisionChatCompletion(ctx, visionCfg, images)
	if err != nil {
		textBlocks = append(textBlocks, map[string]any{
			"type": "input_text",
			"text": fmt.Sprintf("\n\n[System: The user attached image(s), but the Vision interceptor failed to process them. Error: %s]\n\n", err.Error()),
		})
	} else {
		textBlocks = append(textBlocks, map[string]any{
			"type": "input_text",
			"text": fmt.Sprintf("\n\n[System: The user attached %d image(s). Visual analysis/OCR extracted the following context:\n%s]\n\n", len(images), strings.TrimSpace(description)),
		})
	}
	msg["content"] = textBlocks
	items[lastUserIdx] = msg
	return items, true, nil
}

func decodeVisionImageBlock(block map[string]any) (visionImageInput, bool, string, error) {
	blockType := strings.ToLower(strings.TrimSpace(shared.AsString(block["type"])))
	switch blockType {
	case "image_url", "input_image":
		if imageURL, ok := block["image_url"].(map[string]any); ok {
			return resolveVisionImageURL(shared.AsString(imageURL["url"]), contentTypeFromMap(imageURL))
		}
		return resolveVisionImageURL(shared.AsString(block["url"]), contentTypeFromMap(block))
	case "image":
		if source, ok := block["source"].(map[string]any); ok {
			if strings.EqualFold(strings.TrimSpace(shared.AsString(source["type"])), "base64") {
				mediaType := contentTypeFromMap(source)
				if mediaType == "" {
					mediaType = "image/jpeg"
				}
				raw := strings.TrimSpace(shared.AsString(source["data"]))
				if raw == "" {
					return visionImageInput{}, true, "", fmt.Errorf("image source missing data")
				}
				return visionImageInput{URL: "data:" + mediaType + ";base64," + raw}, true, "", nil
			}
			rawSource := shared.AsString(source["url"])
			if strings.TrimSpace(rawSource) == "" {
				rawSource = shared.AsString(source["data"])
			}
			return resolveVisionImageURL(rawSource, contentTypeFromMap(source))
		}
	case "image_file":
		fileID := strings.TrimSpace(shared.AsString(block["file_id"]))
		if fileID == "" {
			if imageFile, ok := block["image_file"].(map[string]any); ok {
				fileID = strings.TrimSpace(shared.AsString(imageFile["file_id"]))
			}
		}
		if fileID != "" {
			return visionImageInput{}, true, fmt.Sprintf("[Image file attached: %s. This proxy cannot inspect referenced OpenAI file IDs in vision preprocessing.]", fileID), nil
		}
	}
	return visionImageInput{}, false, "", nil
}

func resolveVisionImageURL(rawURL string, explicitContentType string) (visionImageInput, bool, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return visionImageInput{}, true, "", fmt.Errorf("image input missing url")
	}
	if isDataURL(rawURL) {
		return visionImageInput{URL: rawURL}, true, "", nil
	}
	lowerURL := strings.ToLower(rawURL)
	if strings.HasPrefix(lowerURL, "http://") || strings.HasPrefix(lowerURL, "https://") {
		if looksLikeSVG(rawURL, explicitContentType) {
			return visionImageInput{}, true, fmt.Sprintf("[SVG vector image from URL: %s. SVG images cannot be processed by the configured vision model.]", rawURL), nil
		}
		return visionImageInput{URL: rawURL}, true, "", nil
	}
	localPath := rawURL
	if strings.HasPrefix(lowerURL, "file:///") {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return visionImageInput{}, true, "", err
		}
		localPath = filepath.FromSlash(strings.TrimPrefix(parsed.Path, "/"))
		if parsed.Host != "" {
			localPath = parsed.Host + localPath
		}
	}
	dataURL, fallback, err := localImagePathToDataURL(localPath, explicitContentType)
	if err != nil {
		return visionImageInput{}, true, "", err
	}
	return visionImageInput{URL: dataURL, Fallback: fallback}, true, fallback, nil
}

func localImagePathToDataURL(pathValue string, explicitContentType string) (string, string, error) {
	pathValue = strings.TrimSpace(pathValue)
	if pathValue == "" {
		return "", "", fmt.Errorf("empty local image path")
	}
	resolved := pathValue
	if strings.HasPrefix(pathValue, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		resolved = filepath.Join(home, pathValue[2:])
	}
	resolved = filepath.Clean(resolved)
	contentType := strings.TrimSpace(explicitContentType)
	if contentType == "" {
		contentType = mime.TypeByExtension(strings.ToLower(filepath.Ext(resolved)))
	}
	if looksLikeSVG(resolved, contentType) {
		return "", fmt.Sprintf("[SVG vector image attached: %s. SVG images cannot be processed by the configured vision model.]", filepath.Base(resolved)), nil
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", "", err
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), "", nil
}

func looksLikeSVG(value string, contentType string) bool {
	if strings.EqualFold(strings.TrimSpace(contentType), "image/svg+xml") {
		return true
	}
	return strings.HasSuffix(strings.ToLower(strings.TrimSpace(value)), ".svg")
}

func callVisionChatCompletion(ctx context.Context, visionCfg config.VisionConfig, images []visionImageInput) (string, error) {
	prompt := strings.TrimSpace(visionCfg.Prompt)
	if prompt == "" {
		prompt = defaultVisionPrompt
	}
	content := make([]map[string]any, 0, len(images)+1)
	content = append(content, map[string]any{
		"type": "text",
		"text": prompt,
	})
	for _, image := range images {
		if strings.TrimSpace(image.URL) == "" {
			continue
		}
		content = append(content, map[string]any{
			"type":      "image_url",
			"image_url": map[string]any{"url": image.URL},
		})
	}
	body := map[string]any{
		"model": visionCfg.Model,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": content,
			},
		},
		"max_tokens": 1500,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	reqCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, strings.TrimSpace(visionCfg.BaseURL), bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(visionCfg.APIKey))
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("vision api returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
	var parsed map[string]any
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", err
	}
	if choices, ok := parsed["choices"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]any); ok {
			if msg, ok := choice["message"].(map[string]any); ok {
				if text := extractVisionMessageContent(msg["content"]); text != "" {
					return text, nil
				}
			}
		}
	}
	return "", fmt.Errorf("vision api response missing message content")
}

func extractVisionMessageContent(raw any) string {
	switch x := raw.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(shared.AsString(block["type"])), "text") {
				if text := strings.TrimSpace(shared.AsString(block["text"])); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	default:
		return ""
	}
}
