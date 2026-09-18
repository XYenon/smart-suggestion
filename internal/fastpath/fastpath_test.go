package fastpath

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "key")
	t.Setenv("TYPESAFE_SYSTEMONE_URL", "https://typesafe.example.test/custom/systemone")
	t.Setenv("TYPESAFE_MODEL", "jev-custom")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_MAX_CANDIDATES", "32")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_SCROLLBACK_LINES", "12")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_CONFIDENCE_THRESHOLD", "0.8")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_PROBABILITY_THRESHOLD", "0.9")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_TIMEOUT_MS", "250")

	config := ConfigFromEnv()
	if config.APIKey != "key" || config.Endpoint != "https://typesafe.example.test/custom/systemone" || config.Model != "jev-custom" {
		t.Fatalf("unexpected TypeSafe config: %#v", config)
	}
	if config.MaxCandidates != 32 || config.ScrollbackLines != 12 {
		t.Fatalf("unexpected config: %#v", config)
	}
	if config.ConfidenceThreshold != 0.8 || config.ProbabilityThreshold != 0.9 || config.Timeout != 250*time.Millisecond {
		t.Fatalf("unexpected thresholds or timeout: %#v", config)
	}
}

func TestConfigFromEnvRejectsInvalidValues(t *testing.T) {
	t.Setenv("TYPESAFE_SYSTEMONE_URL", "  ")
	t.Setenv("TYPESAFE_MODEL", "  ")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_MAX_CANDIDATES", "33")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_SCROLLBACK_LINES", "invalid")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_CONFIDENCE_THRESHOLD", "1.1")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_PROBABILITY_THRESHOLD", "-0.1")
	t.Setenv("SMART_SUGGESTION_FAST_PATH_TIMEOUT_MS", "0")

	config := ConfigFromEnv()
	if config.Endpoint != defaultEndpoint || config.Model != defaultModel {
		t.Fatalf("empty TypeSafe values did not use defaults: %#v", config)
	}
	if config.MaxCandidates != defaultMaxCandidates || config.ScrollbackLines != defaultScrollbackLines {
		t.Fatalf("invalid integers did not use defaults: %#v", config)
	}
	if config.ConfidenceThreshold != defaultConfidenceThreshold || config.ProbabilityThreshold != defaultProbabilityThreshold || config.Timeout != defaultTimeout {
		t.Fatalf("invalid thresholds did not use defaults: %#v", config)
	}
}

func TestDecodeHistoryPreservesEventBoundariesAndText(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("  git status  \x00printf 'a\\nb'\nnext line\x00go test ./...\x00"))
	got, err := DecodeHistory(encoded)
	if err != nil {
		t.Fatalf("DecodeHistory() error = %v", err)
	}
	want := []string{"  git status  ", "printf 'a\\nb'\nnext line", "go test ./..."}
	if len(got) != len(want) {
		t.Fatalf("DecodeHistory() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("DecodeHistory()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDecodeHistoryRejectsInvalidBase64(t *testing.T) {
	if _, err := DecodeHistory("not base64"); err == nil {
		t.Fatal("expected invalid base64 error")
	}
}

func TestBuildCandidatesUsesPrefixAndTransitions(t *testing.T) {
	history := []string{
		"git status",
		"git diff",
		"go test ./...",
		"git status",
		"git diff",
		"git status",
	}

	candidates := BuildCandidates(history, "go t", 24)
	if len(candidates) != 2 {
		t.Fatalf("got %d candidates, want 2: %#v", len(candidates), candidates)
	}

	byCommand := make(map[string]Candidate, len(candidates))
	for _, candidate := range candidates {
		byCommand[candidate.Command] = candidate
	}
	if !byCommand["go test ./..."].PrefixMatch {
		t.Fatal("expected prefix candidate")
	}
	if byCommand["git diff"].TransitionFrequency != 2 {
		t.Fatalf("transition frequency = %d, want 2", byCommand["git diff"].TransitionFrequency)
	}
}

func TestBuildCandidatesRanksPrefixQualityRecencyFrequencyAndTransition(t *testing.T) {
	history := []string{
		"git checkout main",
		"git status",
		"git checkout main",
		"git status",
		"git checkout feature",
		"git checkout main",
	}

	candidates := BuildCandidates(history, "git check", 24)
	if len(candidates) < 2 {
		t.Fatalf("got too few candidates: %#v", candidates)
	}
	if candidates[0].Command != "git checkout main" {
		t.Fatalf("top candidate = %q, want frequently and recently used prefix", candidates[0].Command)
	}
	if candidates[0].Frequency != 3 || candidates[0].Recency != 0 {
		t.Fatalf("unexpected ranking metadata: %#v", candidates[0])
	}
}

func TestSuggestReturnsSinglePrefixWithoutJev(t *testing.T) {
	selector := NewSelector(testConfig("http://unused"), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("Jev should not be called for one strong prefix candidate")
			return nil, nil
		}),
	})

	got, ok := selector.Suggest(t.Context(), "kubectl get p", []string{"ls", "kubectl get pods"}, "/tmp", true, nil)
	if !ok || got != "+ods" {
		t.Fatalf("Suggest() = %q, %v; want +ods, true", got, ok)
	}
}

