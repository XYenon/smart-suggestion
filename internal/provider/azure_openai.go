package provider

import (
	"context"
	"fmt"
	"os"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/azureopenai"
	"github.com/xyenon/smart-suggestion/internal/debug"
)

type AzureOpenAIProvider struct {
	Client          completionClient
	DeploymentName  string
	ReasoningEffort string
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

	var endpoint string
	if baseURL != "" {
		endpoint = normalizeBaseURL(baseURL)
	} else {
		endpoint = fmt.Sprintf("https://%s.openai.azure.com", resourceName)
	}

	client, err := azureopenai.New(
		anyllm.WithAPIKey(apiKey),
		anyllm.WithBaseURL(endpoint),
		anyllm.WithExtra("api_version", apiVersion),
	)
	if err != nil {
		return nil, err
	}

	return &AzureOpenAIProvider{
		Client:          client,
		DeploymentName:  deploymentName,
		ReasoningEffort: os.Getenv("AZURE_OPENAI_REASONING_EFFORT"),
	}, nil
}

func (p *AzureOpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	return p.FetchWithHistory(ctx, input, systemPrompt, nil)
}

func (p *AzureOpenAIProvider) FetchWithHistory(ctx context.Context, input string, systemPrompt string, history []Message) (string, error) {
	logProviderRequest("azure_openai", p.DeploymentName, systemPrompt, history, input)

	params := anyllm.CompletionParams{
		Model:    p.DeploymentName,
		Messages: buildCompletionMessages(systemPrompt, input, history),
	}
	if p.ReasoningEffort != "" {
		params.ReasoningEffort = reasoningEffort(p.ReasoningEffort)
	}

	resp, err := p.Client.Completion(ctx, params)
	debug.Log("Received Azure OpenAI response", map[string]any{
		"response": resp,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	return extractCompletionText(resp, fmt.Errorf("no choices returned from Azure OpenAI API"))
}
