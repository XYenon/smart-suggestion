package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
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
			Name:         "successful suggestion",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"choices": [
					{
						"message": {
							"role": "assistant",
							"content": "<reasoning>The user wants to list files.</reasoning>=ls"
						}
					}
				]
			}`,
			ExpectedOutput: "=ls",
		},
		{
			Name:          "API error",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusForbidden,
			MockResponse:  `{"error": {"message": "access denied"}}`,
			ExpectedError: "failed to create chat completion",
		},
		{
			Name:          "no choices",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusOK,
			MockResponse:  `{"choices": []}`,
			ExpectedError: "no choices returned from Azure OpenAI API",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.MockStatus)
				fmt.Fprint(w, tc.MockResponse)
			}))
			defer server.Close()

			client := openai.NewClient(
				azure.WithEndpoint(server.URL, "2024-10-21"),
				azure.WithAPIKey("test-key"),
			)

			p := &AzureOpenAIProvider{
				DeploymentName: "test-deployment",
				Client:         &client,
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
