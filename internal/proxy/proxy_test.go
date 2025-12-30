//go:build unix

package proxy

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xyenon/smart-suggestion/internal/session"
)

func TestIsProcessRunning(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "process.lock")

	// Current process PID
	pid := os.Getpid()
	os.WriteFile(lockPath, []byte(strconv.Itoa(pid)), 0644)

	if !isProcessRunning(lockPath) {
		t.Error("expected process to be running")
	}

	// Invalid PID
	os.WriteFile(lockPath, []byte("9999999"), 0644)
	if isProcessRunning(lockPath) {
		t.Error("expected process to not be running (invalid PID)")
	}
}

func TestIsProcessRunning_Malformed(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "malformed.lock")

	os.WriteFile(lockPath, []byte("not-a-pid"), 0644)
	if isProcessRunning(lockPath) {
		t.Error("expected process to not be running (malformed PID)")
	}
}

func TestCleanupProcessLock(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "test.lock")

	f, _ := os.Create(lockPath)
	cleanupProcessLock(f, lockPath)

	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("expected lock file to be deleted")
	}
}

func TestCreateProcessLock_StaleLock(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "stale.lock")

	// Create a stale lock file with an invalid PID
	os.WriteFile(lockPath, []byte("9999999"), 0644)

	f, err := createProcessLock(lockPath)
	if err != nil {
		t.Fatalf("expected to be able to acquire stale lock, got error: %v", err)
	}
	if f == nil {
		t.Fatal("expected file handle, got nil")
	}
	cleanupProcessLock(f, lockPath)
}

func TestRunProxy_Error(t *testing.T) {
	err := RunProxy("/non/existent/shell", ProxyOptions{
		LogFile:   filepath.Join(t.TempDir(), "proxy.log"),
		SessionID: "test",
	})
	if err == nil {
		t.Error("expected error for non-existent shell, got nil")
	}
}

func TestCreateProcessLock_InvalidDir(t *testing.T) {
	// Lock path in a location we can't create
	lockPath := "/non/existent/dir/test.lock"
	_, err := createProcessLock(lockPath)
	if err == nil {
		t.Error("expected error for invalid directory, got nil")
	}
}

func TestGetSessionBasedLockFile(t *testing.T) {
	cases := []struct {
		name      string
		base      string
		sessionID string
		expected  string
	}{
		{
			name:      "simple",
			base:      filepath.Join("tmp", "proxy.lock"),
			sessionID: "123",
			expected:  filepath.Join("tmp", "proxy.123.lock"),
		},
	}
	// Let's re-read the logic:
	// baseLockFile := strings.TrimSuffix(opts.LogFile, filepath.Ext(opts.LogFile)) + ".lock"
	// sessionLockFile := getSessionBasedLockFile(baseLockFile, opts.SessionID)
	// getSessionBasedLockFile(base, sessionID):
	//   base := filepath.Base(baseLockFile) // e.g. proxy.lock
	//   ext := filepath.Ext(base) // e.g. .lock
	//   base = strings.TrimSuffix(base, ext) // e.g. proxy
	//   return filepath.Join(dir, fmt.Sprintf("%s.%s%s", base, sessionID, ext)) // e.g. proxy.123.lock

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := getSessionBasedLockFile(tc.base, tc.sessionID)
			if got != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestCreateProcessLock_InvalidPID(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "invalid_pid.lock")

	// Create a lock file with invalid PID content
	os.WriteFile(lockPath, []byte("abc\n"), 0644)

	f, err := createProcessLock(lockPath)
	if err != nil {
		t.Fatalf("expected to be able to acquire lock with invalid PID content, got error: %v", err)
	}
	cleanupProcessLock(f, lockPath)
}

