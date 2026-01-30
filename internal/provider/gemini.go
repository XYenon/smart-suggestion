package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/xyenon/smart-suggestion/internal/debug"
	"google.golang.org/genai"
)

type GeminiProvider struct {
	Model  string
	Client *genai.Client
}

func NewGeminiProvider(ctx context.Context) (*GeminiProvider, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	config := &genai.ClientConfig{APIKey: apiKey}

	baseURL := os.Getenv("GEMINI_BASE_URL")
	if baseURL != "" {
		config.HTTPOptions.BaseURL = baseURL
	}

	client, err := genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	model := envOrDefault(os.Getenv("GEMINI_MODEL"), "gemini-2.5-flash")

	return &GeminiProvider{
		Model:  model,
		Client: client,
	}, nil
}

func (p *GeminiProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *GeminiProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("gemini", p.Model, systemPrompt, history, input)

	config := &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser)}

	var chatHistory []*genai.Content
	for _, msg := range history {
		var role genai.Role
		switch msg.Role {
		case "user":
			role = genai.RoleUser
		case "assistant":
			role = genai.RoleModel
		default:
			continue // Skip unknown roles
		}
		chatHistory = append(chatHistory, genai.NewContentFromText(msg.Content, role))
	}

	chat, err := p.Client.Chats.Create(ctx, p.Model, config, chatHistory)
	if err != nil {
		return "", fmt.Errorf("failed to create chat: %w", err)
	}

	resp, err := chat.SendMessage(ctx, genai.Part{Text: input})
	debug.Log("Received Gemini response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to send message: %w", err)
	}

	if len(resp.Candidates) == 0 {
		return "", fmt.Errorf("no candidates returned from Gemini API")
	}
	if resp.Candidates[0].Content == nil || len(resp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content parts returned from Gemini API")
	}

	part := resp.Candidates[0].Content.Parts[0]
	if part.Text != "" {
		return part.Text, nil
	}

	return "", fmt.Errorf("unexpected part type from Gemini API")
}
