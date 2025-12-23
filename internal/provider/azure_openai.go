package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/yetone/smart-suggestion/internal/debug"
)

type AzureOpenAIProvider struct {
	DeploymentName string
	Client         *openai.Client
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

	apiVersion := os.Getenv("AZURE_OPENAI_API_VERSION")
	if apiVersion == "" {
		apiVersion = "2024-10-21"
	}

	var endpoint string
	if baseURL != "" {
		endpoint = strings.TrimSuffix(baseURL, "/")
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			endpoint = "https://" + endpoint
		}
	} else {
		endpoint = fmt.Sprintf("https://%s.openai.azure.com", resourceName)
	}

	client := openai.NewClient(
		azure.WithEndpoint(endpoint, apiVersion),
		azure.WithAPIKey(apiKey),
	)

	return &AzureOpenAIProvider{
		DeploymentName: deploymentName,
		Client:         &client,
	}, nil
}

func (p *AzureOpenAIProvider) Fetch(ctx context.Context, input string, systemPrompt string) (string, error) {
	debug.Log("Sending Azure OpenAI request", map[string]any{
		"deployment": p.DeploymentName,
	})

	resp, err := p.Client.Chat.Completions.New(
		ctx,
		openai.ChatCompletionNewParams{
			Model: openai.ChatModel(p.DeploymentName),
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemPrompt),
				openai.UserMessage(input),
			},
		},
	)

	if err != nil {
		return "", fmt.Errorf("failed to create chat completion: %w", err)
	}

	rawResp, _ := json.Marshal(resp)
	debug.Log("Received Azure OpenAI response", map[string]any{
		"response": string(rawResp),
	})

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from Azure OpenAI API")
	}

	return resp.Choices[0].Message.Content, nil
}
