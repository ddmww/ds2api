package util

import "testing"

func TestEstimateOpenAIRequestTokensAddsNewAPIStyleOverheads(t *testing.T) {
	messages := []any{
		map[string]any{
			"role":    "system",
			"content": "You are concise.",
		},
		map[string]any{
			"role": "user",
			"name": "tester",
			"content": []any{
				map[string]any{"type": "text", "text": "Describe this image."},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			},
		},
	}
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "lookup",
				"description": "Find a record.",
				"parameters":  map[string]any{"type": "object"},
			},
		},
	}

	textOnly := EstimateTokensByModel("deepseek-v4", "system\nYou are concise.\nuser\ntester\nDescribe this image.\nlookup\nFind a record.\nmap[type:object]")
	got := EstimateOpenAIRequestTokensByModel("deepseek-v4", messages, tools)
	min := textOnly + 2*openAIMessageTokenOverhead + openAINameTokenOverhead + openAIToolsTokenOverhead + openAIReplyTokenOverhead + estimatedImageTokens
	if got < min {
		t.Fatalf("expected NewAPI-style overheads and image tokens, got %d want at least %d", got, min)
	}
}

func TestEstimateOpenAIRequestTokensWithFallback(t *testing.T) {
	got := EstimateOpenAIRequestTokensWithFallback("deepseek-v4", nil, nil, "hello")
	want := EstimateTokensByModel("deepseek-v4", "hello")
	if got != want {
		t.Fatalf("fallback tokens = %d, want %d", got, want)
	}
}
