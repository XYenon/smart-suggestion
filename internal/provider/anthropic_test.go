package provider

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func TestNewAnthropicProvider(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "test-key")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	p, err := NewAnthropicProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected model claude-3-5-sonnet-20241022, got %s", p.Model)
	}
}

func TestNewAnthropicProvider_Errors(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	_, err := NewAnthropicProvider()
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("expected api key error, got %v", err)
	}
}

func TestAnthropicProvider_Fetch(t *testing.T) {
	cases := []TestCase{
		{
			Name:         "successful suggestion",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"model": "claude-3-5-sonnet-20241022",
				"content": [
					{
						"type": "text",
						"text": "<reasoning>The user wants to list files.</reasoning>=ls"
					}
				],
				"stop_reason": "end_turn"
			}`,
			ExpectedOutput: "=ls",
		},
		{
			Name:          "API error",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusUnauthorized,
			MockResponse:  `{"error": {"type": "authentication_error", "message": "invalid api key"}}`,
			ExpectedError: "failed to create message",
		},
		{
			Name:         "no content",
			Input:        "test",
			SystemPrompt: "test",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "msg_456",
				"type": "message",
				"role": "assistant",
				"model": "claude-3-5-sonnet-20241022",
				"content": [],
				"stop_reason": "end_turn"
			}`,
			ExpectedError: "no content returned from Anthropic API",
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

			client := anthropic.NewClient(
				option.WithAPIKey("test-key"),
				option.WithBaseURL(server.URL),
			)

			p := &AnthropicProvider{
				Model:  "claude-3-5-sonnet-20241022",
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
