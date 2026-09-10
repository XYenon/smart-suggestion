package pkg

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
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
	if _, err := os.Stat(logRotateLockPath(logFile)); !os.IsNotExist(err) {
		t.Fatalf("missing log should not create a rotation lock: %v", err)
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

func TestForceRotateMissingFileDoesNotCreateLock(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "missing.log")
	lr := NewLogRotator(&LogRotateConfig{MaxAge: 1})
	if err := lr.ForceRotate(logFile); err != nil {
		t.Fatalf("ForceRotate missing file: %v", err)
	}
	if _, err := os.Stat(logRotateLockPath(logFile)); !os.IsNotExist(err) {
		t.Fatalf("missing log should not create a rotation lock: %v", err)
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

func TestRotateWaitsForPerLogLock(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	if err := os.WriteFile(logFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(logRotateLockPath(logFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		lr := NewLogRotator(&LogRotateConfig{MaxSize: 1, MaxBackups: 5, MaxAge: 1, Compress: false})
		done <- lr.CheckAndRotate(logFile)
	}()

	select {
	case err := <-done:
		t.Fatalf("rotation finished while lock was held: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("rotation did not proceed after lock was released")
	}
}

func TestRotateRemovesReservationSidecar(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	lr := NewLogRotator(&LogRotateConfig{MaxSize: 1, MaxBackups: 5, MaxAge: 1, Compress: false})
	if err := os.WriteFile(logFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".reserving") {
			t.Fatalf("reservation sidecar left after rotate: %s", entry.Name())
		}
	}
}

func TestLogRotator_SameSecondBackupNames(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	lr := NewLogRotator(&LogRotateConfig{MaxSize: 1, MaxBackups: 5, MaxAge: 1, Compress: false})

	for i := 0; i < 3; i++ {
		if err := os.WriteFile(logFile, []byte("content"), 0o644); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		if err := lr.CheckAndRotate(logFile); err != nil {
			t.Fatalf("rotation %d: %v", i, err)
		}
	}

	backups, err := lr.GetBackupFiles(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("expected 3 backups, got %d (%v)", len(backups), backups)
	}
}

func TestLogRotator_BackupMatchingIgnoresUnrelatedFiles(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	unrelatedFiles := []string{
		filepath.Join(tempDir, "test-notes.txt"),
		filepath.Join(tempDir, "test-20260829-123456-not-a-backup.log.bak"),
	}
	oldTime := time.Now().Add(-48 * time.Hour)
	for _, path := range unrelatedFiles {
		if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}

	lr := NewLogRotator(&LogRotateConfig{MaxSize: 1, MaxBackups: 1, MaxAge: 1, Compress: false})
	if err := os.WriteFile(logFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatal(err)
	}

	for _, path := range unrelatedFiles {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("unrelated file %s was removed: %v", path, err)
		}
	}

	backups, err := lr.GetBackupFiles(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 backup, got %d (%v)", len(backups), backups)
	}
}

func TestUniqueBackupPathSkipsCompressedSibling(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	timestamp := now.Format("20060102-150405")
	gz := filepath.Join(tempDir, fmt.Sprintf("test-%s.log.gz", timestamp))
	if err := os.WriteFile(gz, []byte("compressed"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := uniqueBackupPathAt(tempDir, "test", ".log", now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tempDir, fmt.Sprintf("test-%s-1.log", timestamp))
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	assertReservedBackupName(t, got)
}

func TestUniqueBackupPathMoreThan100Candidates(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	timestamp := now.Format("20060102-150405")
	for i := 0; i <= 100; i++ {
		suffix := timestamp
		if i > 0 {
			suffix = fmt.Sprintf("%s-%d", timestamp, i)
		}
		path := filepath.Join(tempDir, fmt.Sprintf("test-%s.log", suffix))
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := uniqueBackupPathAt(tempDir, "test", ".log", now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(tempDir, fmt.Sprintf("test-%s-101.log", timestamp))
	if got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	assertReservedBackupName(t, got)
}

func TestUniqueBackupPathErrorWhenDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	_, err := uniqueBackupPathAt(missing, "test", ".log", time.Now())
	if err == nil {
		t.Fatal("expected error for missing backup directory")
	}
}

func TestUniqueBackupPathReservesExclusively(t *testing.T) {
	tempDir := t.TempDir()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	const n = 50

	paths := make([]string, n)
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			p, err := uniqueBackupPathAt(tempDir, "test", ".log", now)
			if err != nil {
				errCh <- err
				return
			}
			paths[i] = p
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	seen := make(map[string]struct{}, n)
	for _, p := range paths {
		if p == "" {
			t.Fatal("empty reserved path")
		}
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate reserved path %s", p)
		}
		seen[p] = struct{}{}
		assertReservedBackupName(t, p)
	}
}

func assertReservedBackupName(t *testing.T, backupPath string) {
	t.Helper()
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Fatalf("final backup path %s should not exist until rename: %v", backupPath, err)
	}
	if _, err := os.Stat(backupReservationPath(backupPath)); err != nil {
		t.Fatalf("reservation sidecar missing for %s: %v", backupPath, err)
	}
}

func TestCleanupRemovesStaleReservationAndTempGzip(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	sidecar := filepath.Join(tempDir, "test-20260910-120000.log.reserving")
	tmpGz := filepath.Join(tempDir, "test-20260910-120000.log.gz.tmp.1234")
	if err := os.WriteFile(sidecar, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpGz, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	lr := NewLogRotator(&LogRotateConfig{MaxSize: 1, MaxBackups: 1, MaxAge: 1, Compress: false})
	if err := os.WriteFile(logFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{sidecar, tmpGz} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale rotation file %s was not removed: %v", path, err)
		}
	}

	backups, err := lr.GetBackupFiles(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected 1 real backup, got %d (%v)", len(backups), backups)
	}
}

func TestCleanupUsesFilenameStampNotMtime(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")
	oldBackup := filepath.Join(tempDir, "test-20200101-000000.log")
	if err := os.WriteFile(oldBackup, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldBackup, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	lr := NewLogRotator(&LogRotateConfig{MaxSize: 1, MaxBackups: 5, MaxAge: 1, Compress: false})
	if err := os.WriteFile(logFile, []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := lr.CheckAndRotate(logFile); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldBackup); !os.IsNotExist(err) {
		t.Fatalf("backup with old rotation stamp was kept despite recent mtime: %v", err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("forced read failure")
}

func TestCompressFileFailureLeavesNoPartialGz(t *testing.T) {
	tempDir := t.TempDir()
	dst := filepath.Join(tempDir, "src.log.gz")

	lr := NewLogRotator(nil)
	if err := lr.compressReader(failReader{}, dst); err == nil {
		t.Fatal("expected compression to fail")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial gzip file was left at %s: %v", dst, err)
	}

	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp.") {
			t.Fatalf("temporary gzip file was left: %s", entry.Name())
		}
	}
}

func TestCompressFileClosesGzipWriter(t *testing.T) {
	tempDir := t.TempDir()
	src := filepath.Join(tempDir, "src.log")
	dst := filepath.Join(tempDir, "src.log.gz")
	if err := os.WriteFile(src, []byte("compress me"), 0o644); err != nil {
		t.Fatal(err)
	}

	lr := NewLogRotator(nil)
	if err := lr.compressFile(src, dst); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip footer missing: %v", err)
	}
	defer gr.Close()
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "compress me" {
		t.Fatalf("got %q", got)
	}
}
