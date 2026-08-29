package zsh

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

type zshSession struct {
	pty    *os.File
	cmd    *exec.Cmd
	output bytes.Buffer
	mu     sync.Mutex
	tmpDir string
	done   chan struct{}
}

func (s *zshSession) Close() error {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	close(s.done)
	_ = os.RemoveAll(s.tmpDir)
	return s.pty.Close()
}

func (s *zshSession) readLoop() {
	buf := make([]byte, 1024)
	for {
		select {
		case <-s.done:
			return
		default:
			s.pty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, err := s.pty.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.output.Write(buf[:n])
				s.mu.Unlock()
			}
			if err != nil {
				if nerr, ok := err.(*os.PathError); ok && nerr.Timeout() {
					continue
				}
				return
			}
		}
	}
}

func (s *zshSession) Expect(pattern string, timeout time.Duration) (string, error) {
	start := time.Now()
	for time.Since(start) < timeout {
		s.mu.Lock()
		out := s.output.String()
		s.mu.Unlock()
		if strings.Contains(out, pattern) {
			return out, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.mu.Lock()
	finalOut := s.output.String()
	s.mu.Unlock()
	return finalOut, fmt.Errorf("timeout waiting for pattern %q. Got: %s", pattern, finalOut)
}

func (s *zshSession) RunCommand(cmd string, timeout time.Duration) (string, error) {
	s.mu.Lock()
	s.output.Reset()
	s.mu.Unlock()
	_, _ = s.pty.Write([]byte(cmd + "\r\n"))
	// Wait for the command to be echoed and then for the prompt
	return s.Expect("READY_FOR_COMMAND", timeout)
}

func (s *zshSession) SetMockResponse(response string) error {
	_ = os.Remove(filepath.Join(s.tmpDir, "mock_error"))
	_ = os.Remove(filepath.Join(s.tmpDir, "mock_delay"))
	return os.WriteFile(filepath.Join(s.tmpDir, "mock_response"), []byte(response), 0644)
}

func (s *zshSession) SetMockError(err string) error {
	_ = os.Remove(filepath.Join(s.tmpDir, "mock_response"))
	_ = os.Remove(filepath.Join(s.tmpDir, "mock_delay"))
	return os.WriteFile(filepath.Join(s.tmpDir, "mock_error"), []byte(err), 0644)
}

func (s *zshSession) SetMockDelay(delay time.Duration) error {
	return os.WriteFile(filepath.Join(s.tmpDir, "mock_delay"), []byte(fmt.Sprintf("%d", int64(delay.Seconds()))), 0644)
}

func spawnZsh() (*zshSession, error) {
	return spawnZshWithProvider("openai")
}

func spawnZshWithProvider(provider string) (*zshSession, error) {
	return spawnZshWithProviderAndCache(provider, "")
}

func spawnZshWithProviderAndCache(provider, cacheHome string) (*zshSession, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		return nil, err
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir, err := os.MkdirTemp("", "zsh-test-*")
	if err != nil {
		return nil, err
	}
	if cacheHome == "" {
		cacheHome = tmpDir
	}

	autosuggestDir := filepath.Join(tmpDir, "zsh-autosuggestions")
	const zshAutosuggestionsTag = "v0.7.1"
	cloneCmd := exec.Command("git", "clone", "--depth", "1", "--branch", zshAutosuggestionsTag, "https://github.com/zsh-users/zsh-autosuggestions", autosuggestDir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to clone zsh-autosuggestions: %v, output: %s", err, string(out))
	}

	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockBinContent := `#!/bin/sh
echo "Binary called with: $@" >> "$MOCK_LOG_FILE"
echo "$@" > "$MOCK_LAST_ARGS_FILE"

if [ -f "$MOCK_DELAY_FILE" ]; then
    sleep $(cat "$MOCK_DELAY_FILE")
fi

OUTPUT_FILE=""
arg_list="$@"
set -- $arg_list
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) OUTPUT_FILE="$2"; shift 2;;
    *) shift 1;;
  esac
done

if [ -f "$MOCK_ERROR_FILE" ]; then
    cat "$MOCK_ERROR_FILE" >&2
    exit 1
fi

if [ -f "$MOCK_RESPONSE_FILE" ]; then
    if [ "$OUTPUT_FILE" = "-" ]; then
        cat "$MOCK_RESPONSE_FILE"
    else
        cat "$MOCK_RESPONSE_FILE" > "$OUTPUT_FILE"
    fi
else
    if [ "$OUTPUT_FILE" = "-" ]; then
        echo "+ls"
    else
        echo "+ls" > "$OUTPUT_FILE"
    fi
fi
exit 0
`
	err = os.WriteFile(mockBinPath, []byte(mockBinContent), 0755)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}

	// Setup config.zsh
	configDir := filepath.Join(tmpDir, "smart-suggestion")
	_ = os.MkdirAll(configDir, 0755)
	configContent := fmt.Sprintf(`
OPENAI_API_KEY="fake-key"
ANTHROPIC_API_KEY="fake-key"
SMART_SUGGESTION_AI_PROVIDER="%s"
SMART_SUGGESTION_BINARY="%s"
SMART_SUGGESTION_AUTO_UPDATE="false"
SMART_SUGGESTION_PROXY_MODE="false"
SMART_SUGGESTION_DEBUG="true"
`, provider, mockBinPath)
	_ = os.WriteFile(filepath.Join(configDir, "config.zsh"), []byte(configContent), 0644)

	cmd := exec.Command("zsh", "-f", "-i")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+cacheHome,
		"XDG_CONFIG_HOME="+tmpDir,
		"TERM=xterm-256color",
		"MOCK_ERROR_FILE="+filepath.Join(tmpDir, "mock_error"),
		"MOCK_RESPONSE_FILE="+filepath.Join(tmpDir, "mock_response"),
		"MOCK_DELAY_FILE="+filepath.Join(tmpDir, "mock_delay"),
		"MOCK_LAST_ARGS_FILE="+filepath.Join(tmpDir, "last_args"),
		"MOCK_LOG_FILE="+filepath.Join(tmpDir, "mock.log"),
	)

	f, err := pty.Start(cmd)
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}

	session := &zshSession{
		pty:    f,
		cmd:    cmd,
		tmpDir: tmpDir,
		done:   make(chan struct{}),
	}
	go session.readLoop()

	time.Sleep(200 * time.Millisecond)

	_, _ = session.pty.Write([]byte("export PS1='READY_FOR_COMMAND'\r\n"))
	// Wait for the command to be echoed AND for the prompt to appear
	// We can use a trick: export the prompt then echo a unique marker
	_, _ = session.pty.Write([]byte("echo PROMPT_SET\r\n"))
	_, err = session.Expect("PROMPT_SET", 10*time.Second)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to set prompt: %v. Output: %s", err, session.output.String())
	}

	_, _ = session.pty.Write([]byte(fmt.Sprintf("source %s/zsh-autosuggestions.zsh\r\n", autosuggestDir)))
	_, _ = session.pty.Write([]byte("echo AUTOSUGGEST_SOURCED\r\n"))
	_, err = session.Expect("AUTOSUGGEST_SOURCED", 10*time.Second)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to source zsh-autosuggestions: %v. Output: %s", err, session.output.String())
	}

	session.output.Reset()
	_, _ = session.pty.Write([]byte(fmt.Sprintf("source %s\r\n", pluginPath)))
	_, _ = session.pty.Write([]byte("echo PLUGIN_SOURCED\r\n"))
	_, err = session.Expect("PLUGIN_SOURCED", 10*time.Second)
	if err != nil {
		session.Close()
		return nil, fmt.Errorf("failed to source plugin: %v. Output: %s", err, session.output.String())
	}

	return session, nil
}

