package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/xyenon/smart-suggestion/internal/provider"
	"github.com/xyenon/smart-suggestion/internal/proxy"
)

func TestResolveSystemPrompt(t *testing.T) {
	original := systemPrompt
	oldBuildSystemContext := buildSystemContextFunc
	t.Cleanup(func() {
		systemPrompt = original
		buildSystemContextFunc = oldBuildSystemContext
	})

	buildSystemContextFunc = func() (string, error) {
		return "", nil
	}

	systemPrompt = ""
	if got := resolveSystemPrompt(false); got != defaultSystemPrompt {
		t.Fatalf("expected default prompt, got %q", got)
	}

	systemPrompt = "custom"
	if got := resolveSystemPrompt(false); got != "custom" {
		t.Fatalf("expected custom prompt, got %q", got)
	}

	// Test with sendContext=true to verify context concatenation
	buildSystemContextFunc = func() (string, error) {
		return "mocked system context", nil
	}
	systemPrompt = ""
	got := resolveSystemPrompt(true)
	if got != defaultSystemPrompt+"\n\n"+"mocked system context" {
		t.Fatalf("expected prompt with context, got %q", got)
	}

	// Test with sendContext=true and custom prompt
	systemPrompt = "custom"
	got = resolveSystemPrompt(true)
	if got != "custom\n\nmocked system context" {
		t.Fatalf("expected custom prompt with context, got %q", got)
	}
}

func TestBuildUserInputWithScrollback(t *testing.T) {
	old := buildUserContextFunc
	buildUserContextFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "", nil
	}
	t.Cleanup(func() { buildUserContextFunc = old })

	file := filepath.Join(t.TempDir(), "scrollback.txt")
	if err := os.WriteFile(file, []byte("first\nsecond\n"), 0644); err != nil {
		t.Fatalf("failed to write scrollback file: %v", err)
	}

	buildUserContextFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "# Scrollback:\n\nsecond", nil
	}

	got := buildUserInput("test", 1, file, true)
	expected := "# Scrollback:\n\nsecond\n\n# User input:\n\ntest"
	if got != expected {
		t.Fatalf("expected user input with scrollback content, got %q, want %q", got, expected)
	}
}

func TestRunSuggestMissingFlags(t *testing.T) {
	oldProvider := providerName
	oldInput := input
	oldDebug := dbg
	t.Cleanup(func() {
		providerName = oldProvider
		input = oldInput
		dbg = oldDebug
	})

	providerName = ""
	input = ""
	dbg = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSuggest(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing provider flag")
	}

	providerName = "openai"
	input = ""
	err = runSuggest(cmd, nil)
	if err == nil {
		t.Fatal("expected error for missing input flag")
	}
}

func TestWriteSuggestion(t *testing.T) {
	file := filepath.Join(t.TempDir(), "output.txt")
	if err := writeSuggestion(file, "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if string(contents) != "hello" {
		t.Fatalf("expected output to match, got %q", string(contents))
	}

	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	if err := writeSuggestion("-", "world"); err != nil {
		os.Stdout = stdout
		w.Close()
		t.Fatalf("unexpected error: %v", err)
	}
	_ = w.Close()
	os.Stdout = stdout
	data, _ := io.ReadAll(r)
	if string(data) != "world" {
		t.Fatalf("expected stdout output, got %q", string(data))
	}
}

func TestGetExampleHistory(t *testing.T) {
	if len(getExampleHistory()) == 0 {
		t.Fatal("expected example history entries")
	}
}

