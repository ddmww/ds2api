package upstreamblocker

import (
	"testing"

	"ds2api/internal/config"
)

func TestMatchCaseInsensitive(t *testing.T) {
	cfg := config.UpstreamBlockerConfig{
		Enabled:  true,
		Keywords: []string{"sorry", "我无法"},
	}
	if keyword, ok := Match(cfg, "Sorry, I cannot help."); !ok || keyword != "sorry" {
		t.Fatalf("expected sorry match, got keyword=%q ok=%v", keyword, ok)
	}
}

func TestMatchCaseSensitive(t *testing.T) {
	cfg := config.UpstreamBlockerConfig{
		Enabled:       true,
		CaseSensitive: true,
		Keywords:      []string{"Grok"},
	}
	if _, ok := Match(cfg, "grok"); ok {
		t.Fatal("did not expect case-sensitive match")
	}
	if keyword, ok := Match(cfg, "Grok"); !ok || keyword != "Grok" {
		t.Fatalf("expected Grok match, got keyword=%q ok=%v", keyword, ok)
	}
}

func TestAssertAllowedReturnsMatchError(t *testing.T) {
	cfg := config.UpstreamBlockerConfig{
		Enabled:  true,
		Keywords: []string{"拒绝"},
		Message:  "blocked",
	}
	err := AssertAllowed(cfg, "我拒绝回答")
	matchErr, ok := AsMatchError(err)
	if !ok {
		t.Fatalf("expected MatchError, got %T", err)
	}
	if matchErr.Error() != "blocked" || matchErr.Keyword != "拒绝" {
		t.Fatalf("unexpected match error: %#v", matchErr)
	}
}
