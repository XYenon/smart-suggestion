package provider

import (
	"context"
	"errors"
	"fmt"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

const (
	failCreateChatCompletion = "failed to create chat completion"
	roleAssistant            = "assistant"
	roleUser                 = "user"
)

var (
	errMissingAnthropicAPIKey = errors.New("ANTHROPIC_API_KEY environment variable is not set")
	errMissingAzureAPIKey     = errors.New("AZURE_OPENAI_API_KEY environment variable is not set")
	errMissingAzureDeployment = errors.New("AZURE_OPENAI_DEPLOYMENT_NAME environment variable is not set")
	errMissingAzureResource   = errors.New("AZURE_OPENAI_RESOURCE_NAME environment variable is not set")
	errMissingGeminiAPIKey    = errors.New("GEMINI_API_KEY environment variable is not set")
	errMissingOpenAIAPIKey    = errors.New("OPENAI_API_KEY environment variable is not set")

	errNoAnthropicContent = errors.New("no content returned from Anthropic API")
	errNoAzureChoices     = errors.New("no choices returned from Azure OpenAI API")
	errNoGeminiOutput     = errors.New("no candidates returned from Gemini API")
	errNoOpenAIChoices    = errors.New("no choices returned from OpenAI API")
	errEmptyOpenAIOutput  = errors.New("empty output text returned from OpenAI API")
	errNoOpenAIOutput     = errors.New("no output returned from OpenAI API")
)

// completionClient is the any-llm-go surface used by chat-completion providers.
type completionClient interface {
	Completion(ctx context.Context, params anyllm.CompletionParams) (*anyllm.ChatCompletion, error)
}

// responsesClient is the any-llm-go surface used by the OpenAI Responses API.
type responsesClient interface {
	Responses(ctx context.Context, params anyllm.ResponsesParams) (*anyllm.ResponsesResult, error)
}

type chatFetch struct {
	client completionClient
	empty  error
	fail   string
	model  string
	name   string
	effort string
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
		case roleUser:
			messages = append(messages, anyllm.Message{Role: anyllm.RoleUser, Content: msg.Content})
		case roleAssistant:
			messages = append(messages, anyllm.Message{Role: anyllm.RoleAssistant, Content: msg.Content})
		}
	}
	messages = append(messages, anyllm.Message{Role: anyllm.RoleUser, Content: input})

	return messages
}

func reasoningEffort(value string) anyllm.ReasoningEffort {
	return anyllm.ReasoningEffort(value)
}

func fetchChat(
	ctx context.Context,
	fetch chatFetch,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	logProviderRequest(fetch.name, fetch.model, systemPrompt, history, input)

	params := anyllm.CompletionParams{
		Model:    fetch.model,
		Messages: buildCompletionMessages(systemPrompt, input, history),
	}
	if fetch.effort != "" {
		params.ReasoningEffort = reasoningEffort(fetch.effort)
	}

	resp, err := fetch.client.Completion(ctx, params)
	debug.Log("Received "+fetch.name+" response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("%s: %w", fetch.fail, err)
	}

	return extractCompletionText(resp, fetch.empty)
}
