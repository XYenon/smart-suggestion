package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func resetLoggerForTest(t *testing.T) {
	t.Helper()
	Close()
	mu.Lock()
	enabled = false
	logFile = nil
	logger = nil
	initOnce = sync.Once{}
	initError = nil
	mu.Unlock()
	t.Cleanup(Close)
}

func TestLog(t *testing.T) {
	// Create a temp dir for cache
	tempDir := t.TempDir()
	t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", tempDir)

	resetLoggerForTest(t)

	// Enable logging
	Enable(true)
	if !Enabled() {
		t.Error("expected debug to be enabled")
	}

	// Write a log
	msg := "test message"
	data := map[string]any{"key": "value"}
	Log(msg, data)

	// Verify log file creation
	logPath := filepath.Join(tempDir, "smart-suggestion", "debug.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	// Verify content
	var entry map[string]any
	if err := json.Unmarshal(content, &entry); err != nil {
		t.Fatalf("failed to parse log entry: %v", err)
	}

	if entry["log"] != msg {
		t.Errorf("expected log message %q, got %q", msg, entry["log"])
	}
	if entry["key"] != "value" {
		t.Errorf("expected data key 'value', got %v", entry["key"])
	}
}

func TestClose(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	resetLoggerForTest(t)
	Enable(true)
	Log("message", nil)
	Close()

	mu.RLock()
	defer mu.RUnlock()
	if logFile != nil {
		t.Error("expected logFile to be nil after Close")
	}
}

func TestEnableFalse(t *testing.T) {
	resetLoggerForTest(t)
	Enable(false)
	if Enabled() {
		t.Error("expected debug to be disabled")
	}
	Log("should not log", nil)
}

func TestInitError(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", tempDir)

	// Create a file where the directory should be
	cacheDir := filepath.Join(tempDir, "smart-suggestion")
	os.WriteFile(cacheDir, []byte("not a directory"), 0644)

	resetLoggerForTest(t)

	Enable(true)
	Log("test", nil)

	if Enabled() {
		// Log should detect initError and disable logging
		t.Error("expected debug to be disabled after init error")
	}
}

func TestExistingCacheIsMadePrivate(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
	t.Setenv("XDG_CACHE_HOME", tempDir)

	dir := filepath.Join(tempDir, "smart-suggestion")
	logPath := filepath.Join(dir, "debug.log")
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	// Force group/other access in case the process umask already masked it.
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("old"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logPath, 0o666); err != nil {
		t.Fatal(err)
	}

	resetLoggerForTest(t)
	Enable(true)
	Log("perm check", nil)

	assertOwnerPrivate := func(path string) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("%s is still accessible by group/other: %o", path, perm)
		}
	}
	assertOwnerPrivate(dir)
	assertOwnerPrivate(logPath)
}
