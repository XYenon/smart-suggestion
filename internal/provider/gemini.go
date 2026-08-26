package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/gemini"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type GeminiProvider struct {
	Client        completionClient
	Model         string
	ThinkingLevel string
}

func NewGeminiProvider(_ context.Context) (*GeminiProvider, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
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
		thinkingLevel = strings.ToUpper(val)
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

func (p *GeminiProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("gemini", p.Model, systemPrompt, history, input)

	params := anyllm.CompletionParams{
		Model:    p.Model,
		Messages: buildCompletionMessages(systemPrompt, input, history),
	}
	if p.ThinkingLevel != "" {
		params.ReasoningEffort = reasoningEffort(strings.ToLower(p.ThinkingLevel))
	}

	resp, err := p.Client.Completion(ctx, params)
	debug.Log("Received Gemini response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}
	if resp == nil || len(resp.Choices) == 0 {
		return "", fmt.Errorf("no candidates returned from Gemini API")
	}

	text := resp.Choices[0].Message.ContentString()
	if text == "" {
		return "", fmt.Errorf("unexpected part type from Gemini API")
	}
	return text, nil
}
