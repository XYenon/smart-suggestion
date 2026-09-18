package fastpath

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultEndpoint             = "https://api.typesafe.ai/v1/systemone"
	defaultModel                = "jev-latest"
	defaultMaxCandidates        = 24
	defaultRecentCommands       = 5
	defaultScrollbackLines      = 20
	defaultConfidenceThreshold  = 0.60
	defaultProbabilityThreshold = 0.70
	defaultTimeout              = 1200 * time.Millisecond
	noneChoice                  = "none"
)

type Config struct {
	APIKey               string
	Endpoint             string
	Model                string
	MaxCandidates        int
	RecentCommands       int
	ScrollbackLines      int
	ConfidenceThreshold  float64
	ProbabilityThreshold float64
	Timeout              time.Duration
}

type Candidate struct {
	Command             string  `json:"command"`
	PrefixMatch         bool    `json:"prefix_match"`
	PrefixQuality       float64 `json:"prefix_quality"`
	Recency             int     `json:"recency"`
	Frequency           int     `json:"frequency"`
	TransitionFrequency int     `json:"transition_frequency"`
	Score               float64 `json:"local_score"`
}

type Selector struct {
	config Config
	client *http.Client
}

func ConfigFromEnv() Config {
	return Config{
		APIKey:               os.Getenv("TYPESAFE_API_KEY"),
		Endpoint:             envString("TYPESAFE_SYSTEMONE_URL", defaultEndpoint),
		Model:                envString("TYPESAFE_MODEL", defaultModel),
		MaxCandidates:        envInt("SMART_SUGGESTION_FAST_PATH_MAX_CANDIDATES", defaultMaxCandidates, 2, 32),
		RecentCommands:       defaultRecentCommands,
		ScrollbackLines:      envInt("SMART_SUGGESTION_FAST_PATH_SCROLLBACK_LINES", defaultScrollbackLines, 0, 100),
		ConfidenceThreshold:  envFloat("SMART_SUGGESTION_FAST_PATH_CONFIDENCE_THRESHOLD", defaultConfidenceThreshold),
		ProbabilityThreshold: envFloat("SMART_SUGGESTION_FAST_PATH_PROBABILITY_THRESHOLD", defaultProbabilityThreshold),
		Timeout:              time.Duration(envInt("SMART_SUGGESTION_FAST_PATH_TIMEOUT_MS", int(defaultTimeout/time.Millisecond), 1, 10000)) * time.Millisecond,
	}
}

func NewSelector(config Config, client *http.Client) *Selector {
	if client == nil {
		client = &http.Client{Timeout: config.Timeout}
	}
	return &Selector{config: config, client: client}
}

func NewSelectorFromEnv() *Selector {
	return NewSelector(ConfigFromEnv(), nil)
}

func (s *Selector) Enabled() bool {
	return s.config.APIKey != ""
}

func DecodeHistory(value string) ([]string, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("decode fast-path history: %w", err)
	}

	events := bytes.Split(decoded, []byte{0})
	history := make([]string, 0, len(events))
	for _, event := range events {
		if len(event) > 0 {
			history = append(history, string(event))
		}
	}
	return history, nil
}

