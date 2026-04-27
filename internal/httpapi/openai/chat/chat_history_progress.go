package chat

import (
	"net/http"
	"time"

	"ds2api/internal/chathistory"
	openaifmt "ds2api/internal/format/openai"
)

func (s *chatHistorySession) progress(thinking, content string) {
	if s == nil || s.store == nil {
		return
	}
	now := time.Now()
	s.mu.Lock()
	if s.disabled || s.completed || s.progressInFlight || now.Sub(s.lastPersist) < 250*time.Millisecond {
		s.mu.Unlock()
		return
	}
	s.lastPersist = now
	s.progressInFlight = true
	params := chathistory.UpdateParams{
		Status:           "streaming",
		ReasoningContent: thinking,
		Content:          content,
		StatusCode:       http.StatusOK,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
	}
	s.mu.Unlock()

	go s.persistProgress(params)
}

func (s *chatHistorySession) success(statusCode int, thinking, content, finishReason string, usage map[string]any) {
	if !s.markCompleted() {
		return
	}
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "success",
		ReasoningContent: thinking,
		Content:          content,
		StatusCode:       statusCode,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
		FinishReason:     finishReason,
		Usage:            usage,
		Completed:        true,
	})
}

func (s *chatHistorySession) error(statusCode int, message, finishReason, thinking, content string) {
	if !s.markCompleted() {
		return
	}
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "error",
		ReasoningContent: thinking,
		Content:          content,
		Error:            message,
		StatusCode:       statusCode,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
		FinishReason:     finishReason,
		Completed:        true,
	})
}

func (s *chatHistorySession) stopped(thinking, content, finishReason string) {
	if !s.markCompleted() {
		return
	}
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "stopped",
		ReasoningContent: thinking,
		Content:          content,
		StatusCode:       http.StatusOK,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
		FinishReason:     finishReason,
		Usage:            openaifmt.BuildChatUsage(s.model, s.finalPrompt, thinking, content),
		Completed:        true,
	})
}

func (s *chatHistorySession) markCompleted() bool {
	if s == nil || s.store == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.disabled || s.completed {
		return false
	}
	s.completed = true
	return true
}

func (s *chatHistorySession) persistProgress(params chathistory.UpdateParams) {
	defer func() {
		s.mu.Lock()
		s.progressInFlight = false
		s.mu.Unlock()
	}()
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.mu.Lock()
	if s.disabled || s.completed {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.persistUpdateLocked(params)
}

func (s *chatHistorySession) persistUpdate(params chathistory.UpdateParams) {
	if s == nil || s.store == nil || s.isDisabled() {
		return
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.persistUpdateLocked(params)
}

func (s *chatHistorySession) persistUpdateLocked(params chathistory.UpdateParams) {
	if _, err := s.store.Update(s.entryID, params); err != nil {
		s.handlePersistError(params, err)
	}
}
