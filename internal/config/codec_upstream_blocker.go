package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

func (c *UpstreamBlockerConfig) UnmarshalJSON(b []byte) error {
	type alias UpstreamBlockerConfig
	var raw struct {
		alias
		Keywords json.RawMessage `json:"keywords"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*c = UpstreamBlockerConfig(raw.alias)
	if len(raw.Keywords) == 0 || string(raw.Keywords) == "null" {
		c.Keywords = nil
		return nil
	}
	var list []string
	if err := json.Unmarshal(raw.Keywords, &list); err == nil {
		c.Keywords = list
		return nil
	}
	var text string
	if err := json.Unmarshal(raw.Keywords, &text); err == nil {
		c.Keywords = splitUpstreamBlockerKeywords(text)
		return nil
	}
	return fmt.Errorf("keywords must be an array or newline-delimited string")
}

func splitUpstreamBlockerKeywords(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\r' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
