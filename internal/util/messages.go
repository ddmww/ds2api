package util

import (
	"ds2api/internal/claudeconv"
	"ds2api/internal/config"
	"ds2api/internal/prompt"
)

const ClaudeDefaultModel = "claude-sonnet-4-6"

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

func MessagesPrepare(messages []map[string]any) string {
	return prompt.MessagesPrepare(messages)
}

func normalizeContent(v any) string {
	return prompt.NormalizeContent(v)
}

func ConvertClaudeToDeepSeek(claudeReq map[string]any, store *config.Store) map[string]any {
	return claudeconv.ConvertClaudeToDeepSeek(claudeReq, store, ClaudeDefaultModel)
}
