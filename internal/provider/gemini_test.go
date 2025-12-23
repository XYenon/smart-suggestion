package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func TestNewGeminiProvider(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	p, err := NewGeminiProvider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gemini-2.5-flash" {
		t.Errorf("expected default model gemini-2.5-flash, got %s", p.Model)
	}
}

func TestNewGeminiProvider_Errors(t *testing.T) {
	os.Unsetenv("GEMINI_API_KEY")
	_, err := NewGeminiProvider()
	if err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("expected api key error, got %v", err)
	}
}

func TestGeminiProvider_Fetch(t *testing.T) {
	cases := []TestCase{
		{
			Name:         "successful suggestion",
			Input:        "how to list files",
			SystemPrompt: "you are a shell assistant",
			MockStatus:   http.StatusOK,
			MockResponse: `{
				"candidates": [
					{
						"content": {
							"parts": [
								{
									"text": "<reasoning>The user wants to list files.</reasoning>=ls"
								}
							]
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
			MockStatus:    http.StatusBadRequest,
			MockResponse:  `{"error": {"message": "invalid api key"}}`,
			ExpectedError: "failed to generate content",
		},
		{
			Name:          "no candidates",
			Input:         "test",
			SystemPrompt:  "test",
			MockStatus:    http.StatusOK,
			MockResponse:  `{"candidates": []}`,
			ExpectedError: "no candidates returned from Gemini API",
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

			ctx := context.Background()
			client, err := genai.NewClient(ctx,
				option.WithAPIKey("test-key"),
				option.WithEndpoint(server.URL),
			)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			p := &GeminiProvider{
				Model:  "gemini-2.5-flash",
				Client: client,
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
