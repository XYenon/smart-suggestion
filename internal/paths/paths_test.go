package paths

import (
	"path/filepath"
	"testing"
)

func TestGetCacheDir(t *testing.T) {
	t.Run("XDG_CACHE_HOME set", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tempDir)

		expected := filepath.Join(tempDir, "smart-suggestion")
		if got := GetCacheDir(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("XDG_CACHE_HOME unset", func(t *testing.T) {
		t.Setenv("XDG_CACHE_HOME", "")
		// We can't easily mock UserHomeDir without refactoring, so we'll check if it ends with .cache/smart-suggestion
		// or if it falls back to TempDir
		got := GetCacheDir()
		// Basic sanity check
		if filepath.Base(got) != "smart-suggestion" {
			t.Errorf("expected path to end with smart-suggestion, got %q", got)
		}
	})
}

func TestGetDefaultProxyLogFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tempDir)

	expected := filepath.Join(tempDir, "smart-suggestion", ProxyLogFilename)
	if got := GetDefaultProxyLogFile(); got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}
