package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"ds2api/internal/auth"
	"ds2api/internal/promptcompat"
)

func historySplitTestMessages() []any {
	toolCalls := []any{
		map[string]any{
			"name":      "search",
			"arguments": map[string]any{"query": "docs"},
		},
	}
	return []any{
		map[string]any{"role": "system", "content": "system instructions"},
		map[string]any{"role": "user", "content": "first user turn"},
		map[string]any{
			"role":              "assistant",
			"content":           "",
			"reasoning_content": "hidden reasoning",
			"tool_calls":        toolCalls,
		},
		map[string]any{
			"role":         "tool",
			"name":         "search",
			"tool_call_id": "call-1",
			"content":      "tool result",
		},
		map[string]any{"role": "user", "content": "latest user turn"},
	}
}

type streamStatusManagedAuthStub struct{}

func (streamStatusManagedAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "managed-token",
		CallerID:       "caller:test",
		AccountID:      "acct:test",
		TriedAccounts:  map[string]bool{},
	}, nil
}

func (streamStatusManagedAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return (&streamStatusManagedAuthStub{}).Determine(nil)
}

func (streamStatusManagedAuthStub) Release(_ *auth.RequestAuth) {}

func TestBuildOpenAIHistoryTranscriptUsesInlineContext(t *testing.T) {
	_, historyMessages := splitOpenAIHistoryMessages(historySplitTestMessages(), 1)
	transcript := buildOpenAIHistoryTranscript(historyMessages)

	if !strings.HasPrefix(transcript, "Previous conversation history:\n\n") {
		t.Fatalf("expected inline history prefix, got %q", transcript)
	}
	if strings.Contains(transcript, "[file content end]") || strings.Contains(transcript, "[file name]: IGNORE") {
		t.Fatalf("expected no injected file wrapper, got %q", transcript)
	}
	if !strings.Contains(transcript, "first user turn") || !strings.Contains(transcript, "tool result") {
		t.Fatalf("expected historical turns preserved, got %q", transcript)
	}
	if !strings.Contains(transcript, "<tool_calls>") {
		t.Fatalf("expected tool calls preserved, got %q", transcript)
	}
}

func TestSplitOpenAIHistoryMessagesUsesLatestUserTurn(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "system instructions"},
		map[string]any{"role": "user", "content": "first user turn"},
		map[string]any{"role": "assistant", "content": "first assistant turn"},
		map[string]any{"role": "user", "content": "middle user turn"},
		map[string]any{"role": "assistant", "content": "middle assistant turn"},
		map[string]any{"role": "user", "content": "latest user turn"},
	}

	promptMessages, historyMessages := splitOpenAIHistoryMessages(messages, 1)
	if len(promptMessages) == 0 || len(historyMessages) == 0 {
		t.Fatalf("expected both prompt and history messages, got prompt=%d history=%d", len(promptMessages), len(historyMessages))
	}

	promptText, _ := promptcompat.BuildOpenAIPrompt(promptMessages, nil, "", defaultToolChoicePolicy(), true)
	if !strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected latest user turn in prompt, got %s", promptText)
	}
	if strings.Contains(promptText, "middle user turn") {
		t.Fatalf("expected middle user turn to be moved into history, got %s", promptText)
	}

	historyText := buildOpenAIHistoryTranscript(historyMessages)
	if !strings.Contains(historyText, "middle user turn") {
		t.Fatalf("expected middle user turn in split history, got %s", historyText)
	}
	if strings.Contains(historyText, "latest user turn") {
		t.Fatalf("expected latest user turn to remain live, got %s", historyText)
	}
}

func TestApplyHistorySplitSkipsFirstTurn(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			historySplitEnabled: true,
			historySplitTurns:   1,
		},
		DS: ds,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyHistorySplit(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply history split failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload on first turn, got %d", len(ds.uploadCalls))
	}
	if out.FinalPrompt != stdReq.FinalPrompt {
		t.Fatalf("expected prompt unchanged on first turn")
	}
}

