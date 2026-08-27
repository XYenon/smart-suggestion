package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
)

func TestNewGeminiProvider(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	p, err := NewGeminiProvider(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gemini-3.7-flash" {
		t.Errorf("expected default model gemini-3.7-flash, got %s", p.Model)
	}
}

func TestNewGeminiProvider_WithCustomModel(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	os.Setenv("GEMINI_MODEL", "gemini-1.5-pro")
	defer func() {
		os.Unsetenv("GEMINI_API_KEY")
		os.Unsetenv("GEMINI_MODEL")
	}()

	p, err := NewGeminiProvider(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gemini-1.5-pro" {
		t.Errorf("expected model gemini-1.5-pro, got %s", p.Model)
	}
}

func TestNewGeminiProvider_WithBaseURL(t *testing.T) {
	testCases := []struct {
		name    string
		baseURL string
	}{
		{"with_https", "https://custom-api.example.com"},
		{"no_protocol", "custom-api.example.com"},
		{"trailing_slash", "https://custom-api.example.com/"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv("GEMINI_API_KEY", "test-key")
			os.Setenv("GEMINI_BASE_URL", tc.baseURL)
			defer func() {
				os.Unsetenv("GEMINI_API_KEY")
				os.Unsetenv("GEMINI_BASE_URL")
			}()

			p, err := NewGeminiProvider(t.Context())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Model != "gemini-3.7-flash" {
				t.Errorf("expected default model gemini-3.7-flash, got %s", p.Model)
			}
		})
	}
}

func TestNewGeminiProvider_WithThinkingConfig(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	for _, level := range []string{"minimal", "low", "medium", "high", "custom_level", "LOW", "High"} {
		os.Setenv("GEMINI_THINKING_LEVEL", level)
		p, err := NewGeminiProvider(t.Context())
		os.Unsetenv("GEMINI_THINKING_LEVEL")
		if err != nil {
			t.Fatalf("unexpected error for thinking level %s: %v", level, err)
		}
		if p.ThinkingLevel != strings.ToUpper(level) {
			t.Fatalf("expected ThinkingLevel to be %s, got %s", strings.ToUpper(level), p.ThinkingLevel)
		}
	}
}

func TestNewGeminiProvider_Errors(t *testing.T) {
	os.Unsetenv("GEMINI_API_KEY")
	_, err := NewGeminiProvider(t.Context())
	if err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("expected api key error, got %v", err)
	}
}

func TestGeminiProvider_Fetch(t *testing.T) {
	fake := &fakeCompletionClient{response: completionFromContent("=ls")}
	p := &GeminiProvider{
		Client: fake,
		Model:  "gemini-2.5-flash",
	}

	result, err := p.Fetch(t.Context(), "test input", "test prompt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "=ls" {
		t.Errorf("expected '=ls', got %q", result)
	}
}

func TestGeminiProvider_FetchWithHistory_MockedResponses(t *testing.T) {
	testCases := []struct {
		name          string
		response      *anyllm.ChatCompletion
		err           error
		expectError   bool
		errorContains string
	}{
		{
			name:        "successful_response",
			response:    completionFromContent("=ls -la"),
			expectError: false,
		},
		{
			name: "successful_response_with_thoughts",
			response: &anyllm.ChatCompletion{
				Choices: []anyllm.Choice{{
					Message: anyllm.Message{
						Role:      anyllm.RoleAssistant,
						Content:   "=ls -la",
						Reasoning: &anyllm.Reasoning{Content: "Thinking about listing files"},
					},
				}},
			},
			expectError: false,
		},
		{
			name: "only_thoughts_no_text",
			response: &anyllm.ChatCompletion{
				Choices: []anyllm.Choice{{
					Message: anyllm.Message{
						Role:      anyllm.RoleAssistant,
						Reasoning: &anyllm.Reasoning{Content: "Thinking only"},
					},
				}},
			},
			expectError:   true,
			errorContains: "no candidates returned",
		},
		{
			name:          "no_candidates",
			response:      &anyllm.ChatCompletion{},
			expectError:   true,
			errorContains: "no candidates returned",
		},
		{
			name:          "empty_text",
			response:      completionFromContent(""),
			expectError:   true,
			errorContains: "no candidates returned",
		},
		{
			name:          "api_error",
			err:           fmt.Errorf("API key not valid"),
			expectError:   true,
			errorContains: "failed to send message",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := &GeminiProvider{
				Client: &fakeCompletionClient{response: tc.response, err: tc.err},
				Model:  "gemini-2.5-flash",
			}

			history := []Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			}
			result, err := p.FetchWithHistory(t.Context(), "test input", "test prompt", history)
			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errorContains, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if result == "" {
				t.Error("expected non-empty result")
			}
		})
	}
}

func TestGeminiProvider_FetchWithHistory_Scenarios(t *testing.T) {
	success := completionFromContent("=echo test")
	testCases := []struct {
		name         string
		input        string
		systemPrompt string
		history      []Message
	}{
		{name: "empty_system_prompt", input: "test", systemPrompt: "", history: nil},
		{name: "with_system_prompt", input: "test", systemPrompt: "You are helpful", history: nil},
		{
			name:         "mixed_roles_filtering",
			input:        "test",
			systemPrompt: "system",
			history: []Message{
				{Role: "user", Content: "Valid user"},
				{Role: "system", Content: "Should be filtered"},
				{Role: "assistant", Content: "Valid assistant"},
				{Role: "unknown", Content: "Should be filtered"},
				{Role: "", Content: "Empty role filtered"},
			},
		},
		{
			name:         "only_invalid_roles",
			input:        "test",
			systemPrompt: "system",
			history: []Message{
				{Role: "invalid", Content: "skip1"},
				{Role: "moderator", Content: "skip2"},
			},
		},
		{
			name:         "alternating_valid_roles",
			input:        "test",
			systemPrompt: "system",
			history: []Message{
				{Role: "user", Content: "User 1"},
				{Role: "assistant", Content: "Assistant 1"},
				{Role: "user", Content: "User 2"},
				{Role: "assistant", Content: "Assistant 2"},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := &GeminiProvider{
				Client: &fakeCompletionClient{response: success},
				Model:  "gemini-2.5-flash",
			}
			if _, err := p.FetchWithHistory(t.Context(), tc.input, tc.systemPrompt, tc.history); err != nil {
				t.Errorf("unexpected error for %s: %v", tc.name, err)
			}
		})
	}
}

func TestGeminiProvider_FetchWithThinkingConfig(t *testing.T) {
	fake := &fakeCompletionClient{
		response: &anyllm.ChatCompletion{
			Choices: []anyllm.Choice{{
				Message: anyllm.Message{
					Role:      anyllm.RoleAssistant,
					Content:   "=echo test",
					Reasoning: &anyllm.Reasoning{Content: "Thinking..."},
				},
			}},
		},
	}
	p := &GeminiProvider{
		Client:        fake,
		Model:         "gemini-2.5-flash",
		ThinkingLevel: "HIGH",
	}

	resp, err := p.Fetch(t.Context(), "test", "system")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp != "=echo test" {
		t.Errorf("expected '=echo test', got %q", resp)
	}
	if fake.last.ReasoningEffort != "high" {
		t.Errorf("expected reasoning effort high, got %s", fake.last.ReasoningEffort)
	}
}
