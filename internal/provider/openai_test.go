package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

func TestNewOpenAIProvider(t *testing.T) {
	os.Setenv("OPENAI_API_KEY", "test-key")
	defer os.Unsetenv("OPENAI_API_KEY")

	p, err := NewOpenAIProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gpt-5.6-terra" {
		t.Errorf("expected default model gpt-5.6-terra, got %s", p.Model)
	}
	if p.APIType != OpenAIAPITypeChatCompletions {
		t.Errorf("expected default api type chat_completions, got %s", p.APIType)
	}

	os.Setenv("OPENAI_API_TYPE", "chat_completions")
	p, err = NewOpenAIProvider()
	os.Unsetenv("OPENAI_API_TYPE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.APIType != OpenAIAPITypeChatCompletions {
		t.Errorf("expected api type chat_completions, got %s", p.APIType)
	}

	os.Setenv("OPENAI_API_TYPE", "responses")
	p, err = NewOpenAIProvider()
	os.Unsetenv("OPENAI_API_TYPE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.APIType != OpenAIAPITypeResponses {
		t.Errorf("expected api type responses, got %s", p.APIType)
	}

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

	for _, effort := range []string{"low", "medium", "high", "none", "minimal", "custom_effort"} {
		os.Setenv("OPENAI_REASONING_EFFORT", effort)
		p, err = NewOpenAIProvider()
		os.Unsetenv("OPENAI_REASONING_EFFORT")
		if err != nil {
			t.Fatalf("unexpected error for reasoning effort %s: %v", effort, err)
		}
		if p.ReasoningEffort != effort {
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
			Name:           "successful command suggestion",
			Input:          "how to list files",
			SystemPrompt:   "you are a shell assistant",
			ExpectedOutput: "=ls -l",
		},
		{
			Name:           "successful completion suggestion",
			Input:          "ls -",
			SystemPrompt:   "you are a shell assistant",
			ExpectedOutput: "+la",
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
			ExpectedError: "no choices returned from OpenAI API",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fake := &fakeCompletionClient{}
			switch tc.Name {
			case "successful command suggestion":
				fake.response = completionFromContent("<reasoning>The user wants to list files.</reasoning>=ls -l")
			case "successful completion suggestion":
				fake.response = completionFromContent("<reasoning>The user is typing ls and wants completion.</reasoning>+la")
			case "API error":
				fake.err = fmt.Errorf("invalid api key")
			case "no choices":
				fake.response = &anyllm.ChatCompletion{}
			}

			p := &OpenAIProvider{
				APIType: OpenAIAPITypeChatCompletions,
				Client:  fake,
				Model:   "gpt-4o-mini",
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

func TestOpenAIProvider_Fetch_Responses(t *testing.T) {
	cases := []struct {
		Name           string
		Input          string
		SystemPrompt   string
		History        []Message
		Response       *anyllm.ResponsesResult
		Err            error
		ExpectedOutput string
		ExpectedError  string
	}{
		{
			Name:         "successful command suggestion",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			Response: &anyllm.ResponsesResult{
				ID:     "resp_123",
				Output: "<reasoning>The user wants to list files.</reasoning>=ls -l",
			},
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
			Response: &anyllm.ResponsesResult{
				ID:     "resp_456",
				Output: "<reasoning>The user is typing ls and wants completion.</reasoning>+la",
			},
			ExpectedOutput: "+la",
		},
		{
			Name:          "API error",
			Input:         "test",
			SystemPrompt:  "test",
			Err:           fmt.Errorf("invalid api key"),
			ExpectedError: "failed to create response",
		},
		{
			Name:          "no output",
			Input:         "test",
			SystemPrompt:  "test",
			Response:      &anyllm.ResponsesResult{},
			ExpectedError: "no output returned from OpenAI API",
		},
		{
			Name:          "empty output text",
			Input:         "test",
			SystemPrompt:  "test",
			Response:      &anyllm.ResponsesResult{ID: "resp_000"},
			ExpectedError: "empty output text returned from OpenAI API",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fake := &fakeResponsesClient{response: tc.Response, err: tc.Err}
			p := &OpenAIProvider{
				APIType:         OpenAIAPITypeResponses,
				Model:           "gpt-4o-mini",
				ResponsesClient: fake,
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

func TestBuildResponsesInput(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "cmd 1"},
		{Role: "assistant", Content: "result 1"},
		{Role: "system", Content: "ignore this"},
	}
	items := buildResponsesInput("cmd 2", history)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

func TestOpenAIProvider_Fetch_WithReasoningEffort(t *testing.T) {
	t.Run("chat_completions with reasoning effort", func(t *testing.T) {
		fake := &fakeCompletionClient{response: completionFromContent("=ls -la")}
		p := &OpenAIProvider{
			APIType:         OpenAIAPITypeChatCompletions,
			Client:          fake,
			Model:           "o3-mini",
			ReasoningEffort: "low",
		}

		resp, err := p.Fetch(t.Context(), "list files", "system prompt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "=ls -la" {
			t.Errorf("expected '=ls -la', got %q", resp)
		}
		if fake.last.ReasoningEffort != "low" {
			t.Errorf("expected reasoning effort low, got %s", fake.last.ReasoningEffort)
		}
	})

	t.Run("responses with reasoning effort", func(t *testing.T) {
		fake := &fakeResponsesClient{response: &anyllm.ResponsesResult{ID: "resp_123", Output: "=ls -la"}}
		p := &OpenAIProvider{
			APIType:         OpenAIAPITypeResponses,
			Model:           "o3-mini",
			ReasoningEffort: "medium",
			ResponsesClient: fake,
		}

		resp, err := p.Fetch(t.Context(), "list files", "system prompt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != "=ls -la" {
			t.Errorf("expected '=ls -la', got %q", resp)
		}
		if fake.last.Reasoning != "medium" {
			t.Errorf("expected reasoning effort medium, got %s", fake.last.Reasoning)
		}
	})
}
