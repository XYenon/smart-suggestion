package provider

import (
	"context"
	"strings"
)

type Message struct {
	Role    string // "user" or "assistant"
	Content string
}

type Provider interface {
	Fetch(ctx context.Context, input string, systemPrompt string) (string, error)
	FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error)
}

func ParseAndExtractCommand(response string) string {
	closingTag := "</reasoning>"
	if pos := strings.LastIndex(response, closingTag); pos != -1 {
		commandPart := response[pos+len(closingTag):]
		return strings.TrimSpace(commandPart)
	}
	return strings.TrimSpace(response)
}
