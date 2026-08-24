package shellcontext

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReadLatestLines(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got, err := readLatestLines("", 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})

	t.Run("all-lines", func(t *testing.T) {
		input := "a\nb\n"
		got, err := readLatestLines(input, 0)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "a\nb" {
			t.Fatalf("expected trimmed input, got %q", got)
		}
	})

	t.Run("tail", func(t *testing.T) {
		input := "one\ntwo\nthree\n"
		got, err := readLatestLines(input, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "two\nthree" {
			t.Fatalf("expected tail, got %q", got)
		}
	})
}

func TestBuildContextSections(t *testing.T) {
	setEnv := func(key, value string) func() {
		old := os.Getenv(key)
		_ = os.Setenv(key, value)
		return func() { _ = os.Setenv(key, old) }
	}

	cleanupAliases := setEnv("SMART_SUGGESTION_ALIASES", "alias ll='ls -l'")
	cleanupCommands := setEnv("SMART_SUGGESTION_COMMANDS", "ls\ncat")
	cleanupHistory := setEnv("SMART_SUGGESTION_HISTORY", "ls\ncat")
	cleanupTerm := setEnv("TERM", "xterm")
	cleanupShell := setEnv("SHELL", "/bin/zsh")
	t.Cleanup(func() {
		cleanupAliases()
		cleanupCommands()
		cleanupHistory()
		cleanupTerm()
		cleanupShell()
	})

	systemContext, err := BuildSystemContext()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(systemContext, "# This is the alias defined in your shell:") {
		t.Fatal("expected alias section in system context")
	}
	if !strings.Contains(systemContext, "# Available PATH commands:") {
		t.Fatal("expected commands section in system context")
	}

	userContext, err := BuildUserContext(0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(userContext, "# Shell history:") {
		t.Fatal("expected history section in user context")
	}
}

func TestGetSystemInfoDarwin(t *testing.T) {
	oldGOOS := runtimeGOOS
	oldExec := execCommand
	t.Cleanup(func() {
		runtimeGOOS = oldGOOS
		execCommand = oldExec
	})

	runtimeGOOS = "darwin"
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "ProductName:\tmacOS\nProductVersion:\t14.0")
	}

	info := getSystemInfo()
	if !strings.Contains(info, "macOS") {
		t.Fatalf("expected macOS info, got %q", info)
	}
}

func TestGetSystemInfoLinux(t *testing.T) {
	oldGOOS := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = oldGOOS })

	runtimeGOOS = "linux"

	info := getSystemInfo()
	if !strings.Contains(info, "Linux") && !strings.Contains(info, "system is") {
		t.Fatalf("expected Linux info, got %q", info)
	}
}

func TestGetSystemInfoTermux(t *testing.T) {
	oldGOOS := runtimeGOOS
	oldTermux := os.Getenv("TERMUX_VERSION")
	t.Cleanup(func() {
		runtimeGOOS = oldGOOS
		os.Setenv("TERMUX_VERSION", oldTermux)
	})

	runtimeGOOS = "linux"
	os.Setenv("TERMUX_VERSION", "0.118")

	info := getSystemInfo()
	if !strings.Contains(info, "Termux") {
		t.Fatalf("expected Termux info, got %q", info)
	}
}

func TestIsTermux(t *testing.T) {
	oldTermux := os.Getenv("TERMUX_VERSION")
	oldPrefix := os.Getenv("PREFIX")
	t.Cleanup(func() {
		os.Setenv("TERMUX_VERSION", oldTermux)
		os.Setenv("PREFIX", oldPrefix)
	})

	os.Setenv("TERMUX_VERSION", "")
	os.Setenv("PREFIX", "")
	if isTermux() {
		t.Fatal("expected false without env vars")
	}

	os.Setenv("TERMUX_VERSION", "0.118")
	if !isTermux() {
		t.Fatal("expected true with TERMUX_VERSION")
	}

	os.Setenv("TERMUX_VERSION", "")
	os.Setenv("PREFIX", "/data/data/com.termux/files/usr")
	if !isTermux() {
		t.Fatal("expected true with PREFIX containing com.termux")
	}
}

func TestGetUserID(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "uid=1000(user)")
	}

	id := getUserID()
	if !strings.Contains(id, "uid=1000") {
		t.Fatalf("expected uid output, got %q", id)
	}
}

func TestGetUserIDError(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

	id := getUserID()
	if id != "unknown" {
		t.Fatalf("expected unknown, got %q", id)
	}
}

