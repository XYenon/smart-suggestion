//go:build unix

package proxy

import (
	"bytes"
	"errors"
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

	if _, err := os.Stat(lockPath); err != nil {
		t.Error("expected lock file to remain after unlock")
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

func TestSessionLockPathForLogExtensionless(t *testing.T) {
	base := filepath.Join("tmp", "proxy")
	sessionLog := session.GetSessionBasedLogFile(base, "pts_1")
	got := sessionLockPathForLog(base, sessionLog)
	want := filepath.Join("tmp", "proxy.pts_1.lock")
	if got != want {
		t.Fatalf("got %q, want %q (session log %q)", got, want, sessionLog)
	}
}

func TestCleanupOldSessionLogsSkipsLiveExtensionlessSession(t *testing.T) {
	tempDir := t.TempDir()
	baseLog := filepath.Join(tempDir, "proxy")
	sessionLog := session.GetSessionBasedLogFile(baseLog, "pts_1")
	if err := os.WriteFile(sessionLog, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(sessionLog, old, old); err != nil {
		t.Fatal(err)
	}

	lockPath := sessionLockPathForLog(baseLog, sessionLog)
	held, err := createProcessLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupProcessLock(held, lockPath)

	if err := cleanupOldSessionLogs(baseLog, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionLog); err != nil {
		t.Fatalf("live extensionless session log was removed: %v", err)
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

	cleanupProcessLock(f, lockPath)

	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file to remain after unlock: %v", err)
	}

	f, err = createProcessLock(lockPath)
	if err != nil {
		t.Fatalf("failed to recreate lock: %v", err)
	}
	cleanupProcessLock(f, lockPath)
}

func TestCreateProcessLock_AlreadyRunning(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "running.lock")

	held, err := createProcessLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupProcessLock(held, lockPath)

	_, err = createProcessLock(lockPath)
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

func TestRunProxyCapturesRenderedTerminalState(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "")
	tempDir := t.TempDir()
	shell := filepath.Join(tempDir, "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf '\\033[31mold\\033[0m\\rnew\\033[K\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	logFile := filepath.Join(tempDir, "proxy.log")
	var stdout bytes.Buffer

	if err := RunProxyWithIO(shell, ProxyOptions{
		LogFile:   logFile,
		SessionID: "rendered",
	}, strings.NewReader(""), &stdout); err != nil {
		t.Fatal(err)
	}

	if got := stdout.String(); !strings.Contains(got, "\x1b[31mold\x1b[0m\rnew\x1b[K") {
		t.Fatalf("stdout did not preserve raw terminal output: %q", got)
	}
	sessionLog := session.GetSessionBasedLogFile(logFile, "rendered")
	content, err := os.ReadFile(sessionLog)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(content), "new"; got != want {
		t.Fatalf("rendered log = %q, want %q", got, want)
	}
}

func TestTerminalSizeDefaultsForNonTerminal(t *testing.T) {
	if width, height := terminalSize(strings.NewReader("")); width != 80 || height != 24 {
		t.Fatalf("size = %dx%d, want 80x24", width, height)
	}
}

type failingWriter struct {
	n   int
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return w.n, w.err
}

func TestBestEffortLogWriterDoesNotPropagateFailure(t *testing.T) {
	payload := []byte("terminal output")
	tests := []struct {
		name   string
		writer io.Writer
	}{
		{name: "error", writer: failingWriter{err: errors.New("disk full")}},
		{name: "short write", writer: failingWriter{n: len(payload) - 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			writer := io.MultiWriter(&stdout, bestEffortLogWriter{writer: test.writer})
			n, err := writer.Write(payload)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != len(payload) {
				t.Fatalf("wrote %d bytes, want %d", n, len(payload))
			}
			if !bytes.Equal(stdout.Bytes(), payload) {
				t.Fatalf("stdout = %q, want %q", stdout.Bytes(), payload)
			}
		})
	}
}
