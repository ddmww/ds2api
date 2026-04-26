package promptcompat

import (
	"fmt"
	"strings"
)

func BuildOpenAIHistoryTranscript(messages []any) string {
	normalized := NormalizeOpenAIMessagesForPrompt(messages, "")
	transcript := strings.TrimSpace(FlattenOpenAIMessagesForGrok(normalized))
	if transcript == "" {
		return ""
	}
	return fmt.Sprintf("Previous conversation history:\n\n%s", transcript)
}