func TestSelectProvider(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	originalProvider := providerName
	t.Cleanup(func() { providerName = originalProvider })

	setEnv := func(key, value string) func() {
		old := os.Getenv(key)
		_ = os.Setenv(key, value)
		return func() { _ = os.Setenv(key, old) }
	}

	cleanupOpenAI := setEnv("OPENAI_API_KEY", "fake")
	defer cleanupOpenAI()
	providerName = "openai"
	if _, err := selectProvider(cmd); err != nil {
		t.Fatalf("expected openai provider, got %v", err)
	}

	cleanupAnthropic := setEnv("ANTHROPIC_API_KEY", "fake")
	defer cleanupAnthropic()
	providerName = "anthropic"
	if _, err := selectProvider(cmd); err != nil {
		t.Fatalf("expected anthropic provider, got %v", err)
	}

	cleanupGemini := setEnv("GEMINI_API_KEY", "fake")
	defer cleanupGemini()
	providerName = "gemini"
	if _, err := selectProvider(cmd); err != nil {
		t.Fatalf("expected gemini provider, got %v", err)
	}

	cleanupAzureKey := setEnv("AZURE_OPENAI_API_KEY", "fake")
	cleanupAzureDeployment := setEnv("AZURE_OPENAI_DEPLOYMENT_NAME", "deploy")
	cleanupAzureResource := setEnv("AZURE_OPENAI_RESOURCE_NAME", "resource")
	defer cleanupAzureKey()
	defer cleanupAzureDeployment()
	defer cleanupAzureResource()
	providerName = "azure_openai"
	if _, err := selectProvider(cmd); err != nil {
		t.Fatalf("expected azure provider, got %v", err)
	}

	providerName = "unknown"
	if _, err := selectProvider(cmd); err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestRunRotateLogs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "proxy.log")
	if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}

	oldLogFile := proxyLogFile
	oldDebug := dbg
	t.Cleanup(func() {
		proxyLogFile = oldLogFile
		dbg = oldDebug
	})

	proxyLogFile = file
	dbg = false
	if err := runRotateLogs(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunRotateLogsMissingFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.log")
	if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatalf("failed to remove log: %v", err)
	}

	oldLogFile := proxyLogFile
	oldDebug := dbg
	t.Cleanup(func() {
		proxyLogFile = oldLogFile
		dbg = oldDebug
	})

	proxyLogFile = file
	dbg = false

	err := runRotateLogs(nil, nil)
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
}

func TestRunUpdateCheckOnlyAlreadyLatest(t *testing.T) {
	oldExit := exitFunc
	oldCheck := checkUpdateFunc
	oldInstall := installUpdateFunc
	t.Cleanup(func() {
		exitFunc = oldExit
		checkUpdateFunc = oldCheck
		installUpdateFunc = oldInstall
	})

	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	checkUpdateFunc = func(currentVersion string) (string, string, error) {
		return "1.0.0", "", nil
	}
	installUpdateFunc = func(url string) error {
		return nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("check-only", true, "")
	_ = cmd.Flags().Set("check-only", "true")

	runUpdate(cmd, nil)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}

func TestRunUpdateCheckOnlyUpdateAvailable(t *testing.T) {
	oldExit := exitFunc
	oldCheck := checkUpdateFunc
	oldInstall := installUpdateFunc
	t.Cleanup(func() {
		exitFunc = oldExit
		checkUpdateFunc = oldCheck
		installUpdateFunc = oldInstall
	})

	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	checkUpdateFunc = func(currentVersion string) (string, string, error) {
		return "1.1.0", "https://example.com/update", nil
	}
	installCalled := false
	installUpdateFunc = func(url string) error {
		installCalled = true
		return nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("check-only", true, "")
	_ = cmd.Flags().Set("check-only", "true")

	runUpdate(cmd, nil)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if installCalled {
		t.Fatal("expected installUpdateFunc not to be called in --check-only mode")
	}
}

func TestBuildUserInputNoContext(t *testing.T) {
	old := buildUserContextFunc
	t.Cleanup(func() {
		buildUserContextFunc = old
	})

	buildUserContextFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "extra context info", nil
	}

	userInput := buildUserInput("test input", 10, "", false)
	if userInput != "test input" {
		t.Fatalf("expected 'test input' when sendContext is false, got %q", userInput)
	}
}

func TestBuildUserInputContextError(t *testing.T) {
	old := buildUserContextFunc
	t.Cleanup(func() {
		buildUserContextFunc = old
	})

	buildUserContextFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "", errors.New("fail")
	}

	userInput := buildUserInput("test input", 10, "", true)
	if userInput != "test input" {
		t.Fatalf("expected 'test input' on error, got %q", userInput)
	}
}

func TestWriteSuggestionDevStdout(t *testing.T) {
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	if err := writeSuggestion("/dev/stdout", "test"); err != nil {
		w.Close()
		t.Fatalf("unexpected error: %v", err)
	}
	w.Close()
	data, _ := io.ReadAll(r)
	if string(data) != "test" {
		t.Fatalf("expected 'test', got %q", string(data))
	}
}

