package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yetone/smart-suggestion/internal/debug"
)

type AnthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type AnthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []AnthropicMessage `json:"messages"`
}

type AnthropicContent struct {
	Text string `json:"text"`
	Type string `json:"type"`
}

type AnthropicResponse struct {
	Content []AnthropicContent `json:"content"`
}

type AnthropicProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewAnthropicProvider() (*AnthropicProvider, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable is not set")
	}

	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-3-5-sonnet-20241022"
	}

	return &AnthropicProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

func (p *AnthropicProvider) Fetch(input string, systemPrompt string) (string, error) {
	var url string
	baseURL := strings.TrimSuffix(p.BaseURL, "/")
	if strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://") {
		url = fmt.Sprintf("%s/v1/messages", baseURL)
	} else {
		url = fmt.Sprintf("https://%s/v1/messages", baseURL)
	}

	request := AnthropicRequest{
		Model:     p.Model,
		MaxTokens: 1000,
		System:    systemPrompt,
		Messages: []AnthropicMessage{
			{Role: "user", Content: input},
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	debug.Log("Sending Anthropic request", map[string]any{
		"url":     url,
		"request": string(jsonData),
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	debug.Log("Received Anthropic response", map[string]any{
		"status":   resp.Status,
		"response": string(body),
	})

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response AnthropicResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(response.Content) == 0 {
		return "", fmt.Errorf("no content returned from Anthropic API")
	}

	return response.Content[0].Text, nil
}
