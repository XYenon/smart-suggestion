package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"
	"github.com/openai/openai-go/shared"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type OpenAIAPIType string

const (
	OpenAIAPITypeChatCompletions OpenAIAPIType = "chat_completions"
	OpenAIAPITypeResponses       OpenAIAPIType = "responses"
)

type OpenAIProvider struct {
	Model           string
	APIType         OpenAIAPIType
	ReasoningEffort shared.ReasoningEffort
	Client          *openai.Client
}

func parseOpenAIAPIType(val string) (OpenAIAPIType, error) {
	switch val {
	case "", "chat_completions":
		return OpenAIAPITypeChatCompletions, nil
	case "responses":
		return OpenAIAPITypeResponses, nil
	default:
		return "", fmt.Errorf("unsupported OPENAI_API_TYPE: %s (valid: chat_completions, responses)", val)
	}
}

func NewOpenAIProvider() (*OpenAIProvider, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	options := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}

	if baseURL := normalizeBaseURL(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		options = append(options, option.WithBaseURL(baseURL))
	}

	model := envOrDefault(os.Getenv("OPENAI_MODEL"), "gpt-4o-mini")

	apiType, err := parseOpenAIAPIType(os.Getenv("OPENAI_API_TYPE"))
	if err != nil {
		return nil, err
	}

	reasoningEffort := shared.ReasoningEffort(os.Getenv("OPENAI_REASONING_EFFORT"))

	client := openai.NewClient(options...)

	return &OpenAIProvider{
		Model:           model,
		APIType:         apiType,
		ReasoningEffort: reasoningEffort,
		Client:          &client,
	}, nil
}

func (p *OpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *OpenAIProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("openai", p.Model, systemPrompt, history, input)

	switch p.APIType {
	case OpenAIAPITypeResponses:
		return p.fetchResponses(ctx, input, systemPrompt, history)
	case OpenAIAPITypeChatCompletions:
		fallthrough
	default:
		return p.fetchChatCompletions(ctx, input, systemPrompt, history)
	}
}

func (p *OpenAIProvider) fetchChatCompletions(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	messages := buildOpenAIChatMessages(systemPrompt, input, history)

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(p.Model),
		Messages: messages,
	}
	if p.ReasoningEffort != "" {
		params.ReasoningEffort = p.ReasoningEffort
	}

	resp, err := p.Client.Chat.Completions.New(ctx, params)
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

func (p *OpenAIProvider) fetchResponses(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	inputItems := buildOpenAIResponseInput(input, history)

	params := responses.ResponseNewParams{
		Model: responses.ResponsesModel(p.Model),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: inputItems,
		},
	}
	if systemPrompt != "" {
		params.Instructions = openai.String(systemPrompt)
	}
	if p.ReasoningEffort != "" {
		params.Reasoning = shared.ReasoningParam{
			Effort: p.ReasoningEffort,
		}
	}

	resp, err := p.Client.Responses.New(ctx, params)
	debug.Log("Received OpenAI response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create response: %w", err)
	}

	if len(resp.Output) == 0 {
		return "", fmt.Errorf("no output returned from OpenAI API")
	}

	outputText := resp.OutputText()
	if outputText == "" {
		return "", fmt.Errorf("empty output text returned from OpenAI API")
	}

	return outputText, nil
}
