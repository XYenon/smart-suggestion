package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/openai/openai-go/shared"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type AzureOpenAIProvider struct {
	DeploymentName  string
	ReasoningEffort shared.ReasoningEffort
	Client          *openai.Client
}

func NewAzureOpenAIProvider() (*AzureOpenAIProvider, error) {
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_API_KEY environment variable is not set")
	}

	deploymentName := os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME")
	if deploymentName == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_DEPLOYMENT_NAME environment variable is not set")
	}

	baseURL := os.Getenv("AZURE_OPENAI_BASE_URL")
	resourceName := os.Getenv("AZURE_OPENAI_RESOURCE_NAME")

	if baseURL == "" && resourceName == "" {
		return nil, fmt.Errorf("AZURE_OPENAI_RESOURCE_NAME environment variable is not set")
	}

	// 2025-04-01-preview is the latest dated api-version and supports the
	// reasoning_effort parameter for reasoning model deployments.
	apiVersion := envOrDefault(os.Getenv("AZURE_OPENAI_API_VERSION"), "2025-04-01-preview")

	reasoningEffort := shared.ReasoningEffort(os.Getenv("AZURE_OPENAI_REASONING_EFFORT"))

	var endpoint string
	if baseURL != "" {
		endpoint = normalizeBaseURL(baseURL)
	} else {
		endpoint = fmt.Sprintf("https://%s.openai.azure.com", resourceName)
	}

	client := openai.NewClient(
		azure.WithEndpoint(endpoint, apiVersion),
		azure.WithAPIKey(apiKey),
	)

	return &AzureOpenAIProvider{
		DeploymentName:  deploymentName,
		ReasoningEffort: reasoningEffort,
		Client:          &client,
	}, nil
}

func (p *AzureOpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *AzureOpenAIProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("azure_openai", p.DeploymentName, systemPrompt, history, input)

	messages := buildOpenAIChatMessages(systemPrompt, input, history)

	params := openai.ChatCompletionNewParams{
		Model:    openai.ChatModel(p.DeploymentName),
		Messages: messages,
	}
	if p.ReasoningEffort != "" {
		params.ReasoningEffort = p.ReasoningEffort
	}

	resp, err := p.Client.Chat.Completions.New(ctx, params)
	debug.Log("Received Azure OpenAI response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from Azure OpenAI API")
	}

	return resp.Choices[0].Message.Content, nil
}
