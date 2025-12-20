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

type DeepSeekProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewDeepSeekProvider() (*DeepSeekProvider, error) {
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY environment variable is not set")
	}

	baseURL := os.Getenv("DEEPSEEK_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}

	model := os.Getenv("DEEPSEEK_MODEL")
	if model == "" {
		model = "deepseek-chat"
	}

	return &DeepSeekProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

func (p *DeepSeekProvider) Fetch(input string, systemPrompt string) (string, error) {
	var url string
	baseURL := strings.TrimSuffix(p.BaseURL, "/")
	if strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://") {
		url = fmt.Sprintf("%s/chat/completions", baseURL)
	} else {
		url = fmt.Sprintf("https://%s/chat/completions", baseURL)
	}

	request := OpenAIRequest{
		Model: p.Model,
		Messages: []OpenAIMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: input},
		},
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	debug.Log("Sending DeepSeek request", map[string]any{
		"url":     url,
		"request": string(jsonData),
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)

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

	debug.Log("Received DeepSeek response", map[string]any{
		"status":   resp.Status,
		"response": string(body),
	})

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response OpenAIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("DeepSeek API error: %s", response.Error.Message)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no choices returned from DeepSeek API")
	}

	return response.Choices[0].Message.Content, nil
}
