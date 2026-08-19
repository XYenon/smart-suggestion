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
	"github.com/openai/openai-go/responses"
)

func TestNewOpenAIProvider(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	// Default configuration
	p, err := NewOpenAIProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gpt-4o-mini" {
		t.Errorf("expected default model gpt-4o-mini, got %s", p.Model)
	}
	if p.APIType != OpenAIAPITypeChatCompletions {
		t.Errorf("expected default api type chat_completions, got %s", p.APIType)
	}

	// With OPENAI_API_TYPE=chat_completions
	os.Setenv("OPENAI_API_TYPE", "chat_completions")
	p, err = NewOpenAIProvider()
	os.Unsetenv("OPENAI_API_TYPE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.APIType != OpenAIAPITypeChatCompletions {
		t.Errorf("expected api type chat_completions, got %s", p.APIType)
	}

	// With OPENAI_API_TYPE=responses
	os.Setenv("OPENAI_API_TYPE", "responses")
	p, err = NewOpenAIProvider()
	os.Unsetenv("OPENAI_API_TYPE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.APIType != OpenAIAPITypeResponses {
		t.Errorf("expected api type responses, got %s", p.APIType)
	}

	// With custom base URL and model
	os.Setenv("OPENAI_BASE_URL", "https://api.custom.com/v1")
	os.Setenv("OPENAI_MODEL", "gpt-4o")
	p, err = NewOpenAIProvider()
	os.Unsetenv("OPENAI_BASE_URL")
	os.Unsetenv("OPENAI_MODEL")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", p.Model)
	}

	// With OPENAI_REASONING_EFFORT
	for _, effort := range []string{"low", "medium", "high", "none", "minimal", "custom_effort"} {
		os.Setenv("OPENAI_REASONING_EFFORT", effort)
		p, err = NewOpenAIProvider()
		os.Unsetenv("OPENAI_REASONING_EFFORT")
		if err != nil {
			t.Fatalf("unexpected error for reasoning effort %s: %v", effort, err)
		}
		if string(p.ReasoningEffort) != effort {
			t.Errorf("expected reasoning effort %s, got %s", effort, p.ReasoningEffort)
		}
	}
}

func TestNewOpenAIProvider_Errors(t *testing.T) {
	os.Unsetenv("OPENAI_API_KEY")
	_, err := NewOpenAIProvider()
	if err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Errorf("expected api key error, got %v", err)
	}

	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	invalidTypes := []string{"invalid_type", "chat", "response", "chat_completion", "responses "}
	for _, it := range invalidTypes {
		os.Setenv("OPENAI_API_TYPE", it)
		_, err = NewOpenAIProvider()
		os.Unsetenv("OPENAI_API_TYPE")
		if err == nil || !strings.Contains(err.Error(), "unsupported OPENAI_API_TYPE") {
			t.Errorf("expected unsupported OPENAI_API_TYPE error for %q, got %v", it, err)
		}
	}
}