func TestGetUnameInfo(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "Darwin host 24.0.0")
	}

	info := getUnameInfo()
	if !strings.Contains(info, "Darwin") {
		t.Fatalf("expected uname output, got %q", info)
	}
}

func TestGetUnameInfoError(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

	info := getUnameInfo()
	if info != "unknown" {
		t.Fatalf("expected unknown, got %q", info)
	}
}

func TestReadLatestProxyContent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "proxy.log")
	if err := os.WriteFile(file, []byte("line1\nline2\nline3\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	content, err := readLatestProxyContent(file, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "line2\nline3" {
		t.Fatalf("expected tail lines, got %q", content)
	}

	content, err = readLatestProxyContent(file, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "line1\nline2\nline3" {
		t.Fatalf("expected all lines, got %q", content)
	}
}

func TestReadLatestProxyContentMissing(t *testing.T) {
	_, err := readLatestProxyContent("/nonexistent/file.log", 10)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestGetScrollbackWithFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "scrollback.txt")
	if err := os.WriteFile(file, []byte("scroll1\nscroll2\nscroll3\n"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	content, err := getScrollback(2, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "scroll2\nscroll3" {
		t.Fatalf("expected tail, got %q", content)
	}
}

func TestGetTerminalScrollbackWithTput(t *testing.T) {
	oldExec := execCommand
	t.Cleanup(func() { execCommand = oldExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("echo", "24")
	}

	_, err := getTerminalScrollbackWithTput()
	if err == nil {
		t.Fatal("expected error from tput fallback")
	}
	if !strings.Contains(err.Error(), "not fully implemented") {
		t.Fatalf("expected 'not fully implemented' error, got %v", err)
	}
}

func TestGetScreenScrollbackNotInSession(t *testing.T) {
	oldSTY := os.Getenv("STY")
	t.Cleanup(func() { os.Setenv("STY", oldSTY) })

	os.Setenv("STY", "")
	_, err := getScreenScrollback()
	if err == nil {
		t.Fatal("expected error when not in screen session")
	}
}

func TestAppendContextSectionError(t *testing.T) {
	var builder strings.Builder
	appendContextSection(&builder, "Test", func() (string, error) {
		return "", os.ErrNotExist
	})
	if builder.Len() != 0 {
		t.Fatal("expected empty builder on error")
	}
}

func TestAppendContextSectionEmpty(t *testing.T) {
	var builder strings.Builder
	appendContextSection(&builder, "Test", func() (string, error) {
		return "", nil
	})
	if builder.Len() != 0 {
		t.Fatal("expected empty builder for empty value")
	}
}

func TestBuildContextHeader(t *testing.T) {
	oldUser := os.Getenv("USER")
	oldShell := os.Getenv("SHELL")
	oldTerm := os.Getenv("TERM")
	t.Cleanup(func() {
		os.Setenv("USER", oldUser)
		os.Setenv("SHELL", oldShell)
		os.Setenv("TERM", oldTerm)
	})

	os.Setenv("USER", "")
	os.Setenv("SHELL", "")
	os.Setenv("TERM", "")

	header := buildContextHeader()
	if !strings.Contains(header, "unknown") {
		t.Fatal("expected unknown placeholders in header")
	}
}

func TestGetAliasesEmpty(t *testing.T) {
	oldAliases := os.Getenv("SMART_SUGGESTION_ALIASES")
	t.Cleanup(func() { os.Setenv("SMART_SUGGESTION_ALIASES", oldAliases) })

	os.Setenv("SMART_SUGGESTION_ALIASES", "")
	aliases, err := getAliases()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if aliases != "" {
		t.Fatalf("expected empty, got %q", aliases)
	}
}

func TestGetAvailableCommandsEmpty(t *testing.T) {
	oldCmds := os.Getenv("SMART_SUGGESTION_COMMANDS")
	t.Cleanup(func() { os.Setenv("SMART_SUGGESTION_COMMANDS", oldCmds) })

	os.Setenv("SMART_SUGGESTION_COMMANDS", "")
	cmds, err := getAvailableCommands()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmds != "" {
		t.Fatalf("expected empty, got %q", cmds)
	}
}

func TestGetHistoryEmpty(t *testing.T) {
	oldHistory := os.Getenv("SMART_SUGGESTION_HISTORY")
	t.Cleanup(func() { os.Setenv("SMART_SUGGESTION_HISTORY", oldHistory) })

	os.Setenv("SMART_SUGGESTION_HISTORY", "")
	history, err := getHistory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if history != "" {
		t.Fatalf("expected empty, got %q", history)
	}
}

func TestDoGetScrollbackTmux(t *testing.T) {
	oldTmux := os.Getenv("TMUX")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("TMUX", oldTmux)
		execCommand = oldExec
	})

	os.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "tmux" {
			return exec.Command("echo", "tmux scrollback")
		}
		return exec.Command("false")
	}

	content, err := doGetScrollback(10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "tmux scrollback") {
		t.Fatalf("expected tmux content, got %q", content)
	}
}

func TestDoGetScrollbackKitty(t *testing.T) {
	oldTmux := os.Getenv("TMUX")
	oldKitty := os.Getenv("KITTY_LISTEN_ON")
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("TMUX", oldTmux)
		os.Setenv("KITTY_LISTEN_ON", oldKitty)
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		execCommand = oldExec
	})

	os.Setenv("TMUX", "")
	os.Setenv("KITTY_LISTEN_ON", "unix:/tmp/kitty")
	os.Setenv("HERDR_ENV", "")
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "kitten" {
			return exec.Command("echo", "kitty scrollback")
		}
		return exec.Command("false")
	}

	content, err := doGetScrollback(10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "kitty scrollback") {
		t.Fatalf("expected kitty content, got %q", content)
	}
}

