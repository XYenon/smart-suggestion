package provider

import (
	"strings"

	"github.com/xyenon/smart-suggestion/internal/debug"
)

func envOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func normalizeBaseURL(baseURL string) string {
	if baseURL == "" {
		return ""
	}
	normalized := strings.TrimSuffix(baseURL, "/")
	if !strings.HasPrefix(normalized, "http://") && !strings.HasPrefix(normalized, "https://") {
		normalized = "https://" + normalized
	}
	return normalized
}

func logProviderRequest(providerName string, modelOrDeployment string, systemPrompt string, history []Message, input string) {
	debug.Log("Sending provider request", map[string]any{
		"provider":      providerName,
		"model":         modelOrDeployment,
		"system_prompt": systemPrompt,
		"history":       history,
		"input":         input,
	})
}