func (s *zshSession) TriggerSuggest() {
	// Send ^O (Ctrl-O) which is bound to _do_smart_suggestion
	_, _ = s.pty.Write([]byte{0x0f})
}

func suggestionRequestDirs(cacheDir string) ([]string, error) {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "suggest.") {
			dirs = append(dirs, filepath.Join(cacheDir, entry.Name()))
		}
	}
	return dirs, nil
}

func waitForSuggestionRequestCount(cacheDir string, want int, timeout time.Duration) ([]string, error) {
	deadline := time.Now().Add(timeout)
	for {
		dirs, err := suggestionRequestDirs(cacheDir)
		if err != nil {
			return nil, err
		}
		if len(dirs) == want {
			return dirs, nil
		}
		if time.Now().After(deadline) {
			return dirs, fmt.Errorf("got %d suggestion request directories, want %d", len(dirs), want)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAppendSuggestion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	// 1. Set up an "Append" suggestion
	session.SetMockResponse("+appended_text")

	// 2. Type 'echo '
	session.pty.Write([]byte("echo "))
	time.Sleep(200 * time.Millisecond)

	// 3. Trigger suggestion
	session.TriggerSuggest()

	// Wait for binary to be called and widget to finish
	time.Sleep(2 * time.Second)

	lastArgs, err := os.ReadFile(filepath.Join(session.tmpDir, "last_args"))
	if err != nil {
		mockLog, _ := os.ReadFile(filepath.Join(session.tmpDir, "mock.log"))
		debugLog, _ := os.ReadFile(filepath.Join(session.tmpDir, "smart-suggestion/debug.log"))
		t.Fatalf("Binary was not called: %v. Mock log: %s. Debug log: %s. Output: %s", err, string(mockLog), string(debugLog), session.output.String())
	}
	if !strings.Contains(string(lastArgs), "--input echo") {
		t.Errorf("Expected input 'echo' to be passed to binary, but got: %s", string(lastArgs))
	}

	// 4. Verify the suggestion is applied
	// By default, zsh-autosuggestions doesn't automatically put ghost text into BUFFER
	// until it's accepted. We'll send Ctrl-F (^F) which is a common default to accept.
	session.pty.Write([]byte{0x06}) // ^F
	time.Sleep(500 * time.Millisecond)

	session.RunCommand("", 2*time.Second)

	// 5. Expect the output
	_, err = session.Expect("appended_text", 2*time.Second)
	if err != nil {
		t.Fatalf("Suggestion was not appended or executed. Output: %s", session.output.String())
	}
}

func TestReplaceSuggestion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	// 1. Set up a "Replace" suggestion
	// The '=' prefix tells the plugin to replace the current buffer
	session.SetMockResponse("=echo replaced_command")

	// 2. Type something different
	session.pty.Write([]byte("echo original_command"))
	time.Sleep(200 * time.Millisecond)

	// 3. Trigger suggestion
	session.TriggerSuggest()

	// Wait for the suggestion to be applied
	time.Sleep(2 * time.Second)

	// 4. Verify the replacement by executing the command
	// If the buffer was replaced, pressing Enter should run "echo replaced_command"
	session.RunCommand("", 2*time.Second) // Just press Enter (RunCommand appends \r\n)

	// 5. Expect the output of the *replaced* command
	_, err = session.Expect("replaced_command", 2*time.Second)
	if err != nil {
		t.Fatalf("Buffer was not replaced. Expected output 'replaced_command' not found. Output: %s", session.output.String())
	}
}

func TestErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	session.SetMockError("API_ERROR_500")

	session.TriggerSuggest()

	_, err = session.Expect("API_ERROR_500", 10*time.Second)
	if err != nil {
		t.Fatalf("Error message did not appear: %v. Output: %s", err, session.output.String())
	}
}

func TestErrorHandlingPreservesInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	session.SetMockError("API_ERROR_500")

	// Type something to test preservation of user input
	session.pty.Write([]byte("echo my_original_input"))
	time.Sleep(200 * time.Millisecond)

	session.TriggerSuggest()

	_, err = session.Expect("API_ERROR_500", 10*time.Second)
	if err != nil {
		t.Fatalf("Error message did not appear: %v. Output: %s", err, session.output.String())
	}

	// Press Enter to execute the preserved input on the new prompt
	session.RunCommand("", 2*time.Second)

	_, err = session.Expect("my_original_input", 2*time.Second)
	if err != nil {
		t.Fatalf("Original input was not preserved after error. Output: %s", session.output.String())
	}
}

func TestTimeoutHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	session.SetMockDelay(2 * time.Second)
	session.SetMockResponse("+long_running_command")

	session.TriggerSuggest()

	_, err = session.Expect("Press <Ctrl-c> to cancel", 5*time.Second)
	if err != nil {
		t.Fatalf("Loading animation did not appear: %v. Output: %s", err, session.output.String())
	}
}

func TestConfigurationSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	// 1. Initial request with default provider (openai)
	session.TriggerSuggest()

	// Wait for binary to be called
	time.Sleep(2 * time.Second)

	lastArgs, err := os.ReadFile(filepath.Join(session.tmpDir, "last_args"))
	if err != nil {
		mockLog, _ := os.ReadFile(filepath.Join(session.tmpDir, "mock.log"))
		debugLog, _ := os.ReadFile(filepath.Join(session.tmpDir, "smart-suggestion/debug.log"))
		t.Fatalf("Binary not called for initial request: %v. Mock log: %s. Debug log: %s. Output: %s", err, string(mockLog), string(debugLog), session.output.String())
	}
	if !strings.Contains(string(lastArgs), "--provider openai") {
		t.Errorf("Expected initial provider 'openai', got: %s", string(lastArgs))
	}

	// 2. Change provider by updating the config file (not environment variable)
	// The plugin re-sources config.zsh on each _fetch_suggestions call
	configPath := filepath.Join(session.tmpDir, "smart-suggestion", "config.zsh")
	newConfigContent := fmt.Sprintf(`
OPENAI_API_KEY="fake-key"
ANTHROPIC_API_KEY="fake-key"
SMART_SUGGESTION_AI_PROVIDER="anthropic"
SMART_SUGGESTION_BINARY="%s"
SMART_SUGGESTION_AUTO_UPDATE="false"
SMART_SUGGESTION_PROXY_MODE="false"
SMART_SUGGESTION_DEBUG="true"
`, filepath.Join(session.tmpDir, "smart-suggestion-bin"))
	os.WriteFile(configPath, []byte(newConfigContent), 0644)
	time.Sleep(500 * time.Millisecond)

	// 3. Trigger suggestion again and verify it uses the NEW provider
	os.Remove(filepath.Join(session.tmpDir, "last_args"))
	session.TriggerSuggest()

	time.Sleep(2 * time.Second)

	lastArgs, err = os.ReadFile(filepath.Join(session.tmpDir, "last_args"))
	if err != nil {
		mockLog, _ := os.ReadFile(filepath.Join(session.tmpDir, "mock.log"))
		debugLog, _ := os.ReadFile(filepath.Join(session.tmpDir, "smart-suggestion/debug.log"))
		t.Fatalf("Binary not called after changing provider: %v. Mock log: %s. Debug log: %s. Output: %s", err, string(mockLog), string(debugLog), session.output.String())
	}
	if !strings.Contains(string(lastArgs), "--provider anthropic") {
		t.Errorf("Expected provider 'anthropic' after export, but got: %s", string(lastArgs))
	}
}

