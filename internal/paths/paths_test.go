package paths

import (
	"path/filepath"
	"testing"
)

func TestGetCacheDir(t *testing.T) {
	t.Run("SMART_SUGGESTION_CACHE_DIR set", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("SMART_SUGGESTION_CACHE_DIR", tempDir)
		t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "xdg"))

		if got := GetCacheDir(); got != tempDir {
			t.Errorf("expected %q, got %q", tempDir, got)
		}
	})

	t.Run("XDG_CACHE_HOME set", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", tempDir)

		expected := filepath.Join(tempDir, "smart-suggestion")
		if got := GetCacheDir(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("XDG_CACHE_HOME unset", func(t *testing.T) {
		t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
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
	t.Run("XDG_CACHE_HOME set", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("SMART_SUGGESTION_CACHE_DIR", "")
		t.Setenv("XDG_CACHE_HOME", tempDir)

		expected := filepath.Join(tempDir, "smart-suggestion", ProxyLogFilename)
		if got := GetDefaultProxyLogFile(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("SMART_SUGGESTION_CACHE_DIR set", func(t *testing.T) {
		tempDir := t.TempDir()
		t.Setenv("SMART_SUGGESTION_CACHE_DIR", tempDir)
		t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "xdg"))

		expected := filepath.Join(tempDir, ProxyLogFilename)
		if got := GetDefaultProxyLogFile(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})
}
