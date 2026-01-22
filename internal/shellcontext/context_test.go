package shellcontext

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestGetSystemInfo_DarwinError(t *testing.T) {
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "sw_vers" {
			return exec.Command("false")
		}
		return exec.Command("echo", "")
	}

	oldGOOS := runtimeGOOS
	runtimeGOOS = "darwin"
	defer func() { runtimeGOOS = oldGOOS }()

	got := getSystemInfo()
	if got != "Your system is macOS." {
		t.Errorf("expected default macOS msg, got %q", got)
	}
}

func TestGetSystemInfo_DarwinSuccess(t *testing.T) {
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "sw_vers" {
			return exec.Command("echo", "ProductName: macOS\nProductVersion: 14.0")
		}
		return exec.Command("echo", "")
	}

	oldGOOS := runtimeGOOS
	runtimeGOOS = "darwin"
	defer func() { runtimeGOOS = oldGOOS }()

	got := getSystemInfo()
	if !strings.Contains(got, "macOS") {
		t.Errorf("expected system info to contain macOS, got %q", got)
	}
}

func TestGetSystemInfo_LinuxNoReleaseFiles(t *testing.T) {
	oldGOOS := runtimeGOOS
	runtimeGOOS = "linux"
	defer func() { runtimeGOOS = oldGOOS }()

	got := getSystemInfo()
	if got != "Your system is Linux." {
		t.Fatalf("expected Linux default, got %q", got)
	}
}

func TestGetUserID_Error(t *testing.T) {
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "id" {
			return exec.Command("false")
		}
		return exec.Command("echo", "")
	}

	got := getUserID()
	if got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}
}

func TestGetTerminalScrollbackWithTput_Error(t *testing.T) {
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "tput" {
			return exec.Command("false")
		}
		return exec.Command("echo", "")
	}

	_, err := getTerminalScrollbackWithTput()
	if err == nil {
		t.Error("expected error, got nil")
	}
}

func TestGetScreenScrollback_NotInScreen(t *testing.T) {
	t.Setenv("STY", "")

	_, err := getScreenScrollback()
	if err == nil {
		t.Error("expected error when not in screen session")
	}
	if !strings.Contains(err.Error(), "not in a screen session") {
		t.Errorf("expected 'not in a screen session' error, got %v", err)
	}
}

func TestDoGetScrollback_SessionLogFail(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("SMART_SUGGESTION_SESSION_ID", "test-session")
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("STY", "")

	// Create base proxy log
	baseLog := filepath.Join(tempDir, "smart-suggestion", "proxy.log")
	os.MkdirAll(filepath.Dir(baseLog), 0755)
	os.WriteFile(baseLog, []byte("base log"), 0644)

	// Mock commands
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("echo", "")
	}

	got, err := getScrollback(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "base log" {
		t.Errorf("expected base log, got %q", got)
	}
}

func TestReadLatestLines(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"
	got, err := readLatestLines(content, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "line3\nline4\nline5"
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}

	got, err = readLatestLines(content, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("expected %q, got %q", content, got)
	}
}

func TestReadLatestProxyContent(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "proxy.log")

	lines := []string{}
	for i := 1; i <= 60; i++ {
		lines = append(lines, "line"+strconv.Itoa(i))
	}

	err := os.WriteFile(logFile, []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}

	got, err := readLatestProxyContent(logFile, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// readLatestProxyContent now takes maxLines as a parameter
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 60 {
		t.Errorf("expected 60 lines (all lines since < 100), got %d", len(gotLines))
	}
	if gotLines[0] != "line1" {
		t.Errorf("expected first line to be line1, got %q", gotLines[0])
	}
	if gotLines[59] != "line60" {
		t.Errorf("expected last line to be line60, got %q", gotLines[59])
	}
}

