package provider

import (
	"context"
	"strings"
)

type Provider interface {
	Fetch(ctx context.Context, input string, systemPrompt string) (string, error)
}

func ParseAndExtractCommand(response string) string {
	closingTag := "</reasoning>"
	if pos := strings.LastIndex(response, closingTag); pos != -1 {
		commandPart := response[pos+len(closingTag):]
		return strings.TrimSpace(commandPart)
	}
	return strings.TrimSpace(response)
}