func TestProcessLock(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "test.lock")

	// Create lock
	f, err := createProcessLock(lockPath)
	if err != nil {
		t.Fatalf("failed to create lock: %v", err)
	}
	if f == nil {
		t.Fatal("expected file handle, got nil")
	}

	// Try to create another lock (should fail)
	_, err = createProcessLock(lockPath)
	if err == nil {
		t.Error("expected error when creating duplicate lock, got nil")
	}

	// Cleanup
	cleanupProcessLock(f, lockPath)

	// Verify cleanup
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("expected lock file to be deleted, but it exists")
	}

	// Create again (should succeed)
	f, err = createProcessLock(lockPath)
	if err != nil {
		t.Fatalf("failed to recreate lock: %v", err)
	}
	cleanupProcessLock(f, lockPath)
}

func TestCreateProcessLock_AlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "running.lock")

	// Create a lock file with current process PID
	os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())), 0644)

	_, err := createProcessLock(lockPath)
	if err == nil || !strings.Contains(err.Error(), "another instance is already running") {
		t.Errorf("expected already running error, got %v", err)
	}
}

func TestRunProxy_LogFileError(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	err := RunProxyWithIO("true", ProxyOptions{
		LogFile:   "/non/existent/dir/proxy.log",
		SessionID: "test-err",
	}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Error("expected error for invalid log file path, got nil")
	}
}

func TestRunProxy_LockError(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	err := RunProxyWithIO("true", ProxyOptions{
		LogFile:   "/non/existent/dir/proxy.log", // This will cause lock to fail too as it's derived from LogFile
		SessionID: "test-lock-err",
	}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Error("expected error for invalid lock path, got nil")
	}
}

func TestCleanupOldSessionLogs(t *testing.T) {
	tempDir := t.TempDir()
	baseLog := filepath.Join(tempDir, "proxy.log")

	// Create some dummy logs
	oldLog := filepath.Join(tempDir, "proxy.old.log")
	newLog := filepath.Join(tempDir, "proxy.new.log")

	os.WriteFile(oldLog, []byte("old"), 0644)
	os.WriteFile(newLog, []byte("new"), 0644)

	// Set old log time to 2 days ago
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldLog, oldTime, oldTime)

	err := cleanupOldSessionLogs(baseLog, 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to cleanup logs: %v", err)
	}

	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Error("expected old log to be deleted")
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Error("expected new log to still exist")
	}
}

func TestCleanupOldSessionLogs_SkipDir(t *testing.T) {
	tempDir := t.TempDir()
	baseLog := filepath.Join(tempDir, "proxy.log")

	// Create a directory that matches the pattern
	dirMatch := filepath.Join(tempDir, "proxy.123.log")
	os.MkdirAll(dirMatch, 0755)

	err := cleanupOldSessionLogs(baseLog, 24*time.Hour)
	if err != nil {
		t.Fatalf("cleanupOldSessionLogs error: %v", err)
	}
	// Should not crash and should skip the directory
}

func TestCleanupOldSessionLogs_InvalidDir(t *testing.T) {
	err := cleanupOldSessionLogs("/non/existent/dir/proxy.log", 24*time.Hour)
	if err == nil {
		t.Error("expected error for non-existent directory, got nil")
	}
}

func TestRunProxy_Simple(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "proxy.log")

	// Use an empty reader for stdin to trigger immediate exit of the stdin copy goroutine
	stdin := strings.NewReader("")
	var stdout bytes.Buffer

	err := RunProxyWithIO("true", ProxyOptions{
		LogFile:   logFile,
		SessionID: "test-simple",
	}, stdin, &stdout)

	if err != nil {
		t.Fatalf("RunProxy error: %v", err)
	}

	// Verify log file was created
	sessionLog := session.GetSessionBasedLogFile(logFile, "test-simple")
	if _, err := os.Stat(sessionLog); err != nil {
		t.Errorf("expected session log file to exist: %v", err)
	}
}

func TestRunProxy_ExistingLog(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "proxy.log")
	sessionLog := session.GetSessionBasedLogFile(logFile, "test-exist")
	os.WriteFile(sessionLog, []byte("old content"), 0644)

	err := RunProxyWithIO("true", ProxyOptions{
		LogFile:   logFile,
		SessionID: "test-exist",
	}, strings.NewReader(""), io.Discard)

	if err != nil {
		t.Fatalf("RunProxy error: %v", err)
	}
}