func TestBuildContextInfo(t *testing.T) {
	t.Setenv("USER", "testuser")
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("SMART_SUGGESTION_ALIASES", "alias ls='ls -G'")
	t.Setenv("SMART_SUGGESTION_HISTORY", "ls\ncd /tmp")

	got, err := BuildContextInfo(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(got, "testuser") {
		t.Error("expected output to contain username")
	}
	if !strings.Contains(got, "alias ls='ls -G'") {
		t.Error("expected output to contain aliases")
	}
	if !strings.Contains(got, "ls\ncd /tmp") {
		t.Error("expected output to contain history")
	}
}

func TestBuildContextInfo_NoScrollback(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("SMART_SUGGESTION_SESSION_ID", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("STY", "")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()
	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "tput" {
			return exec.Command("echo", "24")
		}
		return exec.Command("echo", "")
	}

	got, err := BuildContextInfo(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "# Scrollback:") {
		t.Error("expected no scrollback in context")
	}
}

func TestGetSystemInfo(t *testing.T) {
	got := getSystemInfo()
	if got == "" {
		t.Error("expected system info to be non-empty")
	}
}

func TestGetUserID(t *testing.T) {
	got := getUserID()
	if got == "" {
		t.Error("expected user ID to be non-empty")
	}
}

func TestGetUserID_Mock(t *testing.T) {
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "id" {
			return exec.Command("echo", "uid=501(user) gid=20(staff)")
		}
		return exec.Command("echo", "")
	}

	got := getUserID()
	if got != "uid=501(user) gid=20(staff)" {
		t.Errorf("expected mock output, got %q", got)
	}
}

func TestGetUnameInfo(t *testing.T) {
	got := getUnameInfo()
	if got == "" {
		t.Error("expected uname info to be non-empty")
	}
}

func TestGetUnameInfo_Error(t *testing.T) {
	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "uname" {
			return exec.Command("false")
		}
		return exec.Command("echo", "")
	}

	got := getUnameInfo()
	if got != "unknown" {
		t.Errorf("expected unknown, got %q", got)
	}
}

func TestGetScrollback_ProxyLog(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("STY", "")

	logPath := filepath.Join(tempDir, "smart-suggestion", "proxy.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	os.WriteFile(logPath, []byte("scrollback content"), 0644)

	got, err := getScrollback(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "scrollback content" {
		t.Errorf("expected %q, got %q", "scrollback content", got)
	}
}

func TestDoGetScrollback_Tmux(t *testing.T) {
	t.Setenv("TMUX", "1")
	t.Setenv("KITTY_LISTEN_ON", "")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "tmux" {
			return exec.Command("echo", "tmux scrollback")
		}
		return exec.Command("echo", "")
	}

	got, err := getScrollback(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tmux scrollback" {
		t.Errorf("expected tmux scrollback, got %q", got)
	}
}

func TestDoGetScrollback_Kitty(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "1")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "kitten" {
			return exec.Command("echo", "kitty scrollback")
		}
		return exec.Command("echo", "")
	}

	got, err := getScrollback(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "kitty scrollback" {
		t.Errorf("expected kitty scrollback, got %q", got)
	}
}

func TestDoGetScrollback_Screen(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("SMART_SUGGESTION_SESSION_ID", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir()) // Ensure proxy log check fails

	t.Setenv("STY", "screen-session")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "screen" {
			// Screen writes to file. The last arg is the file path.
			argFile := arg[len(arg)-1]
			os.MkdirAll(filepath.Dir(argFile), 0755)
			os.WriteFile(argFile, []byte("screen scrollback"), 0644)
			return exec.Command("true")
		}
		return exec.Command("echo", "")
	}

	got, err := getScrollback(100, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "screen scrollback" {
		t.Errorf("expected screen scrollback, got %q", got)
	}
}

func TestDoGetScrollback_Fallback(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("SMART_SUGGESTION_SESSION_ID", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("STY", "")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "tput" {
			return exec.Command("echo", "24")
		}
		return exec.Command("echo", "")
	}

	_, err := getScrollback(100, "")
	if err == nil {
		t.Error("expected error for fallback, got nil")
	} else if !strings.Contains(err.Error(), "no scrollback available") {
		t.Errorf("expected no scrollback available error, got %v", err)
	}
}

func TestDoGetScrollback_ScrollbackFile(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")

	tempDir := t.TempDir()
	scrollbackFile := filepath.Join(tempDir, "screen.txt")
	os.WriteFile(scrollbackFile, []byte("ghostty scrollback content"), 0644)

	got, err := getScrollback(100, scrollbackFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ghostty scrollback content" {
		t.Errorf("expected ghostty scrollback content, got %q", got)
	}
}