func TestPluginRegistration(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir, err := os.MkdirTemp("", "zsh-test-*")
	if err != nil {
		t.Fatalf("Failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockBinContent := "#!/bin/sh\nexit 0\n"
	err = os.WriteFile(mockBinPath, []byte(mockBinContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock binary: %v", err)
	}

	script := fmt.Sprintf(`
export SMART_SUGGESTION_BINARY=%s
source %s
if (( $+widgets[_do_smart_suggestion] )); then
    echo "WIDGET_REGISTERED"
else
    echo "WIDGET_NOT_REGISTERED"
fi

bindkey "^o" | grep -q "_do_smart_suggestion" && echo "KEYBIND_REGISTERED" || echo "KEYBIND_NOT_REGISTERED"
`, mockBinPath, pluginPath)

	cmd := exec.Command("zsh", "-f", "-c", script)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+tmpDir,
		"XDG_CONFIG_HOME="+tmpDir,
		"OPENAI_API_KEY=fake-key",
		"SMART_SUGGESTION_AI_PROVIDER=openai",
		"SMART_SUGGESTION_BINARY="+mockBinPath,
		"SMART_SUGGESTION_AUTO_UPDATE=false",
		"SMART_SUGGESTION_PROXY_MODE=false",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command failed with %v: %s", err, string(out))
	}

	output := string(out)
	if !strings.Contains(output, "WIDGET_REGISTERED") {
		t.Errorf("Widget _do_smart_suggestion not registered. Output:\n%s", output)
	}
	if !strings.Contains(output, "KEYBIND_REGISTERED") {
		t.Errorf("Keybinding ^o not registered to _do_smart_suggestion. Output:\n%s", output)
	}
}

func TestPluginSourcing(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir, err := os.MkdirTemp("", "zsh-test-*")
	if err != nil {
		t.Fatalf("Failed to create tmp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockBinContent := "#!/bin/sh\nexit 0\n"
	err = os.WriteFile(mockBinPath, []byte(mockBinContent), 0755)
	if err != nil {
		t.Fatalf("Failed to create mock binary: %v", err)
	}

	script := fmt.Sprintf(`
export SMART_SUGGESTION_BINARY=%s
source %s
if (( $+functions[_do_smart_suggestion] )); then
    echo "FUNCTION_DEFINED"
else
    echo "FUNCTION_NOT_DEFINED"
fi
`, mockBinPath, pluginPath)

	cmd := exec.Command("zsh", "-f", "-c", script)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+tmpDir,
		"XDG_CONFIG_HOME="+tmpDir,
		"OPENAI_API_KEY=fake-key",
		"SMART_SUGGESTION_AI_PROVIDER=openai",
		"SMART_SUGGESTION_BINARY="+mockBinPath,
		"SMART_SUGGESTION_AUTO_UPDATE=false",
		"SMART_SUGGESTION_PROXY_MODE=false",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command failed with %v: %s", err, string(out))
	}

	if !strings.Contains(string(out), "FUNCTION_DEFINED") {
		t.Errorf("Plugin did not define _do_smart_suggestion. Output:\n%s", string(out))
	}
}

func TestProxyNotStartedWithoutTTY(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir := t.TempDir()
	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockLogPath := filepath.Join(tmpDir, "proxy.log")
	mockBinContent := "#!/bin/sh\nprintf '%s\\n' \"$*\" > \"$MOCK_PROXY_LOG\"\n"
	if err := os.WriteFile(mockBinPath, []byte(mockBinContent), 0755); err != nil {
		t.Fatalf("Failed to create mock binary: %v", err)
	}

	script := fmt.Sprintf("source %q\necho PLUGIN_SOURCED\n", pluginPath)
	cmd := exec.CommandContext(t.Context(), "zsh", "-f", "-i", "-c", script)
	cmd.Dir = projectRoot
	cmd.Stdin = strings.NewReader("")
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+tmpDir,
		"XDG_CONFIG_HOME="+tmpDir,
		"OPENAI_API_KEY=fake-key",
		"SMART_SUGGESTION_AI_PROVIDER=openai",
		"SMART_SUGGESTION_BINARY="+mockBinPath,
		"SMART_SUGGESTION_AUTO_UPDATE=false",
		"SMART_SUGGESTION_PROXY_MODE=true",
		"MOCK_PROXY_LOG="+mockLogPath,
		"TMUX=",
		"KITTY_LISTEN_ON=",
		"GHOSTTY_RESOURCES_DIR=",
		"HERDR_ENV=",
		"SMART_SUGGESTION_PROXY_ACTIVE=",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Command failed with %v: %s", err, string(out))
	}
	if !strings.Contains(string(out), "PLUGIN_SOURCED") {
		t.Fatalf("Plugin did not return control without a TTY. Output:\n%s", string(out))
	}
	if _, err := os.Stat(mockLogPath); !os.IsNotExist(err) {
		t.Fatalf("Proxy started without a TTY; stat error: %v", err)
	}
}

func TestProxyStartedWithTTY(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir := t.TempDir()
	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockBinContent := "#!/bin/sh\necho \"PROXY_STARTED:$*\"\n"
	if err := os.WriteFile(mockBinPath, []byte(mockBinContent), 0755); err != nil {
		t.Fatalf("Failed to create mock binary: %v", err)
	}

	script := fmt.Sprintf("source %q\n", pluginPath)
	cmd := exec.CommandContext(t.Context(), "zsh", "-f", "-i", "-c", script)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+tmpDir,
		"XDG_CONFIG_HOME="+tmpDir,
		"OPENAI_API_KEY=fake-key",
		"SMART_SUGGESTION_AI_PROVIDER=openai",
		"SMART_SUGGESTION_BINARY="+mockBinPath,
		"SMART_SUGGESTION_AUTO_UPDATE=false",
		"SMART_SUGGESTION_PROXY_MODE=true",
		"TMUX=",
		"KITTY_LISTEN_ON=",
		"GHOSTTY_RESOURCES_DIR=",
		"HERDR_ENV=",
		"SMART_SUGGESTION_PROXY_ACTIVE=",
	)

	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("Failed to start zsh with a TTY: %v", err)
	}
	defer terminal.Close()

	outputCh := make(chan string, 1)
	go func() {
		var output bytes.Buffer
		buf := make([]byte, 1024)
		for {
			n, readErr := terminal.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
			}
			if readErr != nil {
				outputCh <- output.String()
				return
			}
		}
	}()

	var output string
	select {
	case output = <-outputCh:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("Timed out waiting for proxy to start")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Proxy process failed: %v. Output:\n%s", err, output)
	}

	if !strings.Contains(output, "PROXY_STARTED:proxy --scrollback-lines 100") {
		t.Fatalf("Proxy did not start with a TTY. Output:\n%s", output)
	}
}

