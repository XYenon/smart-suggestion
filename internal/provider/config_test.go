package provider

import "testing"

func TestEnvOrDefault(t *testing.T) {
	if got := envOrDefault("value", "fallback"); got != "value" {
		t.Fatalf("expected value, got %q", got)
	}
	if got := envOrDefault("", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "empty", input: "", expected: ""},
		{name: "https", input: "https://example.com/", expected: "https://example.com"},
		{name: "http", input: "http://example.com", expected: "http://example.com"},
		{name: "no-scheme", input: "example.com/", expected: "https://example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeBaseURL(tc.input); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
