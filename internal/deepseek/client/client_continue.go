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

var errCompletionContinueFailed = errors.New("completion continuation failed")

type continueOpenFunc func(context.Context, *continueState) (*http.Response, error)

type continueState struct {
	sessionID         string
	responseMessageID int
	lastStatus        string
	finished          bool
	thinkingEnabled   bool
	currentType       string
	text              strings.Builder
	thinking          strings.Builder
	basePayload       map[string]any
	originalPrompt    string
	pendingStatus     [][]byte
	truncationEnabled bool
	plainTextContinue bool
	minChars          int
}

// wrapCompletionWithAutoContinue wraps the completion response body so that
// if the visible output looks incomplete, ds2api rebuilds a cursor2api-style
// continuation prompt and splices the next completion SSE stream onto the
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
	originalPrompt, _ := payload["prompt"].(string)
	thinkingEnabled, _ := payload["thinking_enabled"].(bool)
	truncationEnabled, plainTextContinue, truncationMaxRounds, truncationMinChars := c.truncationAutoContinueSettings()
	config.Logger.Debug("[auto_continue] wrapping completion response", "session_id", sessionID)
	resp.Body = newAutoContinueBody(ctx, resp.Body, continueState{
		sessionID:         sessionID,
		thinkingEnabled:   thinkingEnabled,
		currentType:       initialSSEPartType(thinkingEnabled),
		basePayload:       clonePayloadMap(payload),
		originalPrompt:    originalPrompt,
		truncationEnabled: truncationEnabled,
		plainTextContinue: plainTextContinue,
		minChars:          truncationMinChars,
	}, defaultAutoContinueLimit, truncationMaxRounds, func(ctx context.Context, state *continueState) (*http.Response, error) {
		return c.callContinuationCompletion(ctx, a, powResp, state)
	})
	return resp
}

func (c *Client) truncationAutoContinueSettings() (enabled bool, plainText bool, maxRounds int, minChars int) {
	if c == nil || c.Store == nil {
		return true, true, 2, 120
	}
	return c.Store.TruncationAutoContinueSettings()
}

// callContinuationCompletion sends a normal completion request with the
// cursor2api-style assistant replay + user continuation instruction. It
// deliberately avoids DeepSeek's native /chat/continue endpoint.
func (c *Client) callContinuationCompletion(ctx context.Context, a *auth.RequestAuth, powResp string, state *continueState) (*http.Response, error) {
	if state == nil {
		return nil, errCompletionContinueFailed
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken)
	headers["x-ds-pow-response"] = powResp
	payload := buildContinuationCompletionPayload(state)
	config.Logger.Info("[auto_continue] calling completion continuation", "session_id", state.sessionID, "message_id", state.responseMessageID)
	captureSession := c.capture.Start("deepseek_completion_continue", dsprotocol.DeepSeekCompletionURL, a.AccountID, payload)
	resp, err := c.streamPost(ctx, clients.stream, dsprotocol.DeepSeekCompletionURL, headers, payload)
	if err != nil {
		return nil, err
	}
	if captureSession != nil {
		resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errCompletionContinueFailed
	}
	return resp, nil
}

func buildContinuationCompletionPayload(state *continueState) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	payload := clonePayloadMap(state.basePayload)
	if len(payload) == 0 {
		payload = map[string]any{}
	}
	payload["chat_session_id"] = state.sessionID
	payload["parent_message_id"] = nil
	payload["prompt"] = buildCursorStyleContinuationPrompt(state.originalPrompt, state.text.String())
	return payload
}

