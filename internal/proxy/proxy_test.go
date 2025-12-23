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

	"github.com/yetone/smart-suggestion/internal/session"
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

func TestRunProxy_AlreadyActive(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", "123")
	err := RunProxy("bash", ProxyOptions{
		LogFile:   filepath.Join(t.TempDir(), "proxy.log"),
		SessionID: "test",
	})
	if err != nil {
		t.Errorf("expected nil error for already active proxy, got %v", err)
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
