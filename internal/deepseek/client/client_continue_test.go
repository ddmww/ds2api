package client

import (
	"bytes"
	"context"
	dsprotocol "ds2api/internal/deepseek/protocol"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"ds2api/internal/auth"
)

type failingDoer struct {
	err error
}

func (d failingDoer) Do(_ *http.Request) (*http.Response, error) {
	return nil, d.err
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCallContinuePropagatesPowHeaderToFallbackRequest(t *testing.T) {
	var seenPow string
	var seenURL string

	client := &Client{
		stream: failingDoer{err: errors.New("stream transport failed")},
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				seenPow = req.Header.Get("x-ds-pow-response")
				seenURL = req.URL.String()
				body := io.NopCloser(strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"continued\"}\n" + "data: [DONE]\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := client.callContinue(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
	}, "session-123", 99, "pow-response-abc")
	if err != nil {
		t.Fatalf("callContinue returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if seenPow != "pow-response-abc" {
		t.Fatalf("continue request pow header=%q want=%q", seenPow, "pow-response-abc")
	}
	if seenURL != dsprotocol.DeepSeekContinueURL {
		t.Fatalf("continue request url=%q want=%q", seenURL, dsprotocol.DeepSeekContinueURL)
	}
}

func TestCallCompletionAutoContinueThreadsPowHeaderForTruncatedText(t *testing.T) {
	var seenPow string
	var seenContinueURL string

	initialBody := strings.Join([]string{
		`data: {"response_message_id":321}`,
		`data: {"p":"response/content","v":"这是一段很长的回答，用来模拟上游在特殊状态下把最终输出截断。它已经超过了最小长度，而且最后一句明显没有说完。为了触发启发式检测，这里继续堆叠足够长的上下文，让文本长度超过默认阈值，同时结尾仍然停在一个没有结束标点的普通词语上，表示模型原本应该继续解释这个尚未完成的概念"}`,
		`data: {"p":"response/status","v":"CONTENT_FILTER"}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	client := &Client{
		stream: failingOrCompletionDoer{
			completionResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(initialBody)),
			},
		},
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				seenPow = req.Header.Get("x-ds-pow-response")
				seenContinueURL = req.URL.String()
				body := io.NopCloser(strings.NewReader("data: {\"response_message_id\":322,\"v\":{\"response\":{\"message_id\":322,\"status\":\"FINISHED\"}}}\n" + "data: {\"p\":\"response/content\",\"v\":\"，这里是自动续写补上的内容。\"}\n" + "data: [DONE]\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
	}, map[string]any{
		"chat_session_id": "session-123",
	}, "pow-response-xyz", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read auto-continued body failed: %v", err)
	}
	if seenPow != "pow-response-xyz" {
		t.Fatalf("threaded continue pow header=%q want=%q", seenPow, "pow-response-xyz")
	}
	if seenContinueURL != dsprotocol.DeepSeekContinueURL {
		t.Fatalf("continue url=%q want=%q", seenContinueURL, dsprotocol.DeepSeekContinueURL)
	}
	if !bytes.Contains(out, []byte(`"v":"CONTENT_FILTER"`)) {
		t.Fatalf("expected initial stream content in body, got=%s", string(out))
	}
	if !bytes.Contains(out, []byte("自动续写补上的内容")) {
		t.Fatalf("expected continuation content in body, got=%s", string(out))
	}
	if !bytes.Contains(out, []byte(`data: [DONE]`)) {
		t.Fatalf("expected final DONE sentinel in body, got=%s", string(out))
	}
}

func TestCallCompletionDoesNotAutoContinueCompleteWIPText(t *testing.T) {
	var continueCalls int

	initialBody := strings.Join([]string{
		`data: {"response_message_id":321,"v":{"response":{"message_id":321,"status":"WIP","auto_continue":true}}}`,
		`data: {"p":"response/content","v":"这是一个完整回答。"}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	client := &Client{
		stream: failingOrCompletionDoer{
			completionResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(initialBody)),
			},
		},
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				continueCalls++
				body := io.NopCloser(strings.NewReader("data: {\"p\":\"response/content\",\"v\":\"unexpected\"}\n" + "data: [DONE]\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
	}, map[string]any{
		"chat_session_id": "session-123",
	}, "pow-response-xyz", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read auto-continued body failed: %v", err)
	}
	if continueCalls != 0 {
		t.Fatalf("expected no status-only continue calls, got %d", continueCalls)
	}
	if bytes.Contains(out, []byte("unexpected")) {
		t.Fatalf("unexpected continuation body, got=%s", string(out))
	}
	if bytes.Count(out, []byte("data: [DONE]")) != 1 {
		t.Fatalf("expected one final DONE sentinel, got=%s", string(out))
	}
}

func TestCallCompletionAutoContinuesTruncatedFinishedText(t *testing.T) {
	var continueCalls int

	initialBody := strings.Join([]string{
		`data: {"response_message_id":321}`,
		`data: {"p":"response/content","v":"这是一段很长的回答，用来模拟上游把最终输出截断。它已经超过了最小长度，而且最后一句明显没有说完。为了触发启发式检测，这里继续堆叠足够长的上下文，让文本长度超过默认阈值，同时结尾仍然停在一个没有结束标点的普通词语上，表示模型原本应该继续解释这个尚未完成的概念"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
		`data: [DONE]`,
	}, "\n") + "\n"

	client := &Client{
		stream: failingOrCompletionDoer{
			completionResp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(initialBody)),
			},
		},
		fallbackS: &http.Client{
			Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				continueCalls++
				body := io.NopCloser(strings.NewReader(
					"data: {\"response_message_id\":322}\n" +
						"data: {\"p\":\"response/content\",\"v\":\"，这里是自动续写补上的内容。\"}\n" +
						"data: {\"p\":\"response/status\",\"v\":\"FINISHED\"}\n" +
						"data: [DONE]\n"))
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       body,
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := client.CallCompletion(context.Background(), &auth.RequestAuth{
		DeepSeekToken: "token",
		AccountID:     "acct",
	}, map[string]any{
		"chat_session_id": "session-123",
	}, "pow-response-xyz", 1)
	if err != nil {
		t.Fatalf("CallCompletion returned error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read auto-continued body failed: %v", err)
	}
	if continueCalls != 1 {
		t.Fatalf("expected one truncation continue call, got %d", continueCalls)
	}
	if !bytes.Contains(out, []byte("自动续写补上的内容")) {
		t.Fatalf("expected continuation content in body, got=%s", string(out))
	}
	if bytes.Count(out, []byte("data: [DONE]")) != 1 {
		t.Fatalf("expected one final DONE sentinel, got=%s", string(out))
	}
}

type failingOrCompletionDoer struct {
	completionResp *http.Response
}

func (d failingOrCompletionDoer) Do(req *http.Request) (*http.Response, error) {
	if strings.Contains(req.URL.Path, "/chat/completion") {
		return d.completionResp, nil
	}
	return nil, errors.New("forced stream failure")
}