func TestDoGetScrollbackHerdr(t *testing.T) {
	oldTmux := os.Getenv("TMUX")
	oldKitty := os.Getenv("KITTY_LISTEN_ON")
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	oldHerdrBin := os.Getenv("HERDR_BIN_PATH")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("TMUX", oldTmux)
		os.Setenv("KITTY_LISTEN_ON", oldKitty)
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
		os.Setenv("HERDR_BIN_PATH", oldHerdrBin)
		execCommand = oldExec
	})

	os.Setenv("TMUX", "")
	os.Setenv("KITTY_LISTEN_ON", "")
	os.Setenv("HERDR_ENV", "1")
	os.Setenv("HERDR_PANE_ID", "w1:p1")
	os.Setenv("HERDR_BIN_PATH", "")

	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "herdr" {
			gotArgs = append([]string{name}, args...)
			return exec.Command("echo", "herdr scrollback")
		}
		return exec.Command("false")
	}

	content, err := doGetScrollback(10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "herdr scrollback") {
		t.Fatalf("expected herdr content, got %q", content)
	}

	wantArgs := []string{"herdr", "pane", "read", "w1:p1", "--source", "recent-unwrapped", "--lines", "10"}
	if strings.Join(gotArgs, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("expected herdr args %q, got %q", wantArgs, gotArgs)
	}
}

func TestDoGetScrollbackHerdrBeforeKitty(t *testing.T) {
	oldTmux := os.Getenv("TMUX")
	oldKitty := os.Getenv("KITTY_LISTEN_ON")
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	oldHerdrBin := os.Getenv("HERDR_BIN_PATH")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("TMUX", oldTmux)
		os.Setenv("KITTY_LISTEN_ON", oldKitty)
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
		os.Setenv("HERDR_BIN_PATH", oldHerdrBin)
		execCommand = oldExec
	})

	os.Setenv("TMUX", "")
	os.Setenv("KITTY_LISTEN_ON", "unix:/tmp/kitty")
	os.Setenv("HERDR_ENV", "1")
	os.Setenv("HERDR_PANE_ID", "w1:p1")
	os.Setenv("HERDR_BIN_PATH", "")
	execCommand = func(name string, args ...string) *exec.Cmd {
		switch name {
		case "herdr":
			return exec.Command("echo", "herdr scrollback")
		case "kitten":
			return exec.Command("echo", "kitty scrollback")
		default:
			return exec.Command("false")
		}
	}

	content, err := doGetScrollback(10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "herdr scrollback") {
		t.Fatalf("expected herdr to take priority over kitty, got %q", content)
	}
	if strings.Contains(content, "kitty scrollback") {
		t.Fatalf("did not expect kitty content when herdr is available, got %q", content)
	}
}

func TestGetHerdrScrollbackNotInSession(t *testing.T) {
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	t.Cleanup(func() {
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
	})

	os.Setenv("HERDR_ENV", "")
	os.Setenv("HERDR_PANE_ID", "w1:p1")
	if _, err := getHerdrScrollback(10); err == nil {
		t.Fatal("expected error when HERDR_ENV is not set")
	}

	os.Setenv("HERDR_ENV", "true")
	if _, err := getHerdrScrollback(10); err == nil {
		t.Fatal("expected error when HERDR_ENV is not exactly 1")
	}

	os.Setenv("HERDR_ENV", "1")
	os.Setenv("HERDR_PANE_ID", "")
	if _, err := getHerdrScrollback(10); err == nil {
		t.Fatal("expected error when HERDR_PANE_ID is empty")
	}
}