func TestRunProxy(t *testing.T) {
	oldRunProxy := runProxyFunc
	oldDebug := dbg
	oldLogFile := proxyLogFile
	oldSessionID := sessionID
	oldScrollback := scrollbackLines
	t.Cleanup(func() {
		runProxyFunc = oldRunProxy
		dbg = oldDebug
		proxyLogFile = oldLogFile
		sessionID = oldSessionID
		scrollbackLines = oldScrollback
	})

	called := false
	runProxyFunc = func(shell string, opts proxy.ProxyOptions) error {
		called = true
		return nil
	}

	dbg = false
	proxyLogFile = ""
	sessionID = "test-session"
	scrollbackLines = 50

	runProxy(nil, nil)
	if !called {
		t.Fatal("expected runProxyFunc to be called")
	}
}

func TestRunProxyError(t *testing.T) {
	oldRunProxy := runProxyFunc
	oldDebug := dbg
	oldLogFile := proxyLogFile
	oldSessionID := sessionID
	t.Cleanup(func() {
		runProxyFunc = oldRunProxy
		dbg = oldDebug
		proxyLogFile = oldLogFile
		sessionID = oldSessionID
	})

	runProxyFunc = func(shell string, opts proxy.ProxyOptions) error {
		return errors.New("proxy error")
	}

	dbg = false
	proxyLogFile = "/tmp/test.log"
	sessionID = "test"

	runProxy(nil, nil)
}

func TestRunProxyEmptySessionID(t *testing.T) {
	oldRunProxy := runProxyFunc
	oldDebug := dbg
	oldLogFile := proxyLogFile
	oldSessionID := sessionID
	t.Cleanup(func() {
		runProxyFunc = oldRunProxy
		dbg = oldDebug
		proxyLogFile = oldLogFile
		sessionID = oldSessionID
	})

	var capturedOpts proxy.ProxyOptions
	runProxyFunc = func(shell string, opts proxy.ProxyOptions) error {
		capturedOpts = opts
		return nil
	}

	dbg = false
	proxyLogFile = "/tmp/test.log"
	sessionID = ""

	runProxy(nil, nil)
	if capturedOpts.SessionID == "" {
		t.Fatal("expected auto-generated session ID")
	}
}

func TestRunUpdateCheckError(t *testing.T) {
	oldCheck := checkUpdateFunc
	t.Cleanup(func() { checkUpdateFunc = oldCheck })

	checkUpdateFunc = func(currentVersion string) (string, string, error) {
		return "", "", errors.New("network error")
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("check-only", false, "")

	runUpdate(cmd, nil)
}

func TestRunUpdateInstallError(t *testing.T) {
	oldCheck := checkUpdateFunc
	oldInstall := installUpdateFunc
	t.Cleanup(func() {
		checkUpdateFunc = oldCheck
		installUpdateFunc = oldInstall
	})

	checkUpdateFunc = func(currentVersion string) (string, string, error) {
		return "2.0.0", "https://example.com/update", nil
	}
	installUpdateFunc = func(url string) error {
		return errors.New("install failed")
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("check-only", false, "")

	runUpdate(cmd, nil)
}

func TestRunUpdateInstallSuccess(t *testing.T) {
	oldCheck := checkUpdateFunc
	oldInstall := installUpdateFunc
	t.Cleanup(func() {
		checkUpdateFunc = oldCheck
		installUpdateFunc = oldInstall
	})

	checkUpdateFunc = func(currentVersion string) (string, string, error) {
		return "2.0.0", "https://example.com/update", nil
	}
	installCalled := false
	installUpdateFunc = func(url string) error {
		installCalled = true
		return nil
	}

	cmd := &cobra.Command{}
	cmd.Flags().Bool("check-only", false, "")

	runUpdate(cmd, nil)
	if !installCalled {
		t.Fatal("expected install to be called")
	}
}

func TestRunRotateLogsForceRotateError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "readonly.log")
	if err := os.WriteFile(file, []byte("content"), 0444); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("failed to make dir readonly: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	oldLogFile := proxyLogFile
	oldDebug := dbg
	t.Cleanup(func() {
		proxyLogFile = oldLogFile
		dbg = oldDebug
	})

	proxyLogFile = file
	dbg = false

	err := runRotateLogs(nil, nil)
	if err == nil {
		t.Fatal("expected error for rotate failure")
	}
}

func TestWriteSuggestionWriteError(t *testing.T) {
	err := writeSuggestion("/nonexistent/dir/file.txt", "test")
	if err == nil {
		t.Fatal("expected error for write to nonexistent directory")
	}
}

func TestBuildRootCmd(t *testing.T) {
	cmd := buildRootCmd()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Use != "smart-suggestion" {
		t.Fatalf("expected 'smart-suggestion', got %q", cmd.Use)
	}

	subcommands := cmd.Commands()
	names := make(map[string]bool)
	for _, sub := range subcommands {
		names[sub.Use] = true
	}

	expected := []string{"proxy", "rotate-logs", "update", "version"}
	for _, name := range expected {
		if !names[name] {
			t.Fatalf("expected subcommand %q", name)
		}
	}
}

func TestBuildRootCmdVersionSubcommand(t *testing.T) {
	cmd := buildRootCmd()
	var versionCmd *cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Use == "version" {
			versionCmd = sub
			break
		}
	}
	if versionCmd == nil {
		t.Fatal("expected version subcommand")
	}

	versionCmd.Run(versionCmd, nil)
}

