package provider

import (
	"strings"
	"testing"
)

type TestCase struct {
	Name           string
	Input          string
	SystemPrompt   string
	MockResponse   string // Raw JSON response from API
	MockStatus     int
	ExpectedOutput string
	ExpectedError  string // Substring match
}

// SetupProviderFunc is a function that creates a provider with the given mock behavior
type SetupProviderFunc func(t *testing.T, tc TestCase) Provider

func RunProviderTests(t *testing.T, setup SetupProviderFunc, cases []TestCase) {
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			p := setup(t, tc)
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

func TestParseAndExtractCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no reasoning",
			input:    "ls -la",
			expected: "ls -la",
		},
		{
			name:     "with reasoning",
			input:    "<reasoning>thinking...</reasoning>ls -la",
			expected: "ls -la",
		},
		{
			name:     "with whitespace",
			input:    "<reasoning>thinking...</reasoning>  ls -la  ",
			expected: "ls -la",
		},
		{
			name:     "multiline reasoning",
			input:    "<reasoning>\nthinking\nmore\n</reasoning>\nls -la",
			expected: "ls -la",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAndExtractCommand(tt.input)
			if got != tt.expected {
				t.Errorf("ParseAndExtractCommand(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
