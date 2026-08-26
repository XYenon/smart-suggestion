package provider

import (
	"context"
	"fmt"
	"os"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/anthropic"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type AnthropicProvider struct {
	Client          completionClient
	Model           string
	ReasoningEffort string
}

func NewAnthropicProvider() (*AnthropicProvider, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	opts := []anyllm.Option{anyllm.WithAPIKey(apiKey)}
	if baseURL := normalizeBaseURL(os.Getenv("ANTHROPIC_BASE_URL")); baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}

	client, err := anthropic.New(opts...)
	if err != nil {
		return nil, err
	}

	return &AnthropicProvider{
		Client:          client,
		Model:           envOrDefault(os.Getenv("ANTHROPIC_MODEL"), "claude-sonnet-5"),
		ReasoningEffort: os.Getenv("ANTHROPIC_REASONING_EFFORT"),
	}, nil
}

func (p *AnthropicProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *AnthropicProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("anthropic", p.Model, systemPrompt, history, input)

	params := anyllm.CompletionParams{
		Model:    p.Model,
		Messages: buildCompletionMessages(systemPrompt, input, history),
	}
	if p.ReasoningEffort != "" {
		params.ReasoningEffort = reasoningEffort(p.ReasoningEffort)
	}

	resp, err := p.Client.Completion(ctx, params)
	debug.Log("Received Anthropic response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create message: %w", err)
	}

	text, err := extractCompletionText(resp, fmt.Errorf("no content returned from Anthropic API"))
	if err != nil {
		if resp != nil && len(resp.Choices) > 0 {
			return "", fmt.Errorf("no text content returned from Anthropic API")
		}
		return "", err
	}
	return text, nil
}