func TestProxyNotStartedInHerdr(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir := t.TempDir()
	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockBinContent := "#!/bin/sh\necho \"PROXY_STARTED:$*\"\n"
	if err := os.WriteFile(mockBinPath, []byte(mockBinContent), 0755); err != nil {
		t.Fatalf("Failed to create mock binary: %v", err)
	}

	script := fmt.Sprintf("source %q\necho PLUGIN_SOURCED\n", pluginPath)
	cmd := exec.CommandContext(t.Context(), "zsh", "-f", "-i", "-c", script)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+tmpDir,
		"XDG_CONFIG_HOME="+tmpDir,
		"OPENAI_API_KEY=fake-key",
		"SMART_SUGGESTION_AI_PROVIDER=openai",
		"SMART_SUGGESTION_BINARY="+mockBinPath,
		"SMART_SUGGESTION_AUTO_UPDATE=false",
		"SMART_SUGGESTION_PROXY_MODE=true",
		"TMUX=",
		"KITTY_LISTEN_ON=",
		"GHOSTTY_RESOURCES_DIR=",
		"HERDR_ENV=1",
		"HERDR_PANE_ID=w1:p1",
		"SMART_SUGGESTION_PROXY_ACTIVE=",
	)

	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("Failed to start zsh with a TTY: %v", err)
	}
	defer terminal.Close()

	outputCh := make(chan string, 1)
	go func() {
		var output bytes.Buffer
		buf := make([]byte, 1024)
		for {
			n, readErr := terminal.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
			}
			if readErr != nil {
				outputCh <- output.String()
				return
			}
		}
	}()

	var output string
	select {
	case output = <-outputCh:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("Timed out waiting for plugin to source")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Plugin process failed: %v. Output:\n%s", err, output)
	}

	if !strings.Contains(output, "PLUGIN_SOURCED") {
		t.Fatalf("Plugin did not return control in Herdr. Output:\n%s", output)
	}
	if strings.Contains(output, "PROXY_STARTED") {
		t.Fatalf("Proxy started in Herdr session. Output:\n%s", output)
	}
}

