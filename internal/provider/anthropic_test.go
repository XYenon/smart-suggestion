package provider

import (
	"encoding/json"
	"fmt"
	"io"
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
	if p.Model != "claude-sonnet-5" {
		t.Errorf("expected model claude-sonnet-5, got %s", p.Model)
	}
	if p.ReasoningEffort != "" {
		t.Errorf("expected default reasoning effort to be empty, got %s", p.ReasoningEffort)
	}

	// With ANTHROPIC_REASONING_EFFORT
	for _, effort := range []string{"low", "medium", "high", "none", "minimal", "custom_effort"} {
		os.Setenv("ANTHROPIC_REASONING_EFFORT", effort)
		p, err = NewAnthropicProvider()
		os.Unsetenv("ANTHROPIC_REASONING_EFFORT")
		if err != nil {
			t.Fatalf("unexpected error for reasoning effort %s: %v", effort, err)
		}
		if p.ReasoningEffort != effort {
			t.Errorf("expected reasoning effort %s, got %s", effort, p.ReasoningEffort)
		}
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
		{
			Name:         "successful suggestion with thinking block",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "msg_789",
				"type": "message",
				"role": "assistant",
				"model": "claude-3-7-sonnet-20250219",
				"content": [
					{
						"type": "thinking",
						"thinking": "Thinking about listing files..."
					},
					{
						"type": "text",
						"text": "<reasoning>The user wants to list files.</reasoning>=ls -la"
					}
				],
				"stop_reason": "end_turn"
			}`,
			ExpectedOutput: "=ls -la",
		},
		{
			Name:         "thinking block only (no text content)",
			Input:        "test",
			SystemPrompt: "test",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "msg_999",
				"type": "message",
				"role": "assistant",
				"model": "claude-3-7-sonnet-20250219",
				"content": [
					{
						"type": "thinking",
						"thinking": "Thinking only..."
					}
				],
				"stop_reason": "end_turn"
			}`,
			ExpectedError: "no text content returned from Anthropic API",
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
				Model:           "claude-3-5-sonnet-20241022",
				ReasoningEffort: "low",
				Client:          &client,
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

func TestAnthropicProvider_Fetch_MaxTokens(t *testing.T) {
	// max_tokens caps thinking plus answer text, so it stays at 4096 whether
	// or not reasoning effort is configured.
	const expectedMaxTokens float64 = 4096

	cases := []struct {
		name            string
		reasoningEffort string
	}{
		{name: "without reasoning effort", reasoningEffort: ""},
		{name: "with reasoning effort", reasoningEffort: "high"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var requestBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Errorf("failed to read request body: %v", err)
				}
				if err := json.Unmarshal(body, &requestBody); err != nil {
					t.Errorf("failed to unmarshal request body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, `{
					"id": "msg_max_tokens",
					"type": "message",
					"role": "assistant",
					"model": "claude-sonnet-5",
					"content": [
						{
							"type": "text",
							"text": "=ls"
						}
					],
					"stop_reason": "end_turn"
				}`)
			}))
			defer server.Close()

			client := anthropic.NewClient(
				option.WithAPIKey("test-key"),
				option.WithBaseURL(server.URL),
			)

			p := &AnthropicProvider{
				Model:           "claude-sonnet-5",
				ReasoningEffort: tc.reasoningEffort,
				Client:          &client,
			}

			if _, err := p.Fetch(t.Context(), "list files", "system prompt"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			maxTokens, ok := requestBody["max_tokens"].(float64)
			if !ok {
				t.Fatalf("expected max_tokens in request body, got %v", requestBody["max_tokens"])
			}
			if maxTokens != expectedMaxTokens {
				t.Errorf("expected max_tokens %.0f, got %.0f", expectedMaxTokens, maxTokens)
			}
		})
	}
}
