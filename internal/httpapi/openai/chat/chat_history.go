package chat

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"ds2api/internal/auth"
	"ds2api/internal/chathistory"
	"ds2api/internal/config"
	"ds2api/internal/prompt"
	"ds2api/internal/promptcompat"
)

const adminWebUISourceHeader = "X-Ds2-Source"
const adminWebUISourceValue = "admin-webui-api-tester"

type chatHistorySession struct {
	store       *chathistory.Store
	entryID     string
	startedAt   time.Time
	lastPersist time.Time
	model       string
	finalPrompt string
	startParams chathistory.StartParams
	disabled    bool

	mu               sync.Mutex
	persistMu        sync.Mutex
	progressInFlight bool
	completed        bool
}

func startChatHistory(store *chathistory.Store, r *http.Request, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) *chatHistorySession {
	if store == nil || r == nil || a == nil {
		return nil
	}
	if !store.Enabled() {
		return nil
	}
	if !shouldCaptureChatHistory(r) {
		return nil
	}
	entry, err := store.Start(chathistory.StartParams{
		CallerID:    strings.TrimSpace(a.CallerID),
		AccountID:   strings.TrimSpace(a.AccountID),
		Model:       strings.TrimSpace(stdReq.ResponseModel),
		Stream:      stdReq.Stream,
		UserInput:   extractSingleUserInput(stdReq.Messages),
		Messages:    extractAllMessages(stdReq.Messages),
		HistoryText: stdReq.HistoryText,
		FinalPrompt: stdReq.FinalPrompt,
	})
	startParams := chathistory.StartParams{
		CallerID:    strings.TrimSpace(a.CallerID),
		AccountID:   strings.TrimSpace(a.AccountID),
		Model:       strings.TrimSpace(stdReq.ResponseModel),
		Stream:      stdReq.Stream,
		UserInput:   extractSingleUserInput(stdReq.Messages),
		Messages:    extractAllMessages(stdReq.Messages),
		HistoryText: stdReq.HistoryText,
		FinalPrompt: stdReq.FinalPrompt,
	}
	session := &chatHistorySession{
		store:       store,
		entryID:     entry.ID,
		startedAt:   time.Now(),
		lastPersist: time.Now(),
		model:       strings.TrimSpace(stdReq.ResponseModel),
		finalPrompt: stdReq.FinalPrompt,
		startParams: startParams,
	}
	if err != nil {
		if entry.ID == "" {
			config.Logger.Warn("[chat_history] start failed", "error", err)
			return nil
		}
		config.Logger.Warn("[chat_history] start persisted in memory after write failure", "error", err)
	}
	return session
}

func shouldCaptureChatHistory(r *http.Request) bool {
	if r == nil {
		return false
	}
	if isVercelStreamPrepareRequest(r) || isVercelStreamReleaseRequest(r) {
		return false
	}
	return strings.TrimSpace(r.Header.Get(adminWebUISourceHeader)) != adminWebUISourceValue
}

func extractSingleUserInput(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		if role != "user" {
			continue
		}
		if normalized := strings.TrimSpace(prompt.NormalizeContent(msg["content"])); normalized != "" {
			return normalized
		}
	}
	return ""
}

func extractAllMessages(messages []any) []chathistory.Message {
	out := make([]chathistory.Message, 0, len(messages))
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		content := strings.TrimSpace(prompt.NormalizeContent(msg["content"]))
		if role == "" || content == "" {
			continue
		}
		out = append(out, chathistory.Message{
			Role:    role,
			Content: content,
		})
	}
	return out
}

func (s *chatHistorySession) retryMissingEntry() bool {
	if s == nil || s.store == nil || s.isDisabled() {
		return false
	}
	entry, err := s.store.Start(s.startParams)
	if errors.Is(err, chathistory.ErrDisabled) {
		s.setDisabled()
		return false
	}
	if entry.ID == "" {
		if err != nil {
			config.Logger.Warn("[chat_history] recreate missing entry failed", "error", err)
		}
		return false
	}
	s.entryID = entry.ID
	if err != nil {
		config.Logger.Warn("[chat_history] recreate missing entry persisted in memory after write failure", "error", err)
	}
	return true
}

func (s *chatHistorySession) handlePersistError(params chathistory.UpdateParams, err error) {
	if err == nil || s == nil {
		return
	}
	if errors.Is(err, chathistory.ErrDisabled) {
		s.setDisabled()
		return
	}
	if isChatHistoryMissingError(err) {
		if s.retryMissingEntry() {
			if _, retryErr := s.store.Update(s.entryID, params); retryErr != nil {
				if errors.Is(retryErr, chathistory.ErrDisabled) || isChatHistoryMissingError(retryErr) {
					s.setDisabled()
					return
				}
				config.Logger.Warn("[chat_history] retry after missing entry failed", "error", retryErr)
			}
			return
		}
		s.setDisabled()
		return
	}
	config.Logger.Warn("[chat_history] update failed", "error", err)
}

func (s *chatHistorySession) isDisabled() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disabled
}

func (s *chatHistorySession) setDisabled() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.disabled = true
	s.mu.Unlock()
}

func isChatHistoryMissingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
