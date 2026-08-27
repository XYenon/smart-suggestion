package provider

import (
	"context"
	"fmt"
	"os"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/openai"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type OpenAIAPIType string

const (
	OpenAIAPITypeChatCompletions OpenAIAPIType = "chat_completions"
	OpenAIAPITypeResponses       OpenAIAPIType = "responses"
)

type OpenAIProvider struct {
	APIType         OpenAIAPIType
	Client          completionClient
	Model           string
	ReasoningEffort string
	ResponsesClient responsesClient
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
		return nil, errMissingOpenAIAPIKey
	}

	opts := []anyllm.Option{anyllm.WithAPIKey(apiKey)}
	if baseURL := normalizeBaseURL(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		opts = append(opts, anyllm.WithBaseURL(baseURL))
	}

	client, err := openai.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating OpenAI client: %w", err)
	}

	apiType, err := parseOpenAIAPIType(os.Getenv("OPENAI_API_TYPE"))
	if err != nil {
		return nil, err
	}

	return &OpenAIProvider{
		APIType:         apiType,
		Client:          client,
		Model:           envOrDefault(os.Getenv("OPENAI_MODEL"), "gpt-5.6-terra"),
		ReasoningEffort: os.Getenv("OPENAI_REASONING_EFFORT"),
		ResponsesClient: client,
	}, nil
}

func (p *OpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *OpenAIProvider) FetchWithHistory(
	ctx context.Context,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	if p.APIType == OpenAIAPITypeResponses {
		return p.fetchResponses(ctx, input, systemPrompt, history)
	}

	return p.fetchChatCompletions(ctx, input, systemPrompt, history)
}

func (p *OpenAIProvider) fetchChatCompletions(
	ctx context.Context,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	return fetchChat(ctx, chatFetch{
		client: p.Client,
		empty:  errNoOpenAIChoices,
		fail:   failCreateChatCompletion,
		model:  p.Model,
		name:   "openai",
		effort: p.ReasoningEffort,
	}, input, systemPrompt, history)
}

func (p *OpenAIProvider) fetchResponses(
	ctx context.Context,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	logProviderRequest("openai", p.Model, systemPrompt, history, input)

	params := anyllm.ResponsesParams{
		Input:        buildResponsesInput(input, history),
		Instructions: systemPrompt,
		Model:        p.Model,
	}
	if p.ReasoningEffort != "" {
		params.Reasoning = reasoningEffort(p.ReasoningEffort)
	}

	resp, err := p.ResponsesClient.Responses(ctx, params)
	debug.Log("Received openai response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create response: %w", err)
	}
	if resp == nil || resp.Output == "" {
		if resp != nil && resp.ID != "" {
			return "", errEmptyOpenAIOutput
		}

		return "", errNoOpenAIOutput
	}

	return resp.Output, nil
}

func buildResponsesInput(input string, history []Message) []anyllm.ResponsesInputItem {
	items := make([]anyllm.ResponsesInputItem, 0, len(history)+1)
	for _, msg := range history {
		switch msg.Role {
		case roleUser:
			items = append(items, anyllm.ResponsesInputItem{Role: anyllm.RoleUser, Content: msg.Content})
		case roleAssistant:
			items = append(items, anyllm.ResponsesInputItem{Role: anyllm.RoleAssistant, Content: msg.Content})
		}
	}
	items = append(items, anyllm.ResponsesInputItem{Role: anyllm.RoleUser, Content: input})

	return items
}
