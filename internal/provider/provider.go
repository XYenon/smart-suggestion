package provider

import "strings"

type Provider interface {
	Fetch(input string, systemPrompt string) (string, error)
}

// Common types for OpenAI-compatible APIs
type OpenAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OpenAIRequest struct {
	Model    string          `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
}

type OpenAIChoice struct {
	Message OpenAIMessage `json:"message"`
}

type OpenAIResponse struct {
	Choices []OpenAIChoice `json:"choices"`
	Error   *OpenAIError   `json:"error,omitempty"`
}

type OpenAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func ParseAndExtractCommand(response string) string {
	closingTag := "</reasoning>"
	if pos := strings.LastIndex(response, closingTag); pos != -1 {
		commandPart := response[pos+len(closingTag):]
		return strings.TrimSpace(commandPart)
	}
	return strings.TrimSpace(response)
}
