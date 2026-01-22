package provider

import (
	"context"
	"fmt"
	"os"
	"strings"

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

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL != "" {
		baseURL = strings.TrimSuffix(baseURL, "/")
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "https://" + baseURL
		}
		options = append(options, option.WithBaseURL(baseURL))
	}

	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}

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
	debug.Log("Sending OpenAI request", map[string]any{
		"model":         p.Model,
		"system_prompt": systemPrompt,
		"history":       history,
		"input":         input,
	})

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}

	for _, msg := range history {
		switch msg.Role {
		case "user":
			messages = append(messages, openai.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openai.AssistantMessage(msg.Content))
		}
	}

	messages = append(messages, openai.UserMessage(input))

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
