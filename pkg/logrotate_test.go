package pkg

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSizeString(t *testing.T) {
	cases := []struct {
		input    string
		expected int64
	}{
		{"100", 100},
		{"1KB", 1024},
		{"1MB", 1024 * 1024},
		{"1GB", 1024 * 1024 * 1024},
		{"  500 KB  ", 500 * 1024},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseSizeString(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.expected {
				t.Errorf("expected %d, got %d", tc.expected, got)
			}
		})
	}
}

func TestLogRotator_CheckAndRotate(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	config := &LogRotateConfig{
		MaxSize:    10, // 10 bytes
		MaxBackups: 1,
		MaxAge:     1, // 1 day
		Compress:   false,
	}
	lr := NewLogRotator(config)

	// Case 1: File doesn't exist
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Errorf("unexpected error for non-existent file: %v", err)
	}

	// Case 2: File small
	os.WriteFile(logFile, []byte("small"), 0644)
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(logFile); err != nil {
		t.Error("expected log file to still exist")
	}

	// Case 3: File large (trigger rotation)
	os.WriteFile(logFile, []byte("this is a very large log file content"), 0644)
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original file should be gone (moved to backup)
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("expected original log file to be rotated (moved)")
	}

	// Check if backup exists
	backups, err := lr.GetBackupFiles(logFile)
	if err != nil {
		t.Fatalf("GetBackupFiles error: %v", err)
	}
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	}
}

func TestLogRotator_ForceRotate(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	os.WriteFile(logFile, []byte("some content"), 0644)

	lr := NewLogRotator(&LogRotateConfig{MaxAge: 1})
	if err := lr.ForceRotate(logFile); err != nil {
		t.Fatalf("ForceRotate error: %v", err)
	}

	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		t.Error("expected original file to be rotated")
	}
}

func TestLogRotator_MaxBackups(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	config := &LogRotateConfig{
		MaxSize:    1,
		MaxBackups: 2,
		MaxAge:     1,
		Compress:   false,
	}
	lr := NewLogRotator(config)

	// Create 3 rotations
	for i := 0; i < 3; i++ {
		os.WriteFile(logFile, []byte("content"), 0644)
		if err := lr.CheckAndRotate(logFile); err != nil {
			t.Fatalf("rotation %d error: %v", i, err)
		}
		time.Sleep(1100 * time.Millisecond) // Ensure unique timestamps
	}

	backups, _ := lr.GetBackupFiles(logFile)
	if len(backups) != 2 {
		t.Errorf("expected 2 backups (MaxBackups limit), got %d", len(backups))
	}
}

func TestLogRotator_Compression(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	config := &LogRotateConfig{
		MaxSize:    10,
		MaxBackups: 1,
		MaxAge:     1,
		Compress:   true,
	}
	lr := NewLogRotator(config)

	os.WriteFile(logFile, []byte("this is a very large log file content"), 0644)
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backups, _ := lr.GetBackupFiles(logFile)
	if len(backups) != 1 {
		t.Errorf("expected 1 backup, got %d", len(backups))
	} else if filepath.Ext(backups[0]) != ".gz" {
		t.Errorf("expected backup to have .gz extension, got %s", filepath.Ext(backups[0]))
	}
}
