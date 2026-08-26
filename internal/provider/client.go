package provider

import (
	"context"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

// completionClient is the any-llm-go surface used by chat-completion providers.
type completionClient interface {
	Completion(ctx context.Context, params anyllm.CompletionParams) (*anyllm.ChatCompletion, error)
}

// responsesClient is the any-llm-go surface used by the OpenAI Responses API.
type responsesClient interface {
	Responses(ctx context.Context, params anyllm.ResponsesParams) (*anyllm.ResponsesResult, error)
}

func extractCompletionText(resp *anyllm.ChatCompletion, emptyErr error) (string, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return "", emptyErr
	}

	text := resp.Choices[0].Message.ContentString()
	if text == "" {
		return "", emptyErr
	}
	return text, nil
}

func buildCompletionMessages(systemPrompt string, input string, history []Message) []anyllm.Message {
	messages := make([]anyllm.Message, 0, len(history)+2)
	if systemPrompt != "" {
		messages = append(messages, anyllm.Message{
			Role:    anyllm.RoleSystem,
			Content: systemPrompt,
		})
	}
	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, anyllm.Message{Role: anyllm.RoleUser, Content: msg.Content})
		case "assistant":
			messages = append(messages, anyllm.Message{Role: anyllm.RoleAssistant, Content: msg.Content})
		}
	}
	messages = append(messages, anyllm.Message{Role: anyllm.RoleUser, Content: input})
	return messages
}

func reasoningEffort(value string) anyllm.ReasoningEffort {
	return anyllm.ReasoningEffort(value)
}