func TestOpenAIProvider_Fetch_ChatCompletions(t *testing.T) {
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
				Model:   "gpt-4o-mini",
				APIType: OpenAIAPITypeChatCompletions,
				Client:  &client,
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

type openAITestCase struct {
	Name           string
	Input          string
	SystemPrompt   string
	History        []Message
	MockResponse   string
	MockStatus     int
	ExpectedOutput string
	ExpectedError  string
}

func TestOpenAIProvider_Fetch_Responses(t *testing.T) {
	cases := []openAITestCase{
		{
			Name:         "successful command suggestion",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "resp_123",
				"object": "response",
				"created_at": 1740000000,
				"status": "completed",
				"model": "gpt-4o-mini",
				"output": [
					{
						"id": "msg_123",
						"type": "message",
						"status": "completed",
						"role": "assistant",
						"content": [
							{
								"type": "output_text",
								"text": "<reasoning>The user wants to list files.</reasoning>=ls -l"
							}
						]
					}
				],
				"usage": {
					"input_tokens": 9,
					"output_tokens": 12,
					"total_tokens": 21
				}
			}`,
			ExpectedOutput: "=ls -l",
		},
		{
			Name:         "successful completion suggestion with history",
			Input:        "ls -",
			SystemPrompt: "you are a shell assistant",
			History: []Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "world"},
			},
			MockStatus: http.StatusOK,
			MockResponse: `{
				"id": "resp_456",
				"object": "response",
				"created_at": 1740000000,
				"status": "completed",
				"model": "gpt-4o-mini",
				"output": [
					{
						"id": "msg_456",
						"type": "message",
						"status": "completed",
						"role": "assistant",
						"content": [
							{
								"type": "output_text",
								"text": "<reasoning>The user is typing ls and wants completion.</reasoning>+la"
							}
						]
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
			ExpectedError: "failed to create response",
		},
		{
			Name:          "malformed JSON",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusOK,
			MockResponse:  `{"output": [{"content": [{"text": "broken`,
			ExpectedError: "failed to create response",
		},
		{
			Name:         "no output",
			Input:        "test",
			SystemPrompt: "test",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "resp_789",
				"object": "response",
				"status": "completed",
				"output": []
			}`,
			ExpectedError: "no output returned from OpenAI API",
		},
		{
			Name:         "empty output text",
			Input:        "test",
			SystemPrompt: "test",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"id": "resp_000",
				"object": "response",
				"status": "completed",
				"output": [
					{
						"id": "msg_000",
						"type": "message",
						"status": "completed",
						"role": "assistant",
						"content": [
							{
								"type": "output_text",
								"text": ""
							}
						]
					}
				]
			}`,
			ExpectedError: "empty output text returned from OpenAI API",
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
				Model:   "gpt-4o-mini",
				APIType: OpenAIAPITypeResponses,
				Client:  &client,
			}

			var resp string
			var err error
			if tc.History != nil {
				resp, err = p.FetchWithHistory(t.Context(), tc.Input, tc.SystemPrompt, tc.History)
			} else {
				resp, err = p.Fetch(t.Context(), tc.Input, tc.SystemPrompt)
			}

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

func TestBuildOpenAIResponseInput(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "cmd 1"},
		{Role: "assistant", Content: "result 1"},
		{Role: "system", Content: "ignore this"},
	}
	input := "cmd 2"

	items := buildOpenAIResponseInput(input, history)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Verify items are valid ResponseInputParam
	param := responses.ResponseInputParam(items)
	if len(param) != 3 {
		t.Fatalf("expected 3 items in param, got %d", len(param))
	}
}

func TestOpenAIProvider_Fetch_WithReasoningEffort(t *testing.T) {
	t.Run("chat_completions with reasoning effort", func(t *testing.T) {
		var capturedBody string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
				"id": "chatcmpl-123",
				"object": "chat.completion",
				"created": 1677652288,
				"model": "o3-mini",
				"choices": [
					{
						"index": 0,
						"message": {
							"role": "assistant",
							"content": "=ls -la"
						},
						"finish_reason": "stop"
					}
				]
			}`)
		}))
		defer server.Close()

		client := openai.NewClient(
			option.WithAPIKey("test-key"),
			option.WithBaseURL(server.URL),
		)

		p := &OpenAIProvider{
			Model:           "o3-mini",
			APIType:         OpenAIAPITypeChatCompletions,
			ReasoningEffort: "low",
			Client:          &client,
		}

		resp, err := p.Fetch(t.Context(), "list files", "system prompt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "=ls -la" {
			t.Errorf("expected '=ls -la', got %q", resp)
		}
		_ = capturedBody
	})

	t.Run("responses with reasoning effort", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{
				"id": "resp_123",
				"object": "response",
				"created_at": 1740000000,
				"status": "completed",
				"model": "o3-mini",
				"output": [
					{
						"id": "msg_123",
						"type": "message",
						"status": "completed",
						"role": "assistant",
						"content": [
							{
								"type": "output_text",
								"text": "=ls -la"
							}
						]
					}
				]
			}`)
		}))
		defer server.Close()

		client := openai.NewClient(
			option.WithAPIKey("test-key"),
			option.WithBaseURL(server.URL),
		)

		p := &OpenAIProvider{
			Model:           "o3-mini",
			APIType:         OpenAIAPITypeResponses,
			ReasoningEffort: "medium",
			Client:          &client,
		}

		resp, err := p.Fetch(t.Context(), "list files", "system prompt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "=ls -la" {
			t.Errorf("expected '=ls -la', got %q", resp)
		}
	})
}