func TestRunProxy_RemoveLogFail(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "proxy.log")
	sessionLog := session.GetSessionBasedLogFile(logFile, "test-rem-fail")

	// Create a non-empty directory to make os.Remove fail
	os.MkdirAll(filepath.Join(sessionLog, "subdir"), 0755)

	err := RunProxyWithIO("true", ProxyOptions{
		LogFile:   logFile,
		SessionID: "test-rem-fail",
	}, strings.NewReader(""), io.Discard)

	if err == nil || !strings.Contains(err.Error(), "failed to open session log file") {
		t.Errorf("expected failed to open log file error, got %v", err)
	}
}

func TestRunProxy_PTYError(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	err := RunProxyWithIO("/non/existent/shell", ProxyOptions{
		LogFile:   filepath.Join(t.TempDir(), "proxy.log"),
		SessionID: "test-pty-err",
	}, strings.NewReader(""), io.Discard)
	if err == nil {
		t.Error("expected error for non-existent shell, got nil")
	}
}

func TestLineLimitedWriter_Basic(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "test.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 3)

	// Write 5 lines
	for i := 1; i <= 5; i++ {
		_, err := w.Write([]byte("line" + strconv.Itoa(i) + "\n"))
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}
	}

	// Read file content
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line3" {
		t.Errorf("expected first line to be line3, got %s", lines[0])
	}
	if lines[2] != "line5" {
		t.Errorf("expected last line to be line5, got %s", lines[2])
	}
}

func TestLineLimitedWriter_PartialWrites(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "partial.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 2)

	// Write partial data (no newline yet)
	w.Write([]byte("hel"))
	w.Write([]byte("lo"))
	w.Write([]byte("\n"))
	w.Write([]byte("wor"))
	w.Write([]byte("ld\n"))

	content, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "hello" {
		t.Errorf("expected 'hello', got %s", lines[0])
	}
	if lines[1] != "world" {
		t.Errorf("expected 'world', got %s", lines[1])
	}
}

func TestLineLimitedWriter_NoNewline(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "nonewline.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 5)

	// Write data without newline - should be buffered
	w.Write([]byte("no newline yet"))

	content, _ := os.ReadFile(logPath)
	if len(content) != 0 {
		t.Errorf("expected empty file (data buffered), got %s", string(content))
	}

	// Now add the newline
	w.Write([]byte("\n"))
	content, _ = os.ReadFile(logPath)
	if string(content) != "no newline yet\n" {
		t.Errorf("expected 'no newline yet\\n', got %s", string(content))
	}
}

func TestLineLimitedWriter_ExactLimit(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "exact.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 3)

	// Write exactly 3 lines
	w.Write([]byte("a\nb\nc\n"))

	content, _ := os.ReadFile(logPath)
	expected := "a\nb\nc\n"
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}

	// Add one more line - oldest should be removed
	w.Write([]byte("d\n"))
	content, _ = os.ReadFile(logPath)
	expected = "b\nc\nd\n"
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}
}

func TestLineLimitedWriter_MultipleNewlinesInOneWrite(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "multi.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 2)

	// Write multiple lines at once
	w.Write([]byte("line1\nline2\nline3\nline4\n"))

	content, _ := os.ReadFile(logPath)
	expected := "line3\nline4\n"
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}
}

