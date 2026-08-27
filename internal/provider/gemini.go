package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/gemini"
)

type GeminiProvider struct {
	Client        completionClient
	Model         string
	ThinkingLevel string
}

func NewGeminiProvider(_ context.Context) (*GeminiProvider, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, errMissingGeminiAPIKey
	}

	opts := []anyllm.Option{anyllm.WithAPIKey(apiKey)}
	if baseURL := normalizeBaseURL(os.Getenv("GEMINI_BASE_URL")); baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}

	client, err := gemini.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	var thinkingLevel string
	if val := os.Getenv("GEMINI_THINKING_LEVEL"); val != "" {
		thinkingLevel = strings.ToLower(val)
	}

	return &GeminiProvider{
		Client:        client,
		Model:         envOrDefault(os.Getenv("GEMINI_MODEL"), "gemini-3.7-flash"),
		ThinkingLevel: thinkingLevel,
	}, nil
}

func (p *GeminiProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *GeminiProvider) FetchWithHistory(
	ctx context.Context,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	return fetchChat(ctx, chatFetch{
		client: p.Client,
		empty:  errNoGeminiOutput,
		fail:   "failed to send message",
		model:  p.Model,
		name:   "gemini",
		effort: p.ThinkingLevel,
	}, input, systemPrompt, history)
}