func TestSuggestZeroCandidatesFallsBackWithoutJev(t *testing.T) {
	selector := NewSelector(testConfig("http://unused"), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("Jev should not be called without candidates")
			return nil, nil
		}),
	})

	if got, ok := selector.Suggest(t.Context(), "kubectl", []string{"ls", "pwd"}, "/tmp", true, nil); ok {
		t.Fatalf("Suggest() = %q, true; want fallback", got)
	}
}

func TestSuggestDoesNotCallJevWhenContextSharingIsDisabled(t *testing.T) {
	selector := NewSelector(testConfig("http://unused"), &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("Jev should not be called when context sharing is disabled")
			return nil, nil
		}),
	})

	if got, ok := selector.Suggest(t.Context(), "", transitionHistory(), "/tmp", false, nil); ok {
		t.Fatalf("Suggest() = %q, true; want fallback", got)
	}
}

func TestSuggestAcceptsOnlyKnownHighConfidenceChoice(t *testing.T) {
	server := choiceServer(t, "candidate_01", 0.82, 0.75)
	defer server.Close()
	selector := NewSelector(testConfig(server.URL), server.Client())

	got, ok := selector.Suggest(t.Context(), "", transitionHistory(), "/repo", true, func(lines int) (string, error) {
		if lines != 20 {
			t.Fatalf("scrollback lines = %d, want 20", lines)
		}
		return "tests passed", nil
	})
	if !ok || got != "=git diff" {
		t.Fatalf("Suggest() = %q, %v; want =git diff, true", got, ok)
	}
}

func TestSuggestFormatsSelectedPrefixAsCompletion(t *testing.T) {
	server := choiceServer(t, "candidate_00", 0.82, 0.75)
	defer server.Close()
	selector := NewSelector(testConfig(server.URL), server.Client())

	got, ok := selector.Suggest(t.Context(), "git", []string{"git diff", "go test ./...", "git status"}, "/repo", true, nil)
	if !ok || got != "+ status" {
		t.Fatalf("Suggest() = %q, %v; want + status, true", got, ok)
	}
}

func TestSuggestFallsBack(t *testing.T) {
	tests := []struct {
		name        string
		choice      string
		probability float64
		confidence  float64
	}{
		{name: "none", choice: noneChoice, probability: 0.9, confidence: 0.9},
		{name: "low probability", choice: "candidate_00", probability: 0.69, confidence: 0.9},
		{name: "low confidence", choice: "candidate_00", probability: 0.9, confidence: 0.59},
		{name: "unknown candidate", choice: "candidate_99", probability: 0.9, confidence: 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := choiceServer(t, tt.choice, tt.probability, tt.confidence)
			defer server.Close()
			selector := NewSelector(testConfig(server.URL), server.Client())

			if got, ok := selector.Suggest(t.Context(), "", transitionHistory(), "/repo", true, nil); ok {
				t.Fatalf("Suggest() = %q, true; want fallback", got)
			}
		})
	}
}

func TestSuggestFallsBackOnJevErrorAndTimeout(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "overloaded", http.StatusServiceUnavailable)
		}))
		defer server.Close()
		selector := NewSelector(testConfig(server.URL), server.Client())
		if _, ok := selector.Suggest(t.Context(), "", transitionHistory(), "/repo", true, nil); ok {
			t.Fatal("expected fallback")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
		}))
		defer server.Close()
		config := testConfig(server.URL)
		config.Timeout = time.Millisecond
		selector := NewSelector(config, server.Client())
		if _, ok := selector.Suggest(t.Context(), "", transitionHistory(), "/repo", true, nil); ok {
			t.Fatal("expected fallback")
		}
	})
}

func TestFormatSuggestion(t *testing.T) {
	if got := FormatSuggestion("git st", "git status"); got != "+atus" {
		t.Fatalf("prefix format = %q, want +atus", got)
	}
	if got := FormatSuggestion("", "git status"); got != "=git status" {
		t.Fatalf("command format = %q, want =git status", got)
	}
	if got := FormatSuggestion("go test", "git status"); got != "=git status" {
		t.Fatalf("non-prefix format = %q, want =git status", got)
	}
}

func testConfig(endpoint string) Config {
	return Config{
		APIKey:               "test-key",
		Endpoint:             endpoint,
		Model:                defaultModel,
		MaxCandidates:        defaultMaxCandidates,
		RecentCommands:       defaultRecentCommands,
		ScrollbackLines:      defaultScrollbackLines,
		ConfidenceThreshold:  defaultConfidenceThreshold,
		ProbabilityThreshold: defaultProbabilityThreshold,
		Timeout:              time.Second,
	}
}

func transitionHistory() []string {
	return []string{"git status", "git diff", "git status", "go test ./...", "git status"}
}

func choiceServer(t *testing.T, choice string, probability, confidence float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected authorization header")
		}
		var request choiceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if _, ok := request.Questions["command"].Criteria[noneChoice]; !ok {
			t.Errorf("request omitted explicit none choice")
		}
		if len(request.State.Candidates) == 0 {
			t.Errorf("request omitted candidates from state")
		}
		if got := request.Questions["command"].Criteria["candidate_00"]; got != "Select `state.candidates[0].command` exactly" {
			t.Errorf("candidate criterion = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": defaultModel,
			"answers": map[string]any{
				"command": map[string]any{
					"type": "choice", "choice": choice,
					"probabilities": map[string]float64{choice: probability},
					"confidence":    confidence,
				},
			},
		})
	}))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