func TestLineLimitedWriter_EmptyWrite(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "empty.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 5)

	n, err := w.Write([]byte{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 bytes written, got %d", n)
	}
}

func TestLineLimitedWriter_SingleLine(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "single.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 1)

	w.Write([]byte("first\n"))
	w.Write([]byte("second\n"))
	w.Write([]byte("third\n"))

	content, _ := os.ReadFile(logPath)
	expected := "third\n"
	if string(content) != expected {
		t.Errorf("expected %q, got %q", expected, string(content))
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no escape sequences",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "simple color",
			input:    "\x1b[31mred text\x1b[0m",
			expected: "red text",
		},
		{
			name:     "bold and color",
			input:    "\x1b[1;32mbold green\x1b[0m",
			expected: "bold green",
		},
		{
			name:     "cursor movement",
			input:    "\x1b[2Jclear screen\x1b[H",
			expected: "clear screen",
		},
		{
			name:     "OSC sequence (window title)",
			input:    "\x1b]0;Window Title\x07content",
			expected: "content",
		},
		{
			name:     "OSC 7 file URL",
			input:    "\x1b]7;file://hostname/path\x07content",
			expected: "content",
		},
		{
			name:     "leftover OSC content at line start",
			input:    "7;file://M20RQRV6G4/Users/bytedance\nactual content",
			expected: "\nactual content",
		},
		{
			name:     "mixed content",
			input:    "start \x1b[31mred\x1b[0m middle \x1b[1mbold\x1b[0m end",
			expected: "start red middle bold end",
		},
		{
			name:     "256 color",
			input:    "\x1b[38;5;196mred\x1b[0m",
			expected: "red",
		},
		{
			name:     "RGB color",
			input:    "\x1b[38;2;255;0;0mred\x1b[0m",
			expected: "red",
		},
		{
			name:     "cursor save/restore",
			input:    "\x1b7saved\x1b8restored",
			expected: "savedrestored",
		},
		{
			name:     "erase line",
			input:    "text\x1b[Kerased",
			expected: "texterased",
		},
		{
			name:     "backspace simulates deletion",
			input:    "abc\x08\x08xy",
			expected: "axy",
		},
		{
			name:     "backspace at line start",
			input:    "line1\n\x08\x08line2",
			expected: "line1\nline2",
		},
		{
			name:     "carriage return overwrites line",
			input:    "old text\rnew",
			expected: "new",
		},
		{
			name:     "carriage return with newline",
			input:    "line1\r\nline2",
			expected: "line1\nline2",
		},
		{
			name:     "bell character removed",
			input:    "alert\x07text",
			expected: "alerttext",
		},
		{
			name:     "progress bar simulation",
			input:    "Loading... 10%\rLoading... 50%\rLoading... 100%",
			expected: "Loading... 100%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.expected {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSimulateTerminal(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple text",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "backspace deletes char",
			input:    "ab\x08c",
			expected: "ac",
		},
		{
			name:     "multiple backspaces",
			input:    "abcd\x08\x08\x08xyz",
			expected: "axyz",
		},
		{
			name:     "backspace at start does nothing",
			input:    "\x08\x08abc",
			expected: "abc",
		},
		{
			name:     "backspace stops at newline",
			input:    "line1\n\x08\x08abc",
			expected: "line1\nabc",
		},
		{
			name:     "carriage return resets line",
			input:    "hello\rworld",
			expected: "world",
		},
		{
			name:     "CR preserves previous lines",
			input:    "line1\nold\rnew",
			expected: "line1\nnew",
		},
		{
			name:     "CRLF becomes LF",
			input:    "a\r\nb",
			expected: "a\nb",
		},
		{
			name:     "vertical tab becomes newline",
			input:    "a\x0bb",
			expected: "a\nb",
		},
		{
			name:     "form feed becomes newline",
			input:    "a\x0cb",
			expected: "a\nb",
		},
		{
			name:     "spinner simulation",
			input:    "|\r/\r-\r\\\r|",
			expected: "|",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := simulateTerminal(tt.input)
			if got != tt.expected {
				t.Errorf("simulateTerminal(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLineLimitedWriter_StripANSI(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "ansi.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to create log file: %v", err)
	}
	defer f.Close()

	w := newLineLimitedWriter(f, logPath, 5)

	// Write lines with ANSI escape sequences
	w.Write([]byte("\x1b[31merror: something failed\x1b[0m\n"))
	w.Write([]byte("\x1b[1;32mSuccess!\x1b[0m\n"))
	w.Write([]byte("normal line\n"))

	content, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "error: something failed" {
		t.Errorf("expected 'error: something failed', got %q", lines[0])
	}
	if lines[1] != "Success!" {
		t.Errorf("expected 'Success!', got %q", lines[1])
	}
	if lines[2] != "normal line" {
		t.Errorf("expected 'normal line', got %q", lines[2])
	}
}
