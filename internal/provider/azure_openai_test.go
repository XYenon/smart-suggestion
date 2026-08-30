package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

func TestNewAzureOpenAIProvider(t *testing.T) {
	os.Setenv("AZURE_OPENAI_API_KEY", "test-key")
	os.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME", "test-deployment")
	os.Setenv("AZURE_OPENAI_RESOURCE_NAME", "test-resource")
	defer os.Unsetenv("AZURE_OPENAI_API_KEY")
	defer os.Unsetenv("AZURE_OPENAI_DEPLOYMENT_NAME")
	defer os.Unsetenv("AZURE_OPENAI_RESOURCE_NAME")

	p, err := NewAzureOpenAIProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.DeploymentName != "test-deployment" {
		t.Errorf("expected deployment name test-deployment, got %s", p.DeploymentName)
	}

	for _, effort := range []string{"low", "medium", "high", "none", "minimal", "custom_effort"} {
		os.Setenv("AZURE_OPENAI_REASONING_EFFORT", effort)
		p, err = NewAzureOpenAIProvider()
		os.Unsetenv("AZURE_OPENAI_REASONING_EFFORT")
		if err != nil {
			t.Fatalf("unexpected error for reasoning effort %s: %v", effort, err)
		}
		if p.ReasoningEffort != effort {
			t.Errorf("expected reasoning effort %s, got %s", effort, p.ReasoningEffort)
		}
	}
}

func TestNewAzureOpenAIProvider_Errors(t *testing.T) {
	os.Unsetenv("AZURE_OPENAI_API_KEY")
	os.Unsetenv("AZURE_OPENAI_DEPLOYMENT_NAME")
	os.Unsetenv("AZURE_OPENAI_RESOURCE_NAME")
	os.Unsetenv("AZURE_OPENAI_BASE_URL")

	t.Run("missing api key", func(t *testing.T) {
		_, err := NewAzureOpenAIProvider()
		if err == nil || !strings.Contains(err.Error(), "AZURE_OPENAI_API_KEY") {
			t.Errorf("expected api key error, got %v", err)
		}
	})

	t.Run("missing deployment name", func(t *testing.T) {
		os.Setenv("AZURE_OPENAI_API_KEY", "test")
		defer os.Unsetenv("AZURE_OPENAI_API_KEY")
		_, err := NewAzureOpenAIProvider()
		if err == nil || !strings.Contains(err.Error(), "AZURE_OPENAI_DEPLOYMENT_NAME") {
			t.Errorf("expected deployment name error, got %v", err)
		}
	})

	t.Run("missing resource name", func(t *testing.T) {
		os.Setenv("AZURE_OPENAI_API_KEY", "test")
		os.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME", "test")
		defer os.Unsetenv("AZURE_OPENAI_API_KEY")
		defer os.Unsetenv("AZURE_OPENAI_DEPLOYMENT_NAME")
		_, err := NewAzureOpenAIProvider()
		if err == nil || !strings.Contains(err.Error(), "AZURE_OPENAI_RESOURCE_NAME") {
			t.Errorf("expected resource name error, got %v", err)
		}
	})
}

func TestAzureOpenAIProvider_Fetch(t *testing.T) {
	cases := []TestCase{
		{
			Name:           "successful suggestion",
			Input:          "how to list files",
			SystemPrompt:   "you are a shell assistant",
			ExpectedOutput: "=ls",
		},
		{
			Name:          "API error",
			Input:         "test",
			SystemPrompt:  "test",
			ExpectedError: "failed to create chat completion",
		},
		{
			Name:          "no choices",
			Input:         "test",
			SystemPrompt:  "test",
			ExpectedError: "no choices returned from Azure OpenAI API",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fake := &fakeCompletionClient{}
			switch tc.Name {
			case "successful suggestion":
				fake.response = completionFromContent("<reasoning>The user wants to list files.</reasoning>=ls")
			case "API error":
				fake.err = fmt.Errorf("access denied")
			case "no choices":
				fake.response = &anyllm.ChatCompletion{}
			}

			p := &AzureOpenAIProvider{
				Client:         fake,
				DeploymentName: "test-deployment",
			}

			resp, err := p.Fetch(t.Context(), tc.Input, tc.SystemPrompt)
			if tc.ExpectedError != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tc.ExpectedError)
				} else if !strings.Contains(err.Error(), tc.ExpectedError) {
					t.Errorf("expected error containing %q, got %q", tc.ExpectedError, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := ParseAndExtractCommand(resp)
			if got != tc.ExpectedOutput {
				t.Errorf("expected output %q, got %q (original response: %q)", tc.ExpectedOutput, got, resp)
			}
		})
	}
}

func TestAzureOpenAIProvider_Fetch_WithReasoningEffort(t *testing.T) {
	fake := &fakeCompletionClient{
		response: completionFromContent("=ls -la"),
	}
	p := &AzureOpenAIProvider{
		Client:          fake,
		DeploymentName:  "test-o3-mini",
		ReasoningEffort: "high",
	}

	resp, err := p.Fetch(t.Context(), "list files", "system prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "=ls -la" {
		t.Errorf("expected '=ls -la', got %q", resp)
	}
	if fake.last.ReasoningEffort != "high" {
		t.Errorf("expected reasoning effort high, got %s", fake.last.ReasoningEffort)
	}
}

func TestAzureOpenAIProvider_Fetch_DefaultAPIVersionWithReasoningEffort(t *testing.T) {
	os.Setenv("AZURE_OPENAI_API_KEY", "test-key")
	os.Setenv("AZURE_OPENAI_DEPLOYMENT_NAME", "test-deployment")
	os.Setenv("AZURE_OPENAI_BASE_URL", "https://example.openai.azure.com")
	os.Setenv("AZURE_OPENAI_REASONING_EFFORT", "medium")
	os.Unsetenv("AZURE_OPENAI_API_VERSION")
	os.Unsetenv("AZURE_OPENAI_RESOURCE_NAME")
	defer os.Unsetenv("AZURE_OPENAI_API_KEY")
	defer os.Unsetenv("AZURE_OPENAI_DEPLOYMENT_NAME")
	defer os.Unsetenv("AZURE_OPENAI_BASE_URL")
	defer os.Unsetenv("AZURE_OPENAI_REASONING_EFFORT")

	p, err := NewAzureOpenAIProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ReasoningEffort != "medium" {
		t.Errorf("expected reasoning effort medium, got %s", p.ReasoningEffort)
	}
}
