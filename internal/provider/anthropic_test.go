package provider

import (
	"fmt"
	"os"
	"strings"
	"testing"

	anyllm "github.com/mozilla-ai/any-llm-go"
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
			Name:           "successful suggestion",
			Input:          "how to list files",
			SystemPrompt:   "you are a shell assistant",
			ExpectedOutput: "=ls",
		},
		{
			Name:          "API error",
			Input:         "test",
			SystemPrompt:  "test",
			ExpectedError: "failed to create message",
		},
		{
			Name:          "no content",
			Input:         "test",
			SystemPrompt:  "test",
			ExpectedError: "no content returned from Anthropic API",
		},
		{
			Name:           "successful suggestion with thinking block",
			Input:          "how to list files",
			SystemPrompt:   "you are a shell assistant",
			ExpectedOutput: "=ls -la",
		},
		{
			Name:          "thinking block only (no text content)",
			Input:         "test",
			SystemPrompt:  "test",
			ExpectedError: "no content returned from Anthropic API",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			fake := &fakeCompletionClient{}
			switch tc.Name {
			case "successful suggestion":
				fake.response = completionFromContent("<reasoning>The user wants to list files.</reasoning>=ls")
			case "API error":
				fake.err = fmt.Errorf("invalid api key")
			case "no content":
				fake.response = &anyllm.ChatCompletion{}
			case "successful suggestion with thinking block":
				fake.response = &anyllm.ChatCompletion{
					Choices: []anyllm.Choice{{
						Message: anyllm.Message{
							Role:      anyllm.RoleAssistant,
							Content:   "<reasoning>The user wants to list files.</reasoning>=ls -la",
							Reasoning: &anyllm.Reasoning{Content: "Thinking about listing files..."},
						},
					}},
				}
			case "thinking block only (no text content)":
				fake.response = &anyllm.ChatCompletion{
					Choices: []anyllm.Choice{{
						Message: anyllm.Message{
							Role:      anyllm.RoleAssistant,
							Reasoning: &anyllm.Reasoning{Content: "Thinking only..."},
						},
					}},
				}
			}

			p := &AnthropicProvider{
				Client:          fake,
				Model:           "claude-3-5-sonnet-20241022",
				ReasoningEffort: "low",
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
	fake := &fakeCompletionClient{
		response: completionFromContent("=ls"),
	}
	p := &AnthropicProvider{
		Client:          fake,
		Model:           "claude-sonnet-5",
		ReasoningEffort: "high",
	}
	if _, err := p.Fetch(t.Context(), "list files", "system prompt"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.last.ReasoningEffort != "high" {
		t.Errorf("expected reasoning effort high, got %s", fake.last.ReasoningEffort)
	}
}
