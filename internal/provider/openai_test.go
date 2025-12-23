package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func TestNewOpenAIProvider(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	p, err := NewOpenAIProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gpt-4o-mini" {
		t.Errorf("expected default model gpt-4o-mini, got %s", p.Model)
	}
}

func TestNewOpenAIProvider_Errors(t *testing.T) {
	os.Unsetenv("OPENAI_API_KEY")
	_, err := NewOpenAIProvider()
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("expected api key error, got %v", err)
	}
}

func TestOpenAIProvider_Fetch(t *testing.T) {
	cases := []TestCase{
		{
			Name:         "successful command suggestion",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"created": 1677652288,
				"model": "gpt-4o-mini",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "<reasoning>The user wants to list files.</reasoning>=ls -l"
						},
						"finish_reason": "stop"
					}
				],
				"usage": {
					"prompt_tokens": 9,
					"completion_tokens": 12,
					"total_tokens": 21
				}
			}`,
			ExpectedOutput: "=ls -l",
		},
		{
			Name:         "successful completion suggestion",
			Input:        "ls -",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "chatcmpl-456",
				"object": "chat.completion",
				"created": 1677652288,
				"model": "gpt-4o-mini",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "<reasoning>The user is typing ls and wants completion.</reasoning>+la"
						},
						"finish_reason": "stop"
					}
				]
			}`,
			ExpectedOutput: "+la",
		},
		{
			Name:          "API error",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusBadRequest,
			MockResponse:  `{"error": {"message": "invalid api key"}}`,
			ExpectedError: "failed to create chat completion",
		},
		{
			Name:          "malformed JSON",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusOK,
			MockResponse:  `{"choices": [{"message": {"content": "broken`,
			ExpectedError: "failed to create chat completion",
		},
		{
			Name:         "no choices",
			Input:        "test",
			SystemPrompt: "test",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "chatcmpl-789",
				"object": "chat.completion",
				"choices": []
			}`,
			ExpectedError: "no choices returned from OpenAI API",
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
				option.WithAPIKey("test-key"),
				option.WithBaseURL(server.URL),
			)

			p := &OpenAIProvider{
				Model:  "gpt-4o-mini",
				Client: &client,
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