func TestProxyStartedInHerdrWithoutPaneID(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get wd: %v", err)
	}
	projectRoot, err := filepath.Abs(filepath.Join(cwd, "..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}
	pluginPath := filepath.Join(projectRoot, "smart-suggestion.plugin.zsh")

	tmpDir := t.TempDir()
	mockBinPath := filepath.Join(tmpDir, "smart-suggestion-bin")
	mockBinContent := "#!/bin/sh\necho \"PROXY_STARTED:$*\"\n"
	if err := os.WriteFile(mockBinPath, []byte(mockBinContent), 0755); err != nil {
		t.Fatalf("Failed to create mock binary: %v", err)
	}

	script := fmt.Sprintf("source %q\n", pluginPath)
	cmd := exec.CommandContext(t.Context(), "zsh", "-f", "-i", "-c", script)
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"ZDOTDIR="+tmpDir,
		"HOME="+tmpDir,
		"XDG_CACHE_HOME="+tmpDir,
		"XDG_CONFIG_HOME="+tmpDir,
		"OPENAI_API_KEY=fake-key",
		"SMART_SUGGESTION_AI_PROVIDER=openai",
		"SMART_SUGGESTION_BINARY="+mockBinPath,
		"SMART_SUGGESTION_AUTO_UPDATE=false",
		"SMART_SUGGESTION_PROXY_MODE=true",
		"TMUX=",
		"KITTY_LISTEN_ON=",
		"GHOSTTY_RESOURCES_DIR=",
		"HERDR_ENV=1",
		"HERDR_PANE_ID=",
		"SMART_SUGGESTION_PROXY_ACTIVE=",
	)

	terminal, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("Failed to start zsh with a TTY: %v", err)
	}
	defer terminal.Close()

	outputCh := make(chan string, 1)
	go func() {
		var output bytes.Buffer
		buf := make([]byte, 1024)
		for {
			n, readErr := terminal.Read(buf)
			if n > 0 {
				output.Write(buf[:n])
			}
			if readErr != nil {
				outputCh <- output.String()
				return
			}
		}
	}()

	var output string
	select {
	case output = <-outputCh:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("Timed out waiting for proxy to start")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Proxy process failed: %v. Output:\n%s", err, output)
	}

	if !strings.Contains(output, "PROXY_STARTED:proxy --scrollback-lines 100") {
		t.Fatalf("Proxy did not start without HERDR_PANE_ID. Output:\n%s", output)
	}
}

