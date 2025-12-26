package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/google/generative-ai-go/genai"
	"github.com/xyenon/smart-suggestion/internal/debug"
	"google.golang.org/api/option"
)

type GeminiProvider struct {
	Model  string
	Client *genai.Client
}

func NewGeminiProvider() (*GeminiProvider, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	ctx := context.Background()
	var options []option.ClientOption
	options = append(options, option.WithAPIKey(apiKey))

	baseURL := os.Getenv("GEMINI_BASE_URL")
	if baseURL != "" {
		options = append(options, option.WithEndpoint(baseURL))
	}

	client, err := genai.NewClient(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	return &GeminiProvider{
		Model:  model,
		Client: client,
	}, nil
}

func (p *GeminiProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	debug.Log("Sending Gemini request", map[string]any{
		"model": p.Model,
	})

	model := p.Client.GenerativeModel(p.Model)

	if systemPrompt != "" {
		model.SystemInstruction = &genai.Content{
			Parts: []genai.Part{genai.Text(systemPrompt)},
		}
	}

	resp, err := model.GenerateContent(ctx, genai.Text(input))
	if err != nil {
		return "", fmt.Errorf("failed to generate content: %w", err)
	}

	rawResp, _ := json.Marshal(resp)
	debug.Log("Received Gemini response", map[string]any{
		"response": string(rawResp),
	})

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidates returned from Gemini API")
	}

	if resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content parts returned from Gemini API")
	}

	part := resp.Candidates[0].Content.Parts[0]
	if text, ok := part.(genai.Text); ok {
		return string(text), nil
	}

	return "", fmt.Errorf("unexpected part type from Gemini API")
}