func buildCursorStyleContinuationPrompt(originalPrompt, currentText string) string {
	anchorText := currentText
	const anchorLength = 500
	anchorRunes := []rune(anchorText)
	if len(anchorRunes) > anchorLength {
		anchorText = string(anchorRunes[len(anchorRunes)-anchorLength:])
	}
	continuationPrompt := strings.Join([]string{
		"Your previous response was cut off mid-output.",
		"Here is the exact tail of your last assistant response:",
		"",
		"```text",
		"..." + anchorText,
		"```",
		"",
		"Resume from the very next character after the tail above.",
		"Rules:",
		"1. Output only the missing continuation text.",
		"2. Do not repeat, paraphrase, or restart any content that already appeared.",
		"3. Do not restart the current sentence, paragraph, list item, heading, code fence, XML/HTML block, status panel, or image prompt block.",
		"4. If you are about to repeat previously written content, skip forward to the first not-yet-written token instead.",
		"5. Do not add commentary, explanations, acknowledgements, or quotation marks around the continuation.",
		"6. If the response is already complete, output nothing.",
	}, "\n")

	parts := []string{}
	if strings.TrimSpace(originalPrompt) != "" {
		parts = append(parts, strings.TrimSpace(originalPrompt))
	}
	if strings.TrimSpace(currentText) != "" {
		parts = append(parts, "[assistant]: "+currentText)
	}
	parts = append(parts, "[user]: "+continuationPrompt)
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func clonePayloadMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
	dedupeOutgoing := false
	for {
		hadDone, err := streamBodyWithContinueState(ctx, pw, current, &state, dedupeOutgoing)
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
			nextResp, err := openContinue(ctx, &state)
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
			dedupeOutgoing = true
			continue
		}
		if shouldContinue && reason == "truncated" && truncationRounds >= truncationMaxRounds {
			config.Logger.Warn("[auto_continue] truncation continue limit reached", "session_id", state.sessionID, "message_id", state.responseMessageID, "rounds", truncationRounds)
		}
		// Emit the final [DONE] sentinel if the upstream had one.
		if hadDone {
			if err := writePendingStatusLines(pw, &state); err != nil {
				_ = pw.CloseWithError(err)
				return
			}
			if _, err := io.Copy(pw, bytes.NewBufferString("data: [DONE]\n")); err != nil {
				_ = pw.CloseWithError(err)
			}
		}
		return
	}
	if err := writePendingStatusLines(pw, &state); err != nil {
		_ = pw.CloseWithError(err)
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
func streamBodyWithContinueState(ctx context.Context, pw *io.PipeWriter, body io.Reader, state *continueState, dedupeOutgoing bool) (bool, error) {
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
		if dedupeOutgoing {
			rewritten, skip := dedupeContinuationContentLine(line, state)
			if skip {
				continue
			}
			if len(rewritten) > 0 {
				line = rewritten
				trimmed = strings.TrimSpace(string(line))
			}
		}
		if strings.HasPrefix(trimmed, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if data == "[DONE]" {
				hadDone = true
				continue
			}
			if isDeferredStatusLine(data) {
				state.observe(data)
				state.pendingStatus = append(state.pendingStatus, append([]byte{}, line...))
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

func writePendingStatusLines(pw *io.PipeWriter, state *continueState) error {
	if state == nil || len(state.pendingStatus) == 0 {
		return nil
	}
	for _, line := range state.pendingStatus {
		if _, err := io.Copy(pw, bytes.NewReader(append(line, '\n'))); err != nil {
			return err
		}
	}
	state.pendingStatus = nil
	return nil
}

func isDeferredStatusLine(data string) bool {
	var chunk map[string]any
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return false
	}
	if code, _ := chunk["code"].(string); strings.EqualFold(strings.TrimSpace(code), "content_filter") {
		return true
	}
	if p, _ := chunk["p"].(string); p == "response/status" || p == "status" {
		return true
	}
	return false
}

func dedupeContinuationContentLine(line []byte, state *continueState) ([]byte, bool) {
	if state == nil {
		return nil, false
	}
	trimmed := strings.TrimSpace(string(line))
	if !strings.HasPrefix(trimmed, "data:") {
		return nil, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if data == "" || data == "[DONE]" {
		return nil, false
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return nil, false
	}
	v, ok := chunk["v"].(string)
	if !ok || v == "" {
		return nil, false
	}
	path, _ := chunk["p"].(string)
	partType := "text"
	switch {
	case path == "response/thinking_content":
		partType = "thinking"
	case path == "response/content":
		partType = "text"
	case path == "":
		partType = state.currentType
	default:
		return nil, false
	}
	if partType == "thinking" {
		deduped := truncation.DeduplicateContinuation(state.thinking.String(), v)
		if deduped == "" {
			return nil, true
		}
		chunk["v"] = deduped
	} else {
		deduped := truncation.DeduplicateContinuation(state.text.String(), v)
		if deduped == "" {
			return nil, true
		}
		chunk["v"] = deduped
	}
	encoded, err := json.Marshal(chunk)
	if err != nil {
		return nil, false
	}
	return append([]byte("data: "), encoded...), false
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
	if !s.truncationEnabled || strings.TrimSpace(s.sessionID) == "" {
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
	s.pendingStatus = nil
}

func initialSSEPartType(thinkingEnabled bool) string {
	if thinkingEnabled {
		return "thinking"
	}
	return "text"
}
