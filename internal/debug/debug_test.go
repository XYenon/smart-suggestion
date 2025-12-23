package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLog(t *testing.T) {
	// Create a temp dir for cache
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	// Reset state
	mu.Lock()
	enabled = false
	logFile = nil
	logger = nil
	initOnce = *new(sync.Once) // Reset sync.Once
	initError = nil
	mu.Unlock()

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

	// Clean up
	Close()
}

func TestClose(t *testing.T) {
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
	Enable(false)
	if Enabled() {
		t.Error("expected debug to be disabled")
	}
	Log("should not log", nil)
}

func TestInitError(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	// Create a file where the directory should be
	cacheDir := filepath.Join(tempDir, "smart-suggestion")
	os.WriteFile(cacheDir, []byte("not a directory"), 0644)

	// Reset state
	mu.Lock()
	enabled = false
	logFile = nil
	logger = nil
	initOnce = *new(sync.Once)
	initError = nil
	mu.Unlock()

	Enable(true)
	Log("test", nil)

	if Enabled() {
		// Log should detect initError and disable logging
		t.Error("expected debug to be disabled after init error")
	}
}