func BuildCandidates(history []string, buffer string, limit int) []Candidate {
	if len(history) == 0 || limit <= 0 {
		return nil
	}

	type stats struct {
		frequency           int
		latestIndex         int
		transitionFrequency int
	}

	all := make(map[string]*stats, len(history))
	for i, command := range history {
		entry := all[command]
		if entry == nil {
			entry = &stats{}
			all[command] = entry
		}
		entry.frequency++
		entry.latestIndex = i
	}

	previousCommand := history[len(history)-1]
	for i := 0; i+1 < len(history); i++ {
		if history[i] == previousCommand {
			all[history[i+1]].transitionFrequency++
		}
	}

	candidates := make([]Candidate, 0, len(all))
	for command, entry := range all {
		prefixMatch := buffer != "" && strings.HasPrefix(command, buffer)
		if !prefixMatch && entry.transitionFrequency == 0 {
			continue
		}
		if command == buffer {
			continue
		}

		prefixQuality := 0.0
		if prefixMatch {
			prefixQuality = 1 + float64(len(buffer))/float64(len(command))
		}
		recency := len(history) - 1 - entry.latestIndex
		recencyScore := 1 / float64(recency+1)
		score := 4*prefixQuality + 1.5*recencyScore + 0.5*math.Log1p(float64(entry.frequency)) + 1.5*math.Log1p(float64(entry.transitionFrequency))

		candidates = append(candidates, Candidate{
			Command:             command,
			PrefixMatch:         prefixMatch,
			PrefixQuality:       prefixQuality,
			Recency:             recency,
			Frequency:           entry.frequency,
			TransitionFrequency: entry.transitionFrequency,
			Score:               score,
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Recency != candidates[j].Recency {
			return candidates[i].Recency < candidates[j].Recency
		}
		return candidates[i].Command < candidates[j].Command
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates
}

func FormatSuggestion(buffer, command string) string {
	if buffer != "" && strings.HasPrefix(command, buffer) {
		return "+" + command[len(buffer):]
	}
	return "=" + command
}

func (s *Selector) Suggest(ctx context.Context, buffer string, history []string, cwd string, allowRemote bool, scrollback func(int) (string, error)) (string, bool) {
	if !s.Enabled() {
		return "", false
	}

	candidates := BuildCandidates(history, buffer, s.config.MaxCandidates)
	if len(candidates) == 0 {
		return "", false
	}
	if len(candidates) == 1 && candidates[0].PrefixMatch {
		return FormatSuggestion(buffer, candidates[0].Command), true
	}
	if !allowRemote {
		return "", false
	}

	scrollbackText := ""
	if s.config.ScrollbackLines > 0 && scrollback != nil {
		if value, err := scrollback(s.config.ScrollbackLines); err == nil {
			scrollbackText = value
		}
	}

	choice, probability, confidence, err := s.choose(ctx, choiceState{
		Buffer:         buffer,
		CWD:            cwd,
		RecentCommands: latest(history, s.config.RecentCommands),
		Scrollback:     scrollbackText,
		Candidates:     candidates,
	})
	if err != nil || choice == noneChoice || probability < s.config.ProbabilityThreshold || confidence < s.config.ConfidenceThreshold {
		return "", false
	}

	index, ok := parseChoiceIndex(choice, len(candidates))
	if !ok {
		return "", false
	}
	return FormatSuggestion(buffer, candidates[index].Command), true
}

type choiceState struct {
	Buffer         string      `json:"buffer"`
	CWD            string      `json:"cwd"`
	RecentCommands []string    `json:"recent_commands"`
	Scrollback     string      `json:"scrollback,omitempty"`
	Candidates     []Candidate `json:"candidates"`
}

type choiceQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type choiceRequest struct {
	State     choiceState               `json:"state"`
	Model     string                    `json:"model"`
	Questions map[string]choiceQuestion `json:"questions"`
}

type choiceResponse struct {
	Answers map[string]struct {
		Type          string             `json:"type"`
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
		Confidence    float64            `json:"confidence"`
	} `json:"answers"`
}

func (s *Selector) choose(ctx context.Context, state choiceState) (string, float64, float64, error) {
	criteria := make(map[string]string, len(state.Candidates)+1)
	for i := range state.Candidates {
		criteria[choiceID(i)] = fmt.Sprintf("Select `state.candidates[%d].command` exactly", i)
	}
	criteria[noneChoice] = "None of the commands clearly fits what the user is likely to enter next"

	payload, err := json.Marshal(choiceRequest{
		State: state,
		Model: s.config.Model,
		Questions: map[string]choiceQuestion{
			"command": {
				Type:         "choice",
				Instructions: "Select the single previously executed candidate command the user is most likely to enter now. Use buffer, cwd, recent commands, scrollback, and candidate metadata. Choose none unless the context provides a specific reason to prefer one candidate. Recency, frequency, or repeating the previous command alone is not sufficient evidence. Never create or modify a command.",
				Criteria:     criteria,
			},
		},
	})
	if err != nil {
		return "", 0, 0, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, s.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", 0, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", 0, 0, fmt.Errorf("typesafe returned status %d", resp.StatusCode)
	}

	var result choiceResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil {
		return "", 0, 0, err
	}
	answer, ok := result.Answers["command"]
	if !ok || answer.Type != "choice" {
		return "", 0, 0, fmt.Errorf("missing choice answer")
	}
	probability, ok := answer.Probabilities[answer.Choice]
	if !ok || probability < 0 || probability > 1 || answer.Confidence < 0 || answer.Confidence > 1 {
		return "", 0, 0, fmt.Errorf("missing selected choice probability")
	}
	return answer.Choice, probability, answer.Confidence, nil
}

func choiceID(index int) string {
	return fmt.Sprintf("candidate_%02d", index)
}

func parseChoiceIndex(choice string, count int) (int, bool) {
	value, ok := strings.CutPrefix(choice, "candidate_")
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(value)
	return index, err == nil && index >= 0 && index < count
}

func latest(values []string, count int) []string {
	if count <= 0 {
		return nil
	}
	if len(values) > count {
		values = values[len(values)-count:]
	}
	return append([]string(nil), values...)
}

func envInt(name string, fallback, minValue, maxValue int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < minValue || value > maxValue {
		return fallback
	}
	return value
}

func envFloat(name string, fallback float64) float64 {
	value, err := strconv.ParseFloat(os.Getenv(name), 64)
	if err != nil || value < 0 || value > 1 {
		return fallback
	}
	return value
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
