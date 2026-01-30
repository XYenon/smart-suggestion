package main

import (
	"bytes"
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
	defer func() { systemPrompt = original }()

	systemPrompt = ""
	if got := resolveSystemPrompt(); got != defaultSystemPrompt {
		t.Fatalf("expected default prompt, got %q", got)
	}

	systemPrompt = "custom"
	if got := resolveSystemPrompt(); got != "custom" {
		t.Fatalf("expected custom prompt, got %q", got)
	}
}

func TestBuildPromptWithScrollback(t *testing.T) {
	old := buildContextInfoFunc
	buildContextInfoFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "", nil
	}
	t.Cleanup(func() { buildContextInfoFunc = old })

	file := filepath.Join(t.TempDir(), "scrollback.txt")
	if err := os.WriteFile(file, []byte("first\nsecond\n"), 0644); err != nil {
		t.Fatalf("failed to write scrollback file: %v", err)
	}

	buildContextInfoFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "second", nil
	}

	if got := buildPrompt(1, file, true); !bytes.Contains([]byte(got), []byte("second")) {
		t.Fatalf("expected scrollback content, got %q", got)
	}
}

func TestShouldRequireProviderFlags(t *testing.T) {
	if shouldRequireProviderFlags([]string{"smart-suggestion"}) {
		t.Fatal("expected no requirements for root usage")
	}
	if shouldRequireProviderFlags([]string{"smart-suggestion", "proxy"}) {
		t.Fatal("expected no requirements for proxy")
	}
	if !shouldRequireProviderFlags([]string{"smart-suggestion", "suggest"}) {
		t.Fatal("expected requirements for suggest")
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

	oldExit := exitFunc
	oldLogFile := proxyLogFile
	oldDebug := dbg
	t.Cleanup(func() {
		exitFunc = oldExit
		proxyLogFile = oldLogFile
		dbg = oldDebug
	})

	exitFunc = func(code int) {}
	proxyLogFile = file
	dbg = false
	runRotateLogs(nil, nil)
}

func TestRunRotateLogsMissingFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing.log")
	if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write log: %v", err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatalf("failed to remove log: %v", err)
	}

	oldExit := exitFunc
	oldLogFile := proxyLogFile
	oldDebug := dbg
	t.Cleanup(func() {
		exitFunc = oldExit
		proxyLogFile = oldLogFile
		dbg = oldDebug
	})

	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	proxyLogFile = file
	dbg = false

	runRotateLogs(nil, nil)
	if exitCode != -1 {
		t.Fatalf("expected no exit for missing file, got %d", exitCode)
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

func TestBuildPromptNoContext(t *testing.T) {
	old := buildContextInfoFunc
	oldPrompt := systemPrompt
	t.Cleanup(func() {
		buildContextInfoFunc = old
		systemPrompt = oldPrompt
	})

	systemPrompt = "base prompt"
	buildContextInfoFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "extra context info", nil
	}

	prompt := buildPrompt(10, "", false)
	if bytes.Contains([]byte(prompt), []byte("extra context info")) {
		t.Fatal("expected no context when sendContext is false")
	}
}

