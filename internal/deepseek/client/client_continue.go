package client

import (
	"bufio"
	"bytes"
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/sse"
	"ds2api/internal/truncation"
)

const defaultAutoContinueLimit = 8

type continueOpenFunc func(context.Context, string, int) (*http.Response, error)

type continueState struct {
	sessionID         string
	responseMessageID int
	lastStatus        string
	finished          bool
	thinkingEnabled   bool
	currentType       string
	text              strings.Builder
	thinking          strings.Builder
	truncationEnabled bool
	plainTextContinue bool
	minChars          int
}

// wrapCompletionWithAutoContinue wraps the completion response body so that
// if the visible output looks incomplete, ds2api will automatically call the
// DeepSeek continue endpoint and splice the continuation SSE stream onto the
// original.
// The caller sees a single, seamless SSE stream.
func (c *Client) wrapCompletionWithAutoContinue(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}
	sessionID, _ := payload["chat_session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return resp
	}
	thinkingEnabled, _ := payload["thinking_enabled"].(bool)
	truncationEnabled, plainTextContinue, truncationMaxRounds, truncationMinChars := c.truncationAutoContinueSettings()
	config.Logger.Debug("[auto_continue] wrapping completion response", "session_id", sessionID)
	resp.Body = newAutoContinueBody(ctx, resp.Body, continueState{
		sessionID:         sessionID,
		thinkingEnabled:   thinkingEnabled,
		currentType:       initialSSEPartType(thinkingEnabled),
		truncationEnabled: truncationEnabled,
		plainTextContinue: plainTextContinue,
		minChars:          truncationMinChars,
	}, defaultAutoContinueLimit, truncationMaxRounds, func(ctx context.Context, sessionID string, responseMessageID int) (*http.Response, error) {
		return c.callContinue(ctx, a, sessionID, responseMessageID, powResp)
	})
	return resp
}

func (c *Client) truncationAutoContinueSettings() (enabled bool, plainText bool, maxRounds int, minChars int) {
	if c == nil || c.Store == nil {
		return true, true, 2, 120
	}
	return c.Store.TruncationAutoContinueSettings()
}

// callContinue sends a continue request to DeepSeek to resume generation.
func (c *Client) callContinue(ctx context.Context, a *auth.RequestAuth, sessionID string, responseMessageID int, powResp string) (*http.Response, error) {
	if strings.TrimSpace(sessionID) == "" || responseMessageID <= 0 {
		return nil, errors.New("missing continue identifiers")
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken)
	headers["x-ds-pow-response"] = powResp
	payload := map[string]any{
		"chat_session_id":    sessionID,
		"message_id":         responseMessageID,
		"fallback_to_resume": true,
	}
	config.Logger.Info("[auto_continue] calling continue", "session_id", sessionID, "message_id", responseMessageID)
	captureSession := c.capture.Start("deepseek_continue", dsprotocol.DeepSeekContinueURL, a.AccountID, payload)
	resp, err := c.streamPost(ctx, clients.stream, dsprotocol.DeepSeekContinueURL, headers, payload)
	if err != nil {
		return nil, err
	}
	if captureSession != nil {
		resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errors.New("continue failed")
	}
	return resp, nil
}

// newAutoContinueBody returns a new ReadCloser that transparently pumps
// continuation rounds via an io.Pipe.
func newAutoContinueBody(ctx context.Context, initial io.ReadCloser, state continueState, maxRounds, truncationMaxRounds int, openContinue continueOpenFunc) io.ReadCloser {
	if initial == nil || strings.TrimSpace(state.sessionID) == "" || openContinue == nil {
		return initial
	}
	if maxRounds <= 0 {
		maxRounds = defaultAutoContinueLimit
	}
	if truncationMaxRounds < 0 {
		truncationMaxRounds = 0
	}
	pr, pw := io.Pipe()
	go pumpAutoContinue(ctx, pw, initial, state, maxRounds, truncationMaxRounds, openContinue)
	return pr
}

// pumpAutoContinue is the goroutine that drives the auto-continue loop.
// It reads the initial SSE body, checks whether a continue is required,
// and if so opens a new continue stream and splices it onto the pipe writer.
func pumpAutoContinue(ctx context.Context, pw *io.PipeWriter, initial io.ReadCloser, state continueState, maxRounds, truncationMaxRounds int, openContinue continueOpenFunc) {
	defer func() { _ = pw.Close() }()
	current := initial
	rounds := 0
	truncationRounds := 0
	for {
		hadDone, err := streamBodyWithContinueState(ctx, pw, current, &state)
		_ = current.Close()
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		shouldContinue, reason := state.shouldContinue()
		roundLimit := maxRounds
		if reason == "truncated" {
			roundLimit = truncationMaxRounds
		}
		if shouldContinue && rounds < roundLimit {
			rounds++
			if reason == "truncated" {
				truncationRounds++
			}
			config.Logger.Info("[auto_continue] continuing", "round", rounds, "session_id", state.sessionID, "message_id", state.responseMessageID, "status", state.lastStatus, "reason", reason)
			nextResp, err := openContinue(ctx, state.sessionID, state.responseMessageID)
			if err != nil {
				config.Logger.Warn("[auto_continue] continue request failed", "round", rounds, "error", err)
				if reason == "truncated" {
					break
				}
				_ = pw.CloseWithError(err)
				return
			}
			current = nextResp.Body
			state.prepareForNextRound()
			continue
		}
		if shouldContinue && reason == "truncated" && truncationRounds >= truncationMaxRounds {
			config.Logger.Warn("[auto_continue] truncation continue limit reached", "session_id", state.sessionID, "message_id", state.responseMessageID, "rounds", truncationRounds)
		}
		// Emit the final [DONE] sentinel if the upstream had one.
		if hadDone {
			if _, err := io.Copy(pw, bytes.NewBufferString("data: [DONE]\n")); err != nil {
				_ = pw.CloseWithError(err)
			}
		}
		return
	}
	if _, err := io.Copy(pw, bytes.NewBufferString("data: [DONE]\n")); err != nil {
		_ = pw.CloseWithError(err)
	}
}

