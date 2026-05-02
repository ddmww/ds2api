package openai

import (
	"testing"

	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

type mockOpenAIConfig struct {
	aliases             map[string]string
	wideInput           bool
	toolProcessing      *bool
	autoDeleteMode      string
	toolMode            string
	earlyEmit           string
	responsesTTL        int
	embedProv           string
	vision              config.VisionConfig
	historySplitEnabled bool
	historySplitTurns   int
	historySplitUseFile *bool
	currentInputEnabled bool
	currentInputMin     int
	thinkingInjection   *bool
	thinkingPrompt      string
}

func (m mockOpenAIConfig) ModelAliases() map[string]string { return m.aliases }
func (m mockOpenAIConfig) CompatWideInputStrictOutput() bool {
	return m.wideInput
}
func (m mockOpenAIConfig) CompatStripReferenceMarkers() bool { return true }
func (m mockOpenAIConfig) CompatStreamToolBuffer() bool      { return true }
func (m mockOpenAIConfig) CompatToolProcessingEnabled() bool {
	if m.toolProcessing == nil {
		return true
	}
	return *m.toolProcessing
}
func (m mockOpenAIConfig) ToolcallMode() string                { return m.toolMode }
func (m mockOpenAIConfig) ToolcallEarlyEmitConfidence() string { return m.earlyEmit }
func (m mockOpenAIConfig) ResponsesStoreTTLSeconds() int       { return m.responsesTTL }
func (m mockOpenAIConfig) EmbeddingsProvider() string          { return m.embedProv }
func (m mockOpenAIConfig) VisionConfig() config.VisionConfig   { return m.vision }
func (m mockOpenAIConfig) AutoDeleteMode() string {
	if m.autoDeleteMode == "" {
		return "none"
	}
	return m.autoDeleteMode
}
func (m mockOpenAIConfig) AutoDeleteSessions() bool  { return false }
func (m mockOpenAIConfig) HistorySplitEnabled() bool { return m.historySplitEnabled }
func (m mockOpenAIConfig) HistorySplitTriggerAfterTurns() int {
	if m.historySplitTurns <= 0 {
		return 1
	}
	return m.historySplitTurns
}
func (m mockOpenAIConfig) HistorySplitUseFile() bool {
	if m.historySplitUseFile == nil {
		return true
	}
	return *m.historySplitUseFile
}
func (m mockOpenAIConfig) CurrentInputFileEnabled() bool { return m.currentInputEnabled }
func (m mockOpenAIConfig) CurrentInputFileMinChars() int {
	return m.currentInputMin
}
func (m mockOpenAIConfig) ThinkingInjectionEnabled() bool {
	if m.thinkingInjection == nil {
		return false
	}
	return *m.thinkingInjection
}
func (m mockOpenAIConfig) ThinkingInjectionPrompt() string  { return m.thinkingPrompt }
func (m mockOpenAIConfig) EmptyOutputRetryEnabled() bool    { return true }
func (m mockOpenAIConfig) EmptyOutputRetryMaxAttempts() int { return 1 }

func TestNormalizeOpenAIChatRequestWithConfigInterface(t *testing.T) {
	cfg := mockOpenAIConfig{
		aliases: map[string]string{
			"my-model": "deepseek-v4-flash-search",
		},
		wideInput: true,
	}
	req := map[string]any{
		"model":    "my-model",
		"messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("promptcompat.NormalizeOpenAIChatRequest error: %v", err)
	}
	if out.ResolvedModel != "deepseek-v4-flash-search" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if !out.Search || !out.Thinking {
		t.Fatalf("unexpected model flags: thinking=%v search=%v", out.Thinking, out.Search)
	}
}

func TestNormalizeOpenAIChatRequestDisablesThinkingForNoThinkingModel(t *testing.T) {
	cfg := mockOpenAIConfig{wideInput: true}
	req := map[string]any{
		"model":            "deepseek-v4-pro-nothinking",
		"messages":         []any{map[string]any{"role": "user", "content": "hello"}},
		"reasoning_effort": "high",
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("promptcompat.NormalizeOpenAIChatRequest error: %v", err)
	}
	if out.ResolvedModel != "deepseek-v4-pro-nothinking" {
		t.Fatalf("resolved model mismatch: got=%q", out.ResolvedModel)
	}
	if out.Thinking {
		t.Fatalf("expected nothinking model to force thinking off")
	}
	if out.Search {
		t.Fatalf("expected search=false for deepseek-v4-pro-nothinking, got=%v", out.Search)
	}
}

func TestNormalizeOpenAIChatRequestDisablesToolProcessing(t *testing.T) {
	disabled := false
	cfg := mockOpenAIConfig{wideInput: true, toolProcessing: &disabled}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "lookup",
					"description": "lookup data",
				},
			},
		},
		"tool_choice": "required",
	}
	out, err := promptcompat.NormalizeOpenAIChatRequest(cfg, req, "")
	if err != nil {
		t.Fatalf("promptcompat.NormalizeOpenAIChatRequest error: %v", err)
	}
	if out.ToolsRaw != nil || len(out.ToolNames) != 0 {
		t.Fatalf("expected tools to be ignored, tools=%v names=%v", out.ToolsRaw, out.ToolNames)
	}
	if out.ToolChoice.Mode != promptcompat.ToolChoiceNone {
		t.Fatalf("expected tool choice none, got=%q", out.ToolChoice.Mode)
	}
}

func TestNormalizeOpenAIResponsesRequestWideInputPolicyFromInterface(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"input": "hi",
	}

	_, err := promptcompat.NormalizeOpenAIResponsesRequest(mockOpenAIConfig{
		aliases:   map[string]string{},
		wideInput: false,
	}, req, "")
	if err == nil {
		t.Fatal("expected error when wide input is disabled and only input is provided")
	}

	out, err := promptcompat.NormalizeOpenAIResponsesRequest(mockOpenAIConfig{
		aliases:   map[string]string{},
		wideInput: true,
	}, req, "")
	if err != nil {
		t.Fatalf("unexpected error when wide input is enabled: %v", err)
	}
	if out.Surface != "openai_responses" {
		t.Fatalf("unexpected surface: %q", out.Surface)
	}
}
