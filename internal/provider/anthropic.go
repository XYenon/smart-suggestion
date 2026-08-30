package provider

import (
	"context"
	"fmt"
	"os"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/anthropic"
)

type AnthropicProvider struct {
	Client          completionClient
	Model           string
	ReasoningEffort string
}

func NewAnthropicProvider() (*AnthropicProvider, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, errMissingAnthropicAPIKey
	}

	opts := []anyllm.Option{anyllm.WithAPIKey(apiKey)}
	if baseURL := normalizeBaseURL(os.Getenv("ANTHROPIC_BASE_URL")); baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}

	client, err := anthropic.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating Anthropic client: %w", err)
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

func (p *AnthropicProvider) FetchWithHistory(
	ctx context.Context,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	return fetchChat(ctx, chatFetch{
		client: p.Client,
		empty:  errNoAnthropicContent,
		fail:   "failed to create message",
		model:  p.Model,
		name:   "anthropic",
		effort: p.ReasoningEffort,
	}, input, systemPrompt, history)
}