type mockProvider struct {
	response string
	err      error
}

func (m *mockProvider) Fetch(ctx context.Context, input, systemPrompt string) (string, error) {
	return m.response, m.err
}

func (m *mockProvider) FetchWithHistory(ctx context.Context, input, systemPrompt string, history []provider.Message) (string, error) {
	return m.response, m.err
}

func TestRunSuggestSuccess(t *testing.T) {
	oldSelect := selectProviderFunc
	oldOutput := outputFile
	oldInput := input
	oldProvider := providerName
	oldDebug := dbg
	oldContext := sendContext
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		outputFile = oldOutput
		input = oldInput
		providerName = oldProvider
		dbg = oldDebug
		sendContext = oldContext
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return &mockProvider{response: "=ls -la", err: nil}, nil
	}
	outputFile = filepath.Join(t.TempDir(), "output.txt")
	input = "list files"
	providerName = "mock"
	dbg = false
	sendContext = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	if err := runSuggest(cmd, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}
	if string(content) != "=ls -la" {
		t.Fatalf("expected '=ls -la', got %q", string(content))
	}
}

func TestRunSuggestProviderError(t *testing.T) {
	oldSelect := selectProviderFunc
	oldProvider := providerName
	oldInput := input
	oldDebug := dbg
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		providerName = oldProvider
		input = oldInput
		dbg = oldDebug
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return nil, errors.New("provider error")
	}
	providerName = "mock"
	input = "test"
	dbg = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSuggest(cmd, nil)
	if err == nil {
		t.Fatal("expected error for provider failure")
	}
}

func TestRunSuggestFetchError(t *testing.T) {
	oldSelect := selectProviderFunc
	oldProvider := providerName
	oldInput := input
	oldDebug := dbg
	oldContext := sendContext
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		providerName = oldProvider
		input = oldInput
		dbg = oldDebug
		sendContext = oldContext
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return &mockProvider{response: "", err: errors.New("fetch error")}, nil
	}
	providerName = "mock"
	input = "test"
	dbg = false
	sendContext = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSuggest(cmd, nil)
	if err == nil {
		t.Fatal("expected error for fetch failure")
	}
}

func TestRunSuggestWriteError(t *testing.T) {
	oldSelect := selectProviderFunc
	oldOutput := outputFile
	oldProvider := providerName
	oldInput := input
	oldDebug := dbg
	oldContext := sendContext
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		outputFile = oldOutput
		providerName = oldProvider
		input = oldInput
		dbg = oldDebug
		sendContext = oldContext
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return &mockProvider{response: "=ls", err: nil}, nil
	}
	outputFile = "/nonexistent/path/output.txt"
	providerName = "mock"
	input = "test"
	dbg = false
	sendContext = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	err := runSuggest(cmd, nil)
	if err == nil {
		t.Fatal("expected error for write failure")
	}
}
