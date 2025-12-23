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

func TestGetSystemInfo_Error(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping macOS specific test")
	}

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "sw_vers" {
			return exec.Command("false")
		}
		return exec.Command("echo", "")
	}

	got := getSystemInfo()
	if got != "Your system is macOS." {
		t.Errorf("expected default macOS msg, got %q", got)
	}
}

func TestDoGetShellBuffer_SessionLogFail(t *testing.T) {
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

	got, err := getShellBuffer()
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

	got, err := readLatestProxyContent(logFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// readLatestProxyContent has a const maxLines = 50
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 50 {
		t.Errorf("expected 50 lines, got %d", len(gotLines))
	}
	if gotLines[0] != "line11" {
		t.Errorf("expected first line to be line11, got %q", gotLines[0])
	}
	if gotLines[49] != "line60" {
		t.Errorf("expected last line to be line60, got %q", gotLines[49])
	}
}

func TestBuildContextInfo(t *testing.T) {
	t.Setenv("USER", "testuser")
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("SMART_SUGGESTION_ALIASES", "alias ls='ls -G'")
	t.Setenv("SMART_SUGGESTION_HISTORY", "ls\ncd /tmp")

	got, err := BuildContextInfo()
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

func TestBuildContextInfo_NoBuffer(t *testing.T) {
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

	got, err := BuildContextInfo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "# Shell buffer:") {
		t.Error("expected no shell buffer in context")
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

func TestGetShellBuffer_ProxyLog(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "")
	t.Setenv("STY", "")

	logPath := filepath.Join(tempDir, "smart-suggestion", "proxy.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	os.WriteFile(logPath, []byte("shell buffer content"), 0644)

	got, err := getShellBuffer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "shell buffer content" {
		t.Errorf("expected %q, got %q", "shell buffer content", got)
	}
}

func TestDoGetShellBuffer_Tmux(t *testing.T) {
	t.Setenv("TMUX", "1")
	t.Setenv("KITTY_LISTEN_ON", "")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "tmux" {
			return exec.Command("echo", "tmux buffer")
		}
		return exec.Command("echo", "")
	}

	got, err := getShellBuffer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "tmux buffer" {
		t.Errorf("expected tmux buffer, got %q", got)
	}
}

func TestDoGetShellBuffer_Kitty(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("KITTY_LISTEN_ON", "1")

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		if name == "kitten" {
			return exec.Command("echo", "kitty buffer")
		}
		return exec.Command("echo", "")
	}

	got, err := getShellBuffer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "kitty buffer" {
		t.Errorf("expected kitty buffer, got %q", got)
	}
}

func TestDoGetShellBuffer_Screen(t *testing.T) {
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
			os.WriteFile(argFile, []byte("screen buffer"), 0644)
			return exec.Command("true")
		}
		return exec.Command("echo", "")
	}

	got, err := getShellBuffer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "screen buffer" {
		t.Errorf("expected screen buffer, got %q", got)
	}
}

func TestDoGetShellBuffer_Fallback(t *testing.T) {
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

	_, err := getShellBuffer()
	if err == nil {
		t.Error("expected error for fallback, got nil")
	} else if !strings.Contains(err.Error(), "no terminal buffer available") {
		t.Errorf("expected no terminal buffer error, got %v", err)
	}
}