func TestApplyHistorySplitCarriesHistoryText(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			historySplitEnabled: true,
			historySplitTurns:   1,
		},
		DS: ds,
	}
	req := map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}

	out, err := h.applyHistorySplit(context.Background(), &auth.RequestAuth{DeepSeekToken: "token"}, stdReq)
	if err != nil {
		t.Fatalf("apply history split failed: %v", err)
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload call, got %d", len(ds.uploadCalls))
	}
	if !strings.Contains(out.HistoryText, "first user turn") || !strings.Contains(out.HistoryText, "tool result") {
		t.Fatalf("expected history text to be preserved on normalized request")
	}
	if !strings.Contains(out.FinalPrompt, "Previous conversation history") ||
		!strings.Contains(out.FinalPrompt, "first user turn") ||
		!strings.Contains(out.FinalPrompt, "latest user turn") {
		t.Fatalf("expected inline history and latest turn in final prompt, got %s", out.FinalPrompt)
	}
}

func TestChatCompletionsHistorySplitInlinesHistoryAndKeepsLatestPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			historySplitEnabled: true,
			historySplitTurns:   1,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload call, got %d", len(ds.uploadCalls))
	}
	if ds.completionReq == nil {
		t.Fatal("expected completion payload to be captured")
	}
	promptText, _ := ds.completionReq["prompt"].(string)
	if !strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected latest turn in completion prompt, got %s", promptText)
	}
	if !strings.Contains(promptText, "Previous conversation history") || !strings.Contains(promptText, "first user turn") {
		t.Fatalf("expected historical turns inline in completion prompt, got %s", promptText)
	}
	refIDs, _ := ds.completionReq["ref_file_ids"].([]any)
	if len(refIDs) != 0 {
		t.Fatalf("expected no uploaded history ref_file_id, got %#v", ds.completionReq["ref_file_ids"])
	}
}

func TestResponsesHistorySplitInlinesHistoryAndKeepsLatestPrompt(t *testing.T) {
	ds := &inlineUploadDSStub{}
	h := &openAITestSurface{
		Store: mockOpenAIConfig{
			wideInput:           true,
			historySplitEnabled: true,
			historySplitTurns:   1,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}
	r := chi.NewRouter()
	registerOpenAITestRoutes(r, h)
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   false,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no upload call, got %d", len(ds.uploadCalls))
	}
	if ds.completionReq == nil {
		t.Fatal("expected completion payload to be captured")
	}
	promptText, _ := ds.completionReq["prompt"].(string)
	if !strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected latest turn in completion prompt, got %s", promptText)
	}
	if !strings.Contains(promptText, "Previous conversation history") || !strings.Contains(promptText, "first user turn") {
		t.Fatalf("expected historical turns inline in completion prompt, got %s", promptText)
	}
}

func TestHistorySplitWorksAcrossAutoDeleteModes(t *testing.T) {
	for _, mode := range []string{"none", "single", "all"} {
		t.Run(mode, func(t *testing.T) {
			ds := &inlineUploadDSStub{}
			h := &openAITestSurface{
				Store: mockOpenAIConfig{
					wideInput:           true,
					autoDeleteMode:      mode,
					historySplitEnabled: true,
					historySplitTurns:   1,
				},
				Auth: streamStatusAuthStub{},
				DS:   ds,
			}
			reqBody, _ := json.Marshal(map[string]any{
				"model":    "deepseek-v4-flash",
				"messages": historySplitTestMessages(),
				"stream":   false,
			})
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(reqBody)))
			req.Header.Set("Authorization", "Bearer direct-token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.ChatCompletions(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
			}
			if len(ds.uploadCalls) != 0 {
				t.Fatalf("expected no history split upload for mode=%s, got %d", mode, len(ds.uploadCalls))
			}
			if ds.completionReq == nil {
				t.Fatalf("expected completion payload for mode=%s", mode)
			}
			promptText, _ := ds.completionReq["prompt"].(string)
			if !strings.Contains(promptText, "latest user turn") || !strings.Contains(promptText, "first user turn") {
				t.Fatalf("unexpected prompt for mode=%s: %s", mode, promptText)
			}
		})
	}
}

func defaultToolChoicePolicy() promptcompat.ToolChoicePolicy {
	return promptcompat.DefaultToolChoicePolicy()
}