// streamBodyWithContinueState scans an SSE body line-by-line, writing each
// line through to pw while observing state signals. Intermediate [DONE]
// sentinels are consumed (not forwarded) so that the downstream only sees
// one final [DONE] at the very end.
func streamBodyWithContinueState(ctx context.Context, pw *io.PipeWriter, body io.Reader, state *continueState) (bool, error) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
	hadDone := false
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return hadDone, ctx.Err()
		default:
		}
		line := append([]byte{}, scanner.Bytes()...)
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data == "[DONE]" {
				hadDone = true
				continue
			}
			state.observe(data)
		}
		state.observeContentLine(line)
		if _, err := io.Copy(pw, bytes.NewReader(append(line, '\n'))); err != nil {
			return hadDone, err
		}
	}
	return hadDone, scanner.Err()
}

func (s *continueState) observeContentLine(line []byte) {
	if s == nil {
		return
	}
	if strings.TrimSpace(s.currentType) == "" {
		s.currentType = initialSSEPartType(s.thinkingEnabled)
	}
	result := sse.ParseDeepSeekContentLine(line, s.thinkingEnabled, s.currentType)
	s.currentType = result.NextType
	if !result.Parsed || len(result.Parts) == 0 {
		return
	}
	for _, part := range result.Parts {
		switch part.Type {
		case "thinking":
			s.thinking.WriteString(truncation.DeduplicateContinuation(s.thinking.String(), part.Text))
		default:
			s.text.WriteString(truncation.DeduplicateContinuation(s.text.String(), part.Text))
		}
	}
}

// observe extracts continue-relevant signals from an SSE JSON chunk.
func (s *continueState) observe(data string) {
	if s == nil || strings.TrimSpace(data) == "" {
		return
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}
	// Top-level response_message_id
	if id := intFrom(chunk["response_message_id"]); id > 0 {
		s.responseMessageID = id
	}
	// Path-based status: {"p": "response/status", "v": "FINISHED"}
	if p, _ := chunk["p"].(string); p == "response/status" {
		if status, _ := chunk["v"].(string); status != "" {
			s.lastStatus = strings.TrimSpace(status)
			if strings.EqualFold(s.lastStatus, "FINISHED") {
				s.finished = true
			}
		}
	}
	// Nested v.response
	v, _ := chunk["v"].(map[string]any)
	if response, _ := v["response"].(map[string]any); response != nil {
		if id := intFrom(response["message_id"]); id > 0 {
			s.responseMessageID = id
		}
		if status, _ := response["status"].(string); status != "" {
			s.lastStatus = strings.TrimSpace(status)
			if strings.EqualFold(s.lastStatus, "FINISHED") {
				s.finished = true
			}
		}
		if autoContinue, ok := response["auto_continue"].(bool); ok && autoContinue {
			s.lastStatus = "AUTO_CONTINUE"
		}
	}
	// Nested message.response
	if message, _ := chunk["message"].(map[string]any); message != nil {
		if response, _ := message["response"].(map[string]any); response != nil {
			if id := intFrom(response["message_id"]); id > 0 {
				s.responseMessageID = id
			}
			if status, _ := response["status"].(string); status != "" {
				s.lastStatus = strings.TrimSpace(status)
				if strings.EqualFold(s.lastStatus, "FINISHED") {
					s.finished = true
				}
			}
		}
	}
}

// shouldContinue returns true based only on visible-output completeness.
// Upstream status values are intentionally ignored here: DeepSeek may emit
// WIP/INCOMPLETE/AUTO_CONTINUE/CONTENT_FILTER states that do not reliably
// mean the visible answer should be continued. This mirrors cursor2api's
// approach of treating truncation as a text-shape problem, while using
// DeepSeek's native continue endpoint rather than re-sending the full prompt.
func (s *continueState) shouldContinue() (bool, string) {
	if s == nil {
		return false, ""
	}
	if !s.truncationEnabled || s.responseMessageID <= 0 || strings.TrimSpace(s.sessionID) == "" {
		return false, ""
	}
	if truncation.ShouldContinue(s.text.String(), s.plainTextContinue, s.minChars) {
		return true, "truncated"
	}
	return false, ""
}

// prepareForNextRound resets ephemeral state before processing the next
// continuation stream.
func (s *continueState) prepareForNextRound() {
	if s == nil {
		return
	}
	s.finished = false
	s.lastStatus = ""
	s.currentType = initialSSEPartType(s.thinkingEnabled)
}

func initialSSEPartType(thinkingEnabled bool) string {
	if thinkingEnabled {
		return "thinking"
	}
	return "text"
}
