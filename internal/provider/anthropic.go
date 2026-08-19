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
	Model           string
	ReasoningEffort string
	Client          *anthropic.Client
}

func NewAnthropicProvider() (*AnthropicProvider, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	options := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	if baseURL := normalizeBaseURL(os.Getenv("ANTHROPIC_BASE_URL")); baseURL != "" {
		options = append(options, option.WithBaseURL(baseURL))
	}

	model := envOrDefault(os.Getenv("ANTHROPIC_MODEL"), "claude-3-5-sonnet-20241022")

	reasoningEffort := os.Getenv("ANTHROPIC_REASONING_EFFORT")

	client := anthropic.NewClient(options...)

	return &AnthropicProvider{
		Model:           model,
		ReasoningEffort: reasoningEffort,
		Client:          &client,
	}, nil
}

func (p *AnthropicProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *AnthropicProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("anthropic", p.Model, systemPrompt, history, input)

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

	maxTokens := int64(1000)
	params := anthropic.MessageNewParams{
		Model:    anthropic.Model(p.Model),
		System:   []anthropic.TextBlockParam{{Text: systemPrompt}},
		Messages: messages,
	}

	if p.ReasoningEffort != "" {
		params.Thinking = anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		}
		params.OutputConfig = anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(p.ReasoningEffort),
		}
	}
	params.MaxTokens = maxTokens

	resp, err := p.Client.Messages.New(ctx, params)
	debug.Log("Received Anthropic response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("no content returned from Anthropic API")
	}

	var textBuilder strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" || block.Text != "" {
			textBuilder.WriteString(block.Text)
		}
	}

	if textBuilder.Len() == 0 {
		return "", fmt.Errorf("no text content returned from Anthropic API")
	}

	return textBuilder.String(), nil
}
