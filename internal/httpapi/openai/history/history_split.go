package history

import (
	"context"
	"errors"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/httpapi/openai/shared"
	"ds2api/internal/promptcompat"
)

type Service struct {
	Store shared.ConfigReader
	DS    shared.DeepSeekCaller
}

func (s Service) Apply(ctx context.Context, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) (promptcompat.StandardRequest, error) {
	if s.DS == nil || s.Store == nil || a == nil {
		return stdReq, nil
	}

	promptMessages, historyMessages := SplitOpenAIHistoryMessages(stdReq.Messages, s.Store.HistorySplitTriggerAfterTurns())
	if len(historyMessages) == 0 {
		return stdReq, nil
	}

	historyText := promptcompat.BuildOpenAIHistoryTranscript(historyMessages)
	if strings.TrimSpace(historyText) == "" {
		return stdReq, errors.New("history split produced empty transcript")
	}

	promptMessages = injectHistoryContextMessage(promptMessages, historyText)
	stdReq.Messages = promptMessages
	stdReq.HistoryText = historyText
	stdReq.FinalPrompt, stdReq.ToolNames = promptcompat.BuildOpenAIPrompt(promptMessages, stdReq.ToolsRaw, "", stdReq.ToolChoice, stdReq.Thinking)
	return stdReq, nil
}

func SplitOpenAIHistoryMessages(messages []any, triggerAfterTurns int) ([]any, []any) {
	if triggerAfterTurns <= 0 {
		triggerAfterTurns = 1
	}
	lastUserIndex := -1
	userTurns := 0
	for i, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
		if role != "user" {
			continue
		}
		userTurns++
		lastUserIndex = i
	}
	if userTurns <= triggerAfterTurns || lastUserIndex < 0 {
		return messages, nil
	}

	promptMessages := make([]any, 0, len(messages)-lastUserIndex)
	historyMessages := make([]any, 0, lastUserIndex)
	for i, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			if i >= lastUserIndex {
				promptMessages = append(promptMessages, raw)
			} else {
				historyMessages = append(historyMessages, raw)
			}
			continue
		}
		role := strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
		switch role {
		case "system", "developer":
			promptMessages = append(promptMessages, raw)
		default:
			if i >= lastUserIndex {
				promptMessages = append(promptMessages, raw)
			} else {
				historyMessages = append(historyMessages, raw)
			}
		}
	}
	if len(promptMessages) == 0 {
		return messages, nil
	}
	return promptMessages, historyMessages
}

func injectHistoryContextMessage(messages []any, historyText string) []any {
	historyText = strings.TrimSpace(historyText)
	if historyText == "" {
		return messages
	}
	historyMessage := map[string]any{
		"role":    "system",
		"content": "Use the following previous conversation history as context. It is not a file attachment.\n\n" + historyText,
	}
	out := make([]any, 0, len(messages)+1)
	inserted := false
	for i, raw := range messages {
		if !inserted {
			role := ""
			if msg, ok := raw.(map[string]any); ok {
				role = strings.ToLower(strings.TrimSpace(shared.AsString(msg["role"])))
			}
			if i > 0 && role != "system" && role != "developer" {
				out = append(out, historyMessage)
				inserted = true
			}
		}
		out = append(out, raw)
	}
	if !inserted {
		out = append(out, historyMessage)
	}
	return out
}