func TestNumericReplaceSuggestionIsNotReadAsPID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	session, err := spawnZsh()
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	if err := session.SetMockResponse("=42"); err != nil {
		t.Fatal(err)
	}
	if err := session.SetMockDelay(time.Second); err != nil {
		t.Fatal(err)
	}

	session.pty.Write([]byte("echo original"))
	time.Sleep(200 * time.Millisecond)

	session.TriggerSuggest()
	cacheDir := filepath.Join(session.tmpDir, "smart-suggestion")
	if _, err := waitForSuggestionRequestCount(cacheDir, 1, 5*time.Second); err != nil {
		t.Fatalf("suggestion request did not start: %v", err)
	}
	if requestDirs, err := waitForSuggestionRequestCount(cacheDir, 0, 5*time.Second); err != nil {
		t.Fatalf("suggestion request directories were not removed: %v (%v)", requestDirs, err)
	}

	if output, err := session.RunCommand("", 5*time.Second); err != nil {
		t.Fatalf("numeric replace suggestion did not finish: %v. Output: %s", err, output)
	}

	_, err = session.Expect("42", 5*time.Second)
	if err != nil {
		t.Fatalf("numeric replace suggestion was not applied. Output: %s", session.output.String())
	}
}

func TestPluginSecuresExistingCacheFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cacheHome := t.TempDir()
	cacheDir := filepath.Join(cacheHome, "smart-suggestion")
	if err := os.Mkdir(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	debugLog := filepath.Join(cacheDir, "debug.log")
	if err := os.WriteFile(debugLog, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(debugLog, 0o644); err != nil {
		t.Fatal(err)
	}

	session, err := spawnZshWithProviderAndCache("openai", cacheHome)
	if err != nil {
		t.Fatalf("Failed to spawn zsh: %v", err)
	}
	defer session.Close()

	info, err := os.Stat(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("cache directory permissions are %o, want 700", info.Mode().Perm())
	}
	info, err = os.Stat(debugLog)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("debug log permissions are %o, want 600", info.Mode().Perm())
	}
}