func TestDoGetScrollback_ScrollbackFilePriority(t *testing.T) {
	// Scrollback file should take priority over tmux
	t.Setenv("TMUX", "1")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "tmux" {
			return exec.Command("echo", "tmux scrollback")
		}
		return exec.Command("echo", "")
	}

	tempDir := t.TempDir()
	scrollbackFile := filepath.Join(tempDir, "screen.txt")
	os.WriteFile(scrollbackFile, []byte("ghostty scrollback"), 0644)

	got, err := getScrollback(100, scrollbackFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ghostty scrollback" {
		t.Errorf("expected ghostty scrollback (priority over tmux), got %q", got)
	}
}

func TestIsTermux_WithTermuxVersion(t *testing.T) {
	oldTermuxVersion := os.Getenv("TERMUX_VERSION")
	oldPrefix := os.Getenv("PREFIX")
	defer func() {
		os.Setenv("TERMUX_VERSION", oldTermuxVersion)
		os.Setenv("PREFIX", oldPrefix)
	}()

	os.Setenv("TERMUX_VERSION", "0.118.0")
	os.Setenv("PREFIX", "")

	if !isTermux() {
		t.Error("expected isTermux() to return true when TERMUX_VERSION is set")
	}
}

func TestIsTermux_WithPrefix(t *testing.T) {
	oldTermuxVersion := os.Getenv("TERMUX_VERSION")
	oldPrefix := os.Getenv("PREFIX")
	defer func() {
		os.Setenv("TERMUX_VERSION", oldTermuxVersion)
		os.Setenv("PREFIX", oldPrefix)
	}()

	os.Setenv("TERMUX_VERSION", "")
	os.Setenv("PREFIX", "/data/data/com.termux/files/usr")

	if !isTermux() {
		t.Error("expected isTermux() to return true when PREFIX contains com.termux")
	}
}

func TestIsTermux_NotTermux(t *testing.T) {
	oldTermuxVersion := os.Getenv("TERMUX_VERSION")
	oldPrefix := os.Getenv("PREFIX")
	defer func() {
		os.Setenv("TERMUX_VERSION", oldTermuxVersion)
		os.Setenv("PREFIX", oldPrefix)
	}()

	os.Setenv("TERMUX_VERSION", "")
	os.Setenv("PREFIX", "/usr/local")

	if isTermux() {
		t.Error("expected isTermux() to return false when not in Termux")
	}
}

func TestGetSystemInfo_Termux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Skipping Termux test on macOS")
	}

	oldTermuxVersion := os.Getenv("TERMUX_VERSION")
	oldPrefix := os.Getenv("PREFIX")
	defer func() {
		os.Setenv("TERMUX_VERSION", oldTermuxVersion)
		os.Setenv("PREFIX", oldPrefix)
	}()

	os.Setenv("TERMUX_VERSION", "0.118.0")
	os.Setenv("PREFIX", "/data/data/com.termux/files/usr")

	got := getSystemInfo()
	if !strings.Contains(got, "Android with Termux 0.118.0") {
		t.Errorf("expected system info to contain 'Android with Termux 0.118.0', got %q", got)
	}
}

func TestGetSystemInfo_TermuxWithoutVersion(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("Skipping Termux test on macOS")
	}

	oldTermuxVersion := os.Getenv("TERMUX_VERSION")
	oldPrefix := os.Getenv("PREFIX")
	defer func() {
		os.Setenv("TERMUX_VERSION", oldTermuxVersion)
		os.Setenv("PREFIX", oldPrefix)
	}()

	os.Setenv("TERMUX_VERSION", "")
	os.Setenv("PREFIX", "/data/data/com.termux/files/usr")

	got := getSystemInfo()
	if !strings.Contains(got, "Android with Termux") {
		t.Errorf("expected system info to contain 'Android with Termux', got %q", got)
	}
}

