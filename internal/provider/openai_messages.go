package provider

import (
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/responses"
)

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

func buildOpenAIResponseInput(input string, history []Message) responses.ResponseInputParam {
	items := make(responses.ResponseInputParam, 0, len(history)+1)
	for _, msg := range history {
		switch msg.Role {
		case "user":
			items = append(items, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleUser))
		case "assistant":
			items = append(items, responses.ResponseInputItemParamOfMessage(msg.Content, responses.EasyInputMessageRoleAssistant))
		}
	}
	items = append(items, responses.ResponseInputItemParamOfMessage(input, responses.EasyInputMessageRoleUser))
	return items
}
