package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type AnthropicProvider struct {
	Model  string
	Client *anthropic.Client
}

func NewAnthropicProvider() (*AnthropicProvider, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	options := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if baseURL != "" {
		baseURL = strings.TrimSuffix(baseURL, "/")
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "https://" + baseURL
		}
		options = append(options, option.WithBaseURL(baseURL))
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}

	client := anthropic.NewClient(options...)

	return &AnthropicProvider{
		Model:  model,
		Client: &client,
	}, nil
}

func (p *AnthropicProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *AnthropicProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	debug.Log("Sending Anthropic request", map[string]any{
		"model":         p.Model,
		"system_prompt": systemPrompt,
		"history":       history,
		"input":         input,
	})

	messages := []anthropic.MessageParam{}
	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)))
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)))
		}
	}

	messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(input)))

	resp, err := p.Client.Messages.New(
		ctx,
		anthropic.MessageNewParams{
			Model:     anthropic.Model(p.Model),
			MaxTokens: 1000,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
		},
	)
	debug.Log("Received Anthropic response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("no content returned from Anthropic API")
	}

	return resp.Content[0].Text, nil
}
