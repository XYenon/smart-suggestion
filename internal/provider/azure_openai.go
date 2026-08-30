package provider

import (
	"context"
	"fmt"
	"os"

	anyllm "github.com/mozilla-ai/any-llm-go"
	"github.com/mozilla-ai/any-llm-go/providers/azureopenai"
)

type AzureOpenAIProvider struct {
	Client          completionClient
	DeploymentName  string
	ReasoningEffort string
}

func NewAzureOpenAIProvider() (*AzureOpenAIProvider, error) {
	apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errMissingAzureAPIKey
	}

	deploymentName := os.Getenv("AZURE_OPENAI_DEPLOYMENT_NAME")
	if deploymentName == "" {
		return nil, errMissingAzureDeployment
	}

	baseURL := os.Getenv("AZURE_OPENAI_BASE_URL")
	resourceName := os.Getenv("AZURE_OPENAI_RESOURCE_NAME")
	if baseURL == "" && resourceName == "" {
		return nil, errMissingAzureResource
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
		return nil, fmt.Errorf("creating Azure OpenAI client: %w", err)
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

func (p *AzureOpenAIProvider) FetchWithHistory(
	ctx context.Context,
	input string,
	systemPrompt string,
	history []Message,
) (string, error) {
	return fetchChat(ctx, chatFetch{
		client: p.Client,
		empty:  errNoAzureChoices,
		fail:   failCreateChatCompletion,
		model:  p.DeploymentName,
		name:   "azure_openai",
		effort: p.ReasoningEffort,
	}, input, systemPrompt, history)
}
