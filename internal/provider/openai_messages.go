package provider

import "github.com/openai/openai-go"

func buildOpenAIChatMessages(systemPrompt string, input string, history []Message) []openai.ChatCompletionMessageParamUnion {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}

	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, openai.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(msg.Content))
		}
	}

	messages = append(messages, openai.UserMessage(input))
	return messages
}