func TestBuildPromptContextError(t *testing.T) {
	old := buildContextInfoFunc
	oldPrompt := systemPrompt
	t.Cleanup(func() {
		buildContextInfoFunc = old
		systemPrompt = oldPrompt
	})

	systemPrompt = ""
	buildContextInfoFunc = func(scrollbackLines int, scrollbackFile string) (string, error) {
		return "", errors.New("fail")
	}

	prompt := buildPrompt(10, "", true)
	if prompt != defaultSystemPrompt {
		t.Fatalf("expected default prompt on error, got %q", prompt)
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

func TestMarkRequiredFlags(t *testing.T) {
	rootCmd := &cobra.Command{}
	rootCmd.Flags().String("provider", "", "")
	rootCmd.Flags().String("input", "", "")
	rotateCmd := &cobra.Command{}
	rotateCmd.Flags().String("log-file", "", "")

	markRequiredFlags(rootCmd, rotateCmd)
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

func TestRunRotateLogsEmptyFile(t *testing.T) {
	oldExit := exitFunc
	oldLogFile := proxyLogFile
	oldDebug := dbg
	t.Cleanup(func() {
		exitFunc = oldExit
		proxyLogFile = oldLogFile
		dbg = oldDebug
	})

	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	proxyLogFile = ""
	dbg = false

	runRotateLogs(nil, nil)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1 for empty log file, got %d", exitCode)
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

	oldExit := exitFunc
	oldLogFile := proxyLogFile
	oldDebug := dbg
	oldStdout := os.Stdout
	t.Cleanup(func() {
		exitFunc = oldExit
		proxyLogFile = oldLogFile
		dbg = oldDebug
		os.Stdout = oldStdout
	})

	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	proxyLogFile = file
	dbg = false

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	runRotateLogs(nil, nil)
	_ = w.Close()
	out, _ := io.ReadAll(r)

	if exitCode != 1 {
		t.Fatalf("expected exit code 1 for rotate error, got %d", exitCode)
	}
	if bytes.Contains(out, []byte("Successfully rotated")) {
		t.Fatalf("expected no success output on failure, got %q", string(out))
	}
}

func TestWriteSuggestionWriteError(t *testing.T) {
	err := writeSuggestion("/nonexistent/dir/file.txt", "test")
	if err == nil {
		t.Fatal("expected error for write to nonexistent directory")
	}
}

func TestMarkRequiredFlagsRotateLogs(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	os.Args = []string{"smart-suggestion", "rotate-logs"}
	rootCmd := &cobra.Command{}
	rootCmd.Flags().String("provider", "", "")
	rootCmd.Flags().String("input", "", "")
	rotateCmd := &cobra.Command{}
	rotateCmd.Flags().String("log-file", "", "")

	markRequiredFlags(rootCmd, rotateCmd)
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

func TestShouldRequireProviderFlagsSubcommands(t *testing.T) {
	cases := []struct {
		args     []string
		expected bool
	}{
		{[]string{"smart-suggestion", "rotate-logs"}, false},
		{[]string{"smart-suggestion", "version"}, false},
		{[]string{"smart-suggestion", "update"}, false},
		{[]string{"smart-suggestion", "other"}, true},
	}

	for _, tc := range cases {
		got := shouldRequireProviderFlags(tc.args)
		if got != tc.expected {
			t.Fatalf("for %v: expected %v, got %v", tc.args, tc.expected, got)
		}
	}
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
	oldExit := exitFunc
	oldOutput := outputFile
	oldInput := input
	oldProvider := providerName
	oldDebug := dbg
	oldContext := sendContext
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		exitFunc = oldExit
		outputFile = oldOutput
		input = oldInput
		providerName = oldProvider
		dbg = oldDebug
		sendContext = oldContext
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return &mockProvider{response: "=ls -la", err: nil}, nil
	}
	exitFunc = func(code int) {}
	outputFile = filepath.Join(t.TempDir(), "output.txt")
	input = "list files"
	providerName = "mock"
	dbg = false
	sendContext = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	runSuggest(cmd, nil)

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
	oldExit := exitFunc
	oldProvider := providerName
	oldDebug := dbg
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		exitFunc = oldExit
		providerName = oldProvider
		dbg = oldDebug
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return nil, errors.New("provider error")
	}
	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	providerName = "mock"
	dbg = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	runSuggest(cmd, nil)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRunSuggestFetchError(t *testing.T) {
	oldSelect := selectProviderFunc
	oldExit := exitFunc
	oldProvider := providerName
	oldDebug := dbg
	oldContext := sendContext
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		exitFunc = oldExit
		providerName = oldProvider
		dbg = oldDebug
		sendContext = oldContext
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return &mockProvider{response: "", err: errors.New("fetch error")}, nil
	}
	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	providerName = "mock"
	dbg = false
	sendContext = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	runSuggest(cmd, nil)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}

func TestRunSuggestWriteError(t *testing.T) {
	oldSelect := selectProviderFunc
	oldExit := exitFunc
	oldOutput := outputFile
	oldProvider := providerName
	oldDebug := dbg
	oldContext := sendContext
	t.Cleanup(func() {
		selectProviderFunc = oldSelect
		exitFunc = oldExit
		outputFile = oldOutput
		providerName = oldProvider
		dbg = oldDebug
		sendContext = oldContext
	})

	selectProviderFunc = func(cmd *cobra.Command) (provider.Provider, error) {
		return &mockProvider{response: "=ls", err: nil}, nil
	}
	exitCode := -1
	exitFunc = func(code int) {
		exitCode = code
	}
	outputFile = "/nonexistent/path/output.txt"
	providerName = "mock"
	dbg = false
	sendContext = false

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())

	runSuggest(cmd, nil)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
}
