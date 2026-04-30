package openai

import "ds2api/internal/util"

func BuildChatUsage(model, finalPrompt, finalThinking, finalText string) map[string]any {
	return BuildChatUsageWithPromptTokens(model, util.EstimateTokensByModel(model, finalPrompt), finalThinking, finalText)
}

func BuildChatUsageWithPromptTokens(model string, promptTokens int, finalThinking, finalText string) map[string]any {
	reasoningTokens := util.EstimateTokensByModel(model, finalThinking)
	completionTokens := util.EstimateTokensByModel(model, finalText)
	totalCompletionTokens := reasoningTokens + completionTokens
	return map[string]any{
		"prompt_tokens":     promptTokens,
		"completion_tokens": totalCompletionTokens,
		"total_tokens":      promptTokens + totalCompletionTokens,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": 0,
			"text_tokens":   promptTokens,
			"audio_tokens":  0,
			"image_tokens":  0,
		},
		"completion_tokens_details": map[string]any{
			"text_tokens":      maxInt(totalCompletionTokens-reasoningTokens, 0),
			"audio_tokens":     0,
			"reasoning_tokens": reasoningTokens,
		},
	}
}

func BuildResponsesUsage(model, finalPrompt, finalThinking, finalText string) map[string]any {
	return BuildResponsesUsageWithPromptTokens(model, util.EstimateTokensByModel(model, finalPrompt), finalThinking, finalText)
}

func BuildResponsesUsageWithPromptTokens(model string, promptTokens int, finalThinking, finalText string) map[string]any {
	reasoningTokens := util.EstimateTokensByModel(model, finalThinking)
	completionTokens := util.EstimateTokensByModel(model, finalText)
	return map[string]any{
		"input_tokens":  promptTokens,
		"output_tokens": reasoningTokens + completionTokens,
		"total_tokens":  promptTokens + reasoningTokens + completionTokens,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": reasoningTokens,
		},
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
