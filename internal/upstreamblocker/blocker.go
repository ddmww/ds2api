package upstreamblocker

import (
	"errors"
	"net/http"
	"strings"

	"ds2api/internal/config"
)

const (
	Code           = "upstream_blocked"
	DefaultMessage = "上游渠道商拦截了当前请求，请尝试换个说法后重试，或稍后再试。"
)

type MatchError struct {
	Keyword string
	Message string
}

func (e *MatchError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return e.Message
	}
	return DefaultMessage
}

func (e *MatchError) StatusCode() int {
	return http.StatusForbidden
}

func Normalize(cfg config.UpstreamBlockerConfig) config.UpstreamBlockerConfig {
	out := config.UpstreamBlockerConfig{
		Enabled:       cfg.Enabled,
		CaseSensitive: cfg.CaseSensitive,
		Message:       strings.TrimSpace(cfg.Message),
	}
	if out.Message == "" {
		out.Message = DefaultMessage
	}
	seen := map[string]struct{}{}
	for _, keyword := range cfg.Keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		key := trimmed
		if !out.CaseSensitive {
			key = strings.ToLower(trimmed)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Keywords = append(out.Keywords, trimmed)
	}
	return out
}

func Match(cfg config.UpstreamBlockerConfig, candidate string) (string, bool) {
	cfg = Normalize(cfg)
	if !cfg.Enabled || candidate == "" || len(cfg.Keywords) == 0 {
		return "", false
	}
	haystack := candidate
	if !cfg.CaseSensitive {
		haystack = strings.ToLower(candidate)
	}
	for _, keyword := range cfg.Keywords {
		needle := keyword
		if !cfg.CaseSensitive {
			needle = strings.ToLower(keyword)
		}
		if needle != "" && strings.Contains(haystack, needle) {
			return keyword, true
		}
	}
	return "", false
}

func AssertAllowed(cfg config.UpstreamBlockerConfig, candidate string) error {
	cfg = Normalize(cfg)
	keyword, ok := Match(cfg, candidate)
	if !ok {
		return nil
	}
	return &MatchError{Keyword: keyword, Message: cfg.Message}
}

func AsMatchError(err error) (*MatchError, bool) {
	var matchErr *MatchError
	if errors.As(err, &matchErr) {
		return matchErr, true
	}
	return nil, false
}
