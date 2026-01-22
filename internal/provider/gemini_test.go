package provider

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"google.golang.org/genai"
)

func TestNewGeminiProvider(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	p, err := NewGeminiProvider(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Model != "gemini-2.5-flash" {
		t.Errorf("expected default model gemini-2.5-flash, got %s", p.Model)
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
			if p.Model != "gemini-2.5-flash" {
				t.Errorf("expected default model gemini-2.5-flash, got %s", p.Model)
			}
		})
	}
}

func TestNewGeminiProvider_Errors(t *testing.T) {
	os.Unsetenv("GEMINI_API_KEY")
	_, err := NewGeminiProvider(t.Context())
	if err == nil || !strings.Contains(err.Error(), "GEMINI_API_KEY") {
		t.Errorf("expected api key error, got %v", err)
	}
}

// Mock HTTP client for testing different response scenarios
func createMockHTTPClient(responseBody string, statusCode int) *http.Client {
	return &http.Client{
		Transport: &mockTransport{
			responseBody: responseBody,
			statusCode:   statusCode,
		},
	}
}

type mockTransport struct {
	responseBody string
	statusCode   int
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: m.statusCode,
		Body:       io.NopCloser(strings.NewReader(m.responseBody)),
		Header:     make(http.Header),
	}, nil
}

// Test Fetch method delegation
func TestGeminiProvider_Fetch(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	successResponse := `{
		"candidates": [
			{
				"content": {
					"parts": [
						{"text": "=ls"}
					]
				}
			}
		]
	}`

	ctx := t.Context()
	mockClient := createMockHTTPClient(successResponse, 200)

	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     "test-key",
		HTTPClient: mockClient,
	})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	p := &GeminiProvider{
		Model:  "gemini-2.5-flash",
		Client: client,
	}

	result, err := p.Fetch(ctx, "test input", "test prompt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "=ls" {
		t.Errorf("expected '=ls', got %q", result)
	}
}

// Test with mocked HTTP responses to achieve comprehensive coverage
func TestGeminiProvider_FetchWithHistory_MockedResponses(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	testCases := []struct {
		name          string
		responseBody  string
		statusCode    int
		expectError   bool
		errorContains string
	}{
		{
			name: "successful_response",
			responseBody: `{
				"candidates": [
					{
						"content": {
							"parts": [
								{"text": "=ls -la"}
							]
						}
					}
				]
			}`,
			statusCode:  200,
			expectError: false,
		},
		{
			name: "no_candidates",
			responseBody: `{
				"candidates": []
			}`,
			statusCode:    200,
			expectError:   true,
			errorContains: "no candidates returned",
		},
		{
			name: "null_content",
			responseBody: `{
				"candidates": [
					{
						"content": null
					}
				]
			}`,
			statusCode:    200,
			expectError:   true,
			errorContains: "no content parts returned",
		},
		{
			name: "empty_parts",
			responseBody: `{
				"candidates": [
					{
						"content": {
							"parts": []
						}
					}
				]
			}`,
			statusCode:    200,
			expectError:   true,
			errorContains: "no content parts returned",
		},
		{
			name: "empty_text",
			responseBody: `{
				"candidates": [
					{
						"content": {
							"parts": [
								{"text": ""}
							]
						}
					}
				]
			}`,
			statusCode:    200,
			expectError:   true,
			errorContains: "unexpected part type",
		},
		{
			name: "api_error",
			responseBody: `{
				"error": {
					"message": "API key not valid"
				}
			}`,
			statusCode:    400,
			expectError:   true,
			errorContains: "failed to send message",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			mockClient := createMockHTTPClient(tc.responseBody, tc.statusCode)

			client, err := genai.NewClient(ctx, &genai.ClientConfig{
				APIKey:     "test-key",
				HTTPClient: mockClient,
			})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			p := &GeminiProvider{
				Model:  "gemini-2.5-flash",
				Client: client,
			}

			history := []Message{
				{Role: "user", Content: "Hello"},
				{Role: "assistant", Content: "Hi there!"},
			}

			result, err := p.FetchWithHistory(ctx, "test input", "test prompt", history)

			if tc.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tc.errorContains != "" && !strings.Contains(err.Error(), tc.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tc.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == "" {
					t.Error("expected non-empty result")
				}
			}
		})
	}
}

// Test system prompt and role filtering scenarios
func TestGeminiProvider_FetchWithHistory_Scenarios(t *testing.T) {
	os.Setenv("GEMINI_API_KEY", "test-key")
	defer os.Unsetenv("GEMINI_API_KEY")

	successResponse := `{
		"candidates": [
			{
				"content": {
					"parts": [
						{"text": "=echo test"}
					]
				}
			}
		]
	}`

	testCases := []struct {
		name         string
		input        string
		systemPrompt string
		history      []Message
	}{
		{
			name:         "empty_system_prompt",
			input:        "test",
			systemPrompt: "",
			history:      nil,
		},
		{
			name:         "with_system_prompt",
			input:        "test",
			systemPrompt: "You are helpful",
			history:      nil,
		},
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
			ctx := t.Context()
			mockClient := createMockHTTPClient(successResponse, 200)

			client, err := genai.NewClient(ctx, &genai.ClientConfig{
				APIKey:     "test-key",
				HTTPClient: mockClient,
			})
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			p := &GeminiProvider{
				Model:  "gemini-2.5-flash",
				Client: client,
			}

			_, err = p.FetchWithHistory(ctx, tc.input, tc.systemPrompt, tc.history)
			if err != nil {
				t.Errorf("unexpected error for %s: %v", tc.name, err)
			}
		})
	}
}
