package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type OpenAIProvider struct {
	Model  string
	Client *openai.Client
}

func NewOpenAIProvider() (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	options := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	if baseURL := normalizeBaseURL(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		options = append(options, option.WithBaseURL(baseURL))
	}

	model := envOrDefault(os.Getenv("OPENAI_MODEL"), "gpt-4o-mini")

	client := openai.NewClient(options...)

	return &OpenAIProvider{
		Model:  model,
		Client: &client,
	}, nil
}

func (p *OpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *OpenAIProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("openai", p.Model, systemPrompt, history, input)

	messages := buildOpenAIChatMessages(systemPrompt, input, history)

	resp, err := p.Client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Model:    openai.ChatModel(p.Model),
			Messages: messages,
		},
	)
	debug.Log("Received OpenAI response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from OpenAI API")
	}

	return resp.Choices[0].Message.Content, nil
}
