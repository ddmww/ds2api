package promptcompat

import (
	"strings"
)

func buildOpenAIFinalPrompt(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return BuildOpenAIPrompt(messagesRaw, toolsRaw, traceID, DefaultToolChoicePolicy(), thinkingEnabled)
}

func BuildOpenAIPrompt(messagesRaw []any, toolsRaw any, traceID string, toolPolicy ToolChoicePolicy, thinkingEnabled bool) (string, []string) {
	messages := NormalizeOpenAIMessagesForPrompt(messagesRaw, traceID)
	toolNames := []string{}
	finalPrompt := FlattenOpenAIMessagesForGrok(messages)
	if tools, ok := toolsRaw.([]any); ok && len(tools) > 0 {
		toolPrompt, names := buildOpenAIToolSystemPrompt(tools, toolPolicy)
		if strings.TrimSpace(toolPrompt) != "" {
			finalPrompt = "[system]: " + toolPrompt + "\n\n" + finalPrompt
		}
		toolNames = names
	}
	if thinkingEnabled {
		if instruction := buildOpenAIThinkingSystemPrompt(); strings.TrimSpace(instruction) != "" {
			finalPrompt = "[system]: " + instruction + "\n\n" + finalPrompt
		}
	}
	return strings.TrimSpace(finalPrompt), toolNames
}

// BuildOpenAIPromptForAdapter exposes the OpenAI-compatible prompt building flow so
// other protocol adapters (for example Gemini) can reuse the same tool/history
// normalization logic and remain behavior-compatible with chat/completions.
func BuildOpenAIPromptForAdapter(messagesRaw []any, toolsRaw any, traceID string, thinkingEnabled bool) (string, []string) {
	return buildOpenAIFinalPrompt(messagesRaw, toolsRaw, traceID, thinkingEnabled)
}

func buildOpenAIThinkingSystemPrompt() string {
	return strings.Join([]string{
		"Continue the conversation from the full prior context and the latest tool results.",
		"Treat earlier messages as binding context; answer the user's current request as a continuation, not a restart.",
		"Keep reasoning internal. Do not leave the final user-facing answer only in reasoning; always provide the answer in visible assistant content.",
	}, "\n")
}
