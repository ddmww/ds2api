package promptcompat

import (
	"encoding/json"
	"fmt"
	"strings"

	"ds2api/internal/prompt"
)

func FlattenOpenAIMessagesForGrok(messages []map[string]any) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		role, _ := msg["role"].(string)
		role = strings.ToLower(strings.TrimSpace(role))
		content := msg["content"]

		switch role {
		case "tool", "function":
			toolCallID := strings.TrimSpace(asString(msg["tool_call_id"]))
			if toolCallID == "" {
				toolCallID = strings.TrimSpace(asString(msg["name"]))
			}
			label := "[tool result"
			if toolCallID != "" {
				label += " for " + toolCallID
			}
			label += "]:"
			parts = appendNonEmptyPromptPart(parts, label+"\n"+stringifyOpenAIContent(content))
			continue
		case "assistant":
			if toolCalls := grokToolCallsToXML(msg["tool_calls"]); toolCalls != "" {
				parts = appendNonEmptyPromptPart(parts, "[assistant]:\n"+toolCalls)
				continue
			}
		}

		text := stringifyOpenAIContent(content)
		if role == "" {
			role = "user"
		}
		parts = appendNonEmptyPromptPart(parts, fmt.Sprintf("[%s]: %s", role, text))
	}
	return strings.Join(parts, "\n\n")
}

func appendNonEmptyPromptPart(parts []string, part string) []string {
	if strings.TrimSpace(part) == "" {
		return parts
	}
	return append(parts, part)
}

func stringifyOpenAIContent(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []map[string]any:
		asAny := make([]any, 0, len(typed))
		for _, item := range typed {
			asAny = append(asAny, item)
		}
		return stringifyOpenAIContent(asAny)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			mapped, ok := item.(map[string]any)
			if !ok {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(asString(mapped["type"]))) {
			case "text", "input_text", "output_text":
				if text := asString(mapped["text"]); text != "" {
					parts = append(parts, text)
				}
			case "image_url":
				if imageURL, ok := mapped["image_url"].(map[string]any); ok {
					if url := asString(imageURL["url"]); url != "" {
						parts = append(parts, url)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return prompt.NormalizeContent(content)
	}
}

func grokToolCallsToXML(raw any) string {
	items := normalizeToolCallsForGrok(raw)
	if len(items) == 0 {
		return ""
	}
	lines := []string{"<tool_calls>"}
	for _, tool := range items {
		function, _ := tool["function"].(map[string]any)
		name := strings.TrimSpace(asString(function["name"]))
		if name == "" {
			name = strings.TrimSpace(asString(tool["name"]))
		}
		if name == "" {
			continue
		}
		arguments := "{}"
		if rawArgs, ok := function["arguments"].(string); ok {
			if normalized := normalizeGrokJSON(rawArgs); normalized != "" {
				arguments = normalized
			} else if strings.TrimSpace(rawArgs) != "" {
				arguments = strings.TrimSpace(rawArgs)
			}
		} else if value := function["arguments"]; value != nil {
			if data, err := json.Marshal(value); err == nil {
				arguments = string(data)
			}
		} else if value := tool["arguments"]; value != nil {
			if data, err := json.Marshal(value); err == nil {
				arguments = string(data)
			}
		}
		lines = append(lines,
			"  <tool_call>",
			"    <tool_name>"+name+"</tool_name>",
			"    <parameters>"+arguments+"</parameters>",
			"  </tool_call>",
		)
	}
	if len(lines) == 1 {
		return ""
	}
	lines = append(lines, "</tool_calls>")
	return strings.Join(lines, "\n")
}

func normalizeToolCallsForGrok(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if mapped, ok := item.(map[string]any); ok {
				out = append(out, mapped)
			}
		}
		return out
	default:
		return nil
	}
}

func normalizeGrokJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}
