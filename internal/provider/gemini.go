package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/xyenon/smart-suggestion/internal/debug"
	"google.golang.org/genai"
)

type GeminiProvider struct {
	Model         string
	ThinkingLevel genai.ThinkingLevel
	Client        *genai.Client
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

	model := envOrDefault(os.Getenv("GEMINI_MODEL"), "gemini-3.7-flash")

	var thinkingLevel genai.ThinkingLevel
	if val := os.Getenv("GEMINI_THINKING_LEVEL"); val != "" {
		thinkingLevel = genai.ThinkingLevel(strings.ToUpper(val))
	}

	return &GeminiProvider{
		Model:         model,
		ThinkingLevel: thinkingLevel,
		Client:        client,
	}, nil
}

func (p *GeminiProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *GeminiProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("gemini", p.Model, systemPrompt, history, input)

	config := &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText(systemPrompt, genai.RoleUser)}
	if p.ThinkingLevel != "" {
		config.ThinkingConfig = &genai.ThinkingConfig{
			ThinkingLevel: p.ThinkingLevel,
		}
	}

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

	var textBuilder strings.Builder
	for _, part := range resp.Candidates[0].Content.Parts {
		if !part.Thought && part.Text != "" {
			textBuilder.WriteString(part.Text)
		}
	}

	if textBuilder.Len() > 0 {
		return textBuilder.String(), nil
	}

	return "", fmt.Errorf("unexpected part type from Gemini API")
}
