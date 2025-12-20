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

type GeminiPart struct {
	Text string `json:"text"`
}

type GeminiContent struct {
	Parts []GeminiPart `json:"parts"`
	Role  string       `json:"role"`
}

type GeminiRequest struct {
	Contents []GeminiContent `json:"contents"`
}

type GeminiCandidate struct {
	Content GeminiContent `json:"content"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
	Error      *GeminiError      `json:"error,omitempty"`
}

type GeminiError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type GeminiProvider struct {
	APIKey  string
	BaseURL string
	Model   string
}

func NewGeminiProvider() (*GeminiProvider, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable is not set")
	}

	baseURL := os.Getenv("GEMINI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://generativelanguage.googleapis.com"
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	return &GeminiProvider{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	}, nil
}

func (p *GeminiProvider) Fetch(input string, systemPrompt string) (string, error) {
	var url string
	baseURL := strings.TrimSuffix(p.BaseURL, "/")
	if strings.HasPrefix(baseURL, "http://") || strings.HasPrefix(baseURL, "https://") {
		url = fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", baseURL, p.Model, p.APIKey)
	} else {
		url = fmt.Sprintf("https://%s/v1beta/models/%s:generateContent?key=%s", baseURL, p.Model, p.APIKey)
	}

	var contents []GeminiContent
	if systemPrompt != "" {
		contents = append(contents, GeminiContent{
			Parts: []GeminiPart{{Text: systemPrompt}},
			Role:  "user",
		})
		contents = append(contents, GeminiContent{
			Parts: []GeminiPart{{Text: "I understand. I'll follow these instructions."}},
			Role:  "model",
		})
	}

	contents = append(contents, GeminiContent{
		Parts: []GeminiPart{{Text: input}},
		Role:  "user",
	})

	request := GeminiRequest{
		Contents: contents,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	debug.Log("Sending Gemini request", map[string]any{
		"url":     url,
		"request": string(jsonData),
	})

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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

	debug.Log("Received Gemini response", map[string]any{
		"status":   resp.Status,
		"response": string(body),
	})

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response GeminiResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if response.Error != nil {
		return "", fmt.Errorf("Gemini API error: %s", response.Error.Message)
	}

	if len(response.Candidates) == 0 {
		return "", fmt.Errorf("no candidates returned from Gemini API")
	}

	if len(response.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("no content parts returned from Gemini API")
	}

	return response.Candidates[0].Content.Parts[0].Text, nil
}
