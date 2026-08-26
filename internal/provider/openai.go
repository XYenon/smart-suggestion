package provider

import (
	"context"
	"fmt"
	"os"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/openai"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type OpenAIAPIType string

const (
	OpenAIAPITypeChatCompletions OpenAIAPIType = "chat_completions"
	OpenAIAPITypeResponses       OpenAIAPIType = "responses"
)

type OpenAIProvider struct {
	APIType         OpenAIAPIType
	Client          completionClient
	Model           string
	ReasoningEffort string
	ResponsesClient responsesClient
}

func parseOpenAIAPIType(val string) (OpenAIAPIType, error) {
	switch val {
	case "", "chat_completions":
		return OpenAIAPITypeChatCompletions, nil
	case "responses":
		return OpenAIAPITypeResponses, nil
	default:
		return "", fmt.Errorf("unsupported OPENAI_API_TYPE: %s (valid: chat_completions, responses)", val)
	}
}

func NewOpenAIProvider() (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	opts := []anyllm.Option{anyllm.WithAPIKey(apiKey)}
	if baseURL := normalizeBaseURL(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}

	client, err := openai.New(opts...)
	if err != nil {
		return nil, err
	}

	apiType, err := parseOpenAIAPIType(os.Getenv("OPENAI_API_TYPE"))
	if err != nil {
		return nil, err
	}

	return &OpenAIProvider{
		APIType:         apiType,
		Client:          client,
		Model:           envOrDefault(os.Getenv("OPENAI_MODEL"), "gpt-5.6-terra"),
		ReasoningEffort: os.Getenv("OPENAI_REASONING_EFFORT"),
		ResponsesClient: client,
	}, nil
}

func (p *OpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *OpenAIProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("openai", p.Model, systemPrompt, history, input)

	switch p.APIType {
	case OpenAIAPITypeResponses:
		return p.fetchResponses(ctx, input, systemPrompt, history)
	case OpenAIAPITypeChatCompletions:
		fallthrough
	default:
		return p.fetchChatCompletions(ctx, input, systemPrompt, history)
	}
}

func (p *OpenAIProvider) fetchChatCompletions(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	params := anyllm.CompletionParams{
		Model:    p.Model,
		Messages: buildCompletionMessages(systemPrompt, input, history),
	}
	if p.ReasoningEffort != "" {
		params.ReasoningEffort = reasoningEffort(p.ReasoningEffort)
	}

	resp, err := p.Client.Completion(ctx, params)
	debug.Log("Received OpenAI response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	return extractCompletionText(resp, fmt.Errorf("no choices returned from OpenAI API"))
}

func (p *OpenAIProvider) fetchResponses(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	params := anyllm.ResponsesParams{
		Input:        buildResponsesInput(input, history),
		Instructions: systemPrompt,
		Model:        p.Model,
	}
	if p.ReasoningEffort != "" {
		params.Reasoning = reasoningEffort(p.ReasoningEffort)
	}

	resp, err := p.ResponsesClient.Responses(ctx, params)
	debug.Log("Received OpenAI response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create response: %w", err)
	}
	if resp == nil || resp.Output == "" {
		if resp != nil && resp.ID != "" && resp.Output == "" {
			return "", fmt.Errorf("empty output text returned from OpenAI API")
		}
		return "", fmt.Errorf("no output returned from OpenAI API")
	}
	return resp.Output, nil
}

func buildResponsesInput(input string, history []Message) []anyllm.ResponsesInputItem {
	items := make([]anyllm.ResponsesInputItem, 0, len(history)+1)
	for _, msg := range history {
		switch msg.Role {
		case "user":
			items = append(items, anyllm.ResponsesInputItem{Role: anyllm.RoleUser, Content: msg.Content})
		case "assistant":
			items = append(items, anyllm.ResponsesInputItem{Role: anyllm.RoleAssistant, Content: msg.Content})
		}
	}
	items = append(items, anyllm.ResponsesInputItem{Role: anyllm.RoleUser, Content: input})
	return items
}