func TestGetHerdrScrollbackCommandError(t *testing.T) {
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	oldHerdrBin := os.Getenv("HERDR_BIN_PATH")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
		os.Setenv("HERDR_BIN_PATH", oldHerdrBin)
		execCommand = oldExec
	})

	os.Setenv("HERDR_ENV", "1")
	os.Setenv("HERDR_PANE_ID", "w1:p1")
	os.Setenv("HERDR_BIN_PATH", "")
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

	if _, err := getHerdrScrollback(10); err == nil {
		t.Fatal("expected error when herdr command fails")
	}
}

func TestGetHerdrScrollbackUsesBinPath(t *testing.T) {
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	oldHerdrBin := os.Getenv("HERDR_BIN_PATH")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
		os.Setenv("HERDR_BIN_PATH", oldHerdrBin)
		execCommand = oldExec
	})

	os.Setenv("HERDR_ENV", "1")
	os.Setenv("HERDR_PANE_ID", "w1:p1")
	os.Setenv("HERDR_BIN_PATH", "/custom/bin/herdr")

	var gotName string
	execCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		return exec.Command("echo", "herdr scrollback")
	}

	content, err := getHerdrScrollback(10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "herdr scrollback" {
		t.Fatalf("expected herdr content, got %q", content)
	}
	if gotName != "/custom/bin/herdr" {
		t.Fatalf("expected HERDR_BIN_PATH binary, got %q", gotName)
	}
}

func TestGetHerdrScrollbackUnlimitedLines(t *testing.T) {
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	oldHerdrBin := os.Getenv("HERDR_BIN_PATH")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
		os.Setenv("HERDR_BIN_PATH", oldHerdrBin)
		execCommand = oldExec
	})

	os.Setenv("HERDR_ENV", "1")
	os.Setenv("HERDR_PANE_ID", "w1:p1")
	os.Setenv("HERDR_BIN_PATH", "")

	var gotArgs []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		if name == "herdr" {
			gotArgs = append([]string{name}, args...)
			return exec.Command("echo", "herdr scrollback")
		}
		return exec.Command("false")
	}

	content, err := getHerdrScrollback(0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != "herdr scrollback" {
		t.Fatalf("expected herdr content, got %q", content)
	}

	wantArgs := []string{"herdr", "pane", "read", "w1:p1", "--source", "recent-unwrapped", "--lines", strconv.Itoa(herdrRecentLinesUnlimited)}
	if strings.Join(gotArgs, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("expected herdr args %q, got %q", wantArgs, gotArgs)
	}
}

func TestGetScrollbackError(t *testing.T) {
	oldTmux := os.Getenv("TMUX")
	oldKitty := os.Getenv("KITTY_LISTEN_ON")
	oldSTY := os.Getenv("STY")
	oldHerdrEnv := os.Getenv("HERDR_ENV")
	oldHerdrPane := os.Getenv("HERDR_PANE_ID")
	oldSessionID := os.Getenv("SMART_SUGGESTION_SESSION_ID")
	oldExec := execCommand
	t.Cleanup(func() {
		os.Setenv("TMUX", oldTmux)
		os.Setenv("KITTY_LISTEN_ON", oldKitty)
		os.Setenv("STY", oldSTY)
		os.Setenv("HERDR_ENV", oldHerdrEnv)
		os.Setenv("HERDR_PANE_ID", oldHerdrPane)
		os.Setenv("SMART_SUGGESTION_SESSION_ID", oldSessionID)
		execCommand = oldExec
	})

	os.Setenv("TMUX", "")
	os.Setenv("KITTY_LISTEN_ON", "")
	os.Setenv("STY", "")
	os.Setenv("HERDR_ENV", "")
	os.Setenv("HERDR_PANE_ID", "")
	os.Setenv("SMART_SUGGESTION_SESSION_ID", "")
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("false")
	}

	_, err := getScrollback(10, "")
	if err == nil {
		t.Fatal("expected error when no scrollback source available")
	}
}

func TestBuildUserContextNegativeLines(t *testing.T) {
	infoNegative, err := BuildUserContext(-10, "")
	if err != nil {
		t.Fatalf("unexpected error with negative lines: %v", err)
	}
	infoZero, err := BuildUserContext(0, "")
	if err != nil {
		t.Fatalf("unexpected error with zero lines: %v", err)
	}
	if infoNegative != infoZero {
		t.Fatalf("expected same output for negative and zero lines, got (negative) %q and (zero) %q", infoNegative, infoZero)
	}
}
