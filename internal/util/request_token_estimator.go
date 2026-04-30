package util

import (
	"fmt"
	"strings"
)

const (
	openAIMessageTokenOverhead = 3
	openAINameTokenOverhead    = 3
	openAIToolsTokenOverhead   = 8
	openAIReplyTokenOverhead   = 3

	estimatedImageTokens = 520
	estimatedAudioTokens = 256
	estimatedVideoTokens = 8192
	estimatedFileTokens  = 4096
)

type requestTokenMeta struct {
	texts       []string
	messages    int
	names       int
	tools       int
	mediaTokens int
}

// EstimateOpenAIRequestTokensByModel mirrors NewAPI's OpenAI request token
// accounting: text is counted as a combined transcript, then message/name/tool
// framing and media placeholders are added separately.
func EstimateOpenAIRequestTokensByModel(modelName string, messages []any, toolsRaw any) int {
	meta := requestTokenMeta{}
	for _, raw := range messages {
		meta.addMessage(raw)
	}
	meta.addTools(toAnySlice(toolsRaw))

	total := EstimateTokensByModel(modelName, strings.Join(meta.texts, "\n"))
	if meta.messages > 0 || meta.tools > 0 {
		total += meta.tools * openAIToolsTokenOverhead
		total += meta.messages * openAIMessageTokenOverhead
		total += meta.names * openAINameTokenOverhead
		total += openAIReplyTokenOverhead
	}
	total += meta.mediaTokens
	return total
}

func EstimateOpenAIRequestTokensWithFallback(modelName string, messages []any, toolsRaw any, fallbackPrompt string) int {
	if tokens := EstimateOpenAIRequestTokensByModel(modelName, messages, toolsRaw); tokens > 0 {
		return tokens
	}
	return EstimateTokensByModel(modelName, fallbackPrompt)
}

func (m *requestTokenMeta) addMessage(raw any) {
	msg, ok := raw.(map[string]any)
	if !ok {
		if text := strings.TrimSpace(fmt.Sprint(raw)); text != "" {
			m.texts = append(m.texts, text)
		}
		return
	}
	m.messages++
	m.addText(tokenMetaString(msg["role"]))
	if name := tokenMetaString(msg["name"]); name != "" {
		m.names++
		m.addText(name)
	}
	m.addContent(msg["content"])
}

func (m *requestTokenMeta) addContent(raw any) {
	switch v := raw.(type) {
	case nil:
		return
	case string:
		m.addText(v)
	case []any:
		for _, item := range v {
			m.addContentPart(item)
		}
	case []map[string]any:
		for _, item := range v {
			m.addContentPart(item)
		}
	case map[string]any:
		m.addContentPart(v)
	default:
		m.addText(fmt.Sprint(v))
	}
}

func (m *requestTokenMeta) addContentPart(raw any) {
	part, ok := raw.(map[string]any)
	if !ok {
		m.addText(fmt.Sprint(raw))
		return
	}
	partType := strings.ToLower(strings.TrimSpace(tokenMetaString(part["type"])))
	switch partType {
	case "text", "input_text", "output_text":
		m.addText(tokenMetaString(part["text"]))
	case "image", "image_url", "input_image":
		m.mediaTokens += estimatedImageTokens
		m.addText(tokenMetaString(part["text"]))
	case "input_audio", "audio", "audio_url":
		m.mediaTokens += estimatedAudioTokens
	case "input_video", "video", "video_url":
		m.mediaTokens += estimatedVideoTokens
	case "file", "input_file":
		m.mediaTokens += estimatedFileTokens
		m.addText(tokenMetaString(part["filename"]))
	default:
		if text := tokenMetaString(part["text"]); text != "" {
			m.addText(text)
			return
		}
		if partType != "" && strings.Contains(partType, "image") {
			m.mediaTokens += estimatedImageTokens
		}
	}
}

func (m *requestTokenMeta) addTools(tools []any) {
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		m.tools++
		fn := toMapValue(tool, "function")
		if len(fn) == 0 {
			fn = tool
		}
		m.addText(tokenMetaString(fn["name"]))
		m.addText(tokenMetaString(fn["description"]))
		if params, ok := fn["parameters"]; ok {
			m.addText(fmt.Sprint(params))
		}
	}
}

func (m *requestTokenMeta) addText(text string) {
	text = strings.TrimSpace(text)
	if text != "" {
		m.texts = append(m.texts, text)
	}
}

func tokenMetaString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toAnySlice(v any) []any {
	switch items := v.(type) {
	case []any:
		return items
	case []map[string]any:
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func toMapValue(v any, key string) map[string]any {
	if m, ok := v.(map[string]any); ok {
		if key == "" {
			return m
		}
		if child, ok := m[key].(map[string]any); ok {
			return child
		}
	}
	return nil
}