func TestConcurrentSuggestionsUseSeparateRequestDirectories(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cacheHome := t.TempDir()
	first, err := spawnZshWithProviderAndCache("openai", cacheHome)
	if err != nil {
		t.Fatalf("Failed to spawn first zsh: %v", err)
	}
	defer first.Close()
	second, err := spawnZshWithProviderAndCache("openai", cacheHome)
	if err != nil {
		t.Fatalf("Failed to spawn second zsh: %v", err)
	}
	defer second.Close()

	if err := first.SetMockResponse("=echo first_concurrent_suggestion"); err != nil {
		t.Fatal(err)
	}
	if err := first.SetMockDelay(2 * time.Second); err != nil {
		t.Fatal(err)
	}
	if err := second.SetMockResponse("=echo second_concurrent_suggestion"); err != nil {
		t.Fatal(err)
	}
	if err := second.SetMockDelay(2 * time.Second); err != nil {
		t.Fatal(err)
	}

	first.pty.Write([]byte("echo first_original"))
	second.pty.Write([]byte("echo second_original"))
	time.Sleep(200 * time.Millisecond)
	first.TriggerSuggest()
	second.TriggerSuggest()

	cacheDir := filepath.Join(cacheHome, "smart-suggestion")
	if requestDirs, err := waitForSuggestionRequestCount(cacheDir, 2, 5*time.Second); err != nil {
		t.Fatalf("expected 2 concurrent request directories: %v (%v)", requestDirs, err)
	}

	if requestDirs, err := waitForSuggestionRequestCount(cacheDir, 0, 5*time.Second); err != nil {
		t.Fatalf("suggestion request directories were not removed: %v (%v)", requestDirs, err)
	}

	firstOutput, err := first.RunCommand("", 3*time.Second)
	if err != nil {
		t.Fatalf("first suggestion did not finish: %v. Output: %s", err, firstOutput)
	}
	if !strings.Contains(firstOutput, "first_concurrent_suggestion") {
		t.Fatalf("first shell received the wrong suggestion. Output: %s", firstOutput)
	}
	secondOutput, err := second.RunCommand("", 3*time.Second)
	if err != nil {
		t.Fatalf("second suggestion did not finish: %v. Output: %s", err, secondOutput)
	}
	if !strings.Contains(secondOutput, "second_concurrent_suggestion") {
		t.Fatalf("second shell received the wrong suggestion. Output: %s", secondOutput)
	}
}