func TestGetAvailableCommands(t *testing.T) {
	// Test with environment variable (primary method)
	originalCommands := os.Getenv("SMART_SUGGESTION_COMMANDS")
	defer func() {
		if originalCommands != "" {
			os.Setenv("SMART_SUGGESTION_COMMANDS", originalCommands)
		} else {
			os.Unsetenv("SMART_SUGGESTION_COMMANDS")
		}
	}()

	// Test with space-separated commands
	testCommands := "ls cat grep git docker kubectl npm yarn go python node curl wget ssh scp rsync tar gzip unzip vim nano emacs make cmake gcc clang java javac mvn gradle"
	os.Setenv("SMART_SUGGESTION_COMMANDS", testCommands)

	commands, err := getAvailableCommands()
	if err != nil {
		t.Fatalf("getAvailableCommands() failed: %v", err)
	}

	if commands != testCommands {
		t.Errorf("getAvailableCommands() should return env var content, got: %s, expected: %s", commands, testCommands)
	}

	// Test when environment variable is not set
	os.Unsetenv("SMART_SUGGESTION_COMMANDS")

	commands, err = getAvailableCommands()
	if err != nil {
		t.Errorf("getAvailableCommands() should not return error when no env var is set, got: %v", err)
	}

	if commands != "" {
		t.Errorf("getAvailableCommands() should return empty string when no env var is set, got: %s", commands)
	}
}

func TestBuildContextInfoWithCommands(t *testing.T) {
	// Set up environment variable for commands
	originalCommands := os.Getenv("SMART_SUGGESTION_COMMANDS")
	defer func() {
		if originalCommands != "" {
			os.Setenv("SMART_SUGGESTION_COMMANDS", originalCommands)
		} else {
			os.Unsetenv("SMART_SUGGESTION_COMMANDS")
		}
	}()

	testCommands := "ls cat grep git docker kubectl npm yarn go python"
	os.Setenv("SMART_SUGGESTION_COMMANDS", testCommands)

	// Test that BuildContextInfo includes available commands
	context, err := BuildContextInfo(10, "")
	if err != nil {
		t.Fatalf("BuildContextInfo() failed: %v", err)
	}

	if !strings.Contains(context, "# Available PATH commands:") {
		t.Errorf("BuildContextInfo() should include available PATH commands section")
	}

	if !strings.Contains(context, "ls cat grep git docker") {
		t.Errorf("BuildContextInfo() should include the commands from environment variable")
	}
}

func TestBuildContextInfoWithoutCommands(t *testing.T) {
	// Test without environment variable
	originalCommands := os.Getenv("SMART_SUGGESTION_COMMANDS")
	defer func() {
		if originalCommands != "" {
			os.Setenv("SMART_SUGGESTION_COMMANDS", originalCommands)
		} else {
			os.Unsetenv("SMART_SUGGESTION_COMMANDS")
		}
	}()

	os.Unsetenv("SMART_SUGGESTION_COMMANDS")

	// Test that BuildContextInfo works without available commands
	context, err := BuildContextInfo(10, "")
	if err != nil {
		t.Fatalf("BuildContextInfo() failed: %v", err)
	}

	// Should not include available commands section when env var is not set
	if strings.Contains(context, "# Available PATH commands:") {
		t.Errorf("BuildContextInfo() should not include available PATH commands section when env var is not set")
	}
}

func TestGetAvailableCommandsWithLargeList(t *testing.T) {
	// Test with a very large list of commands to ensure no truncation
	originalCommands := os.Getenv("SMART_SUGGESTION_COMMANDS")
	defer func() {
		if originalCommands != "" {
			os.Setenv("SMART_SUGGESTION_COMMANDS", originalCommands)
		} else {
			os.Unsetenv("SMART_SUGGESTION_COMMANDS")
		}
	}()

	// Create a large list of commands (100+ commands) with space separation
	var commandList []string
	for i := 0; i < 100; i++ {
		commandList = append(commandList, "cmd"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	testCommands := strings.Join(commandList, " ")

	os.Setenv("SMART_SUGGESTION_COMMANDS", testCommands)

	commands, err := getAvailableCommands()
	if err != nil {
		t.Fatalf("getAvailableCommands() failed with large list: %v", err)
	}

	if commands != testCommands {
		t.Errorf("getAvailableCommands() should return all commands without truncation")
	}

	// Verify all 100 commands are present
	returnedCommands := strings.Split(commands, " ")
	if len(returnedCommands) != 100 {
		t.Errorf("Expected 100 commands, got %d", len(returnedCommands))
	}
}
