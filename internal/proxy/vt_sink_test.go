//go:build unix

package proxy

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/xyenon/smart-suggestion/pkg"
)

func newTestVTSink(t *testing.T, maxLines, width, height int) (*vtSink, string) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "proxy.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	sink := newVTSink(file, logPath, maxLines, width, height)
	t.Cleanup(func() {
		if err := sink.Close(); err != nil {
			t.Errorf("close VT sink: %v", err)
		}
	})
	return sink, logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	var content []byte
	err := pkg.WithLogReadLock(path, func() error {
		var err error
		content, err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func writeVT(t *testing.T, sink *vtSink, text string) {
	t.Helper()
	n, err := sink.Write([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if n != len(text) {
		t.Fatalf("wrote %d bytes, want %d", n, len(text))
	}
	if err := sink.flush(); err != nil {
		t.Fatalf("flush VT sink: %v", err)
	}
}

func TestVTSinkAppliesTerminalSequences(t *testing.T) {
	sink, logPath := newTestVTSink(t, 20, 40, 4)

	writeVT(t, sink, "\x1b[31mfirst\x1b[0m\r\nsecond")
	writeVT(t, sink, "\x1b[1A\rupdated\x1b[K")

	if got, want := readLog(t, logPath), "updated\nsecond"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestVTSinkCapturesPartialLineAndRedraw(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 40, 4)

	writeVT(t, sink, "Loading 10%")
	if got, want := readLog(t, logPath), "Loading 10%"; got != want {
		t.Fatalf("partial log = %q, want %q", got, want)
	}
	writeVT(t, sink, "\rLoading 100%\x1b[K")
	if got, want := readLog(t, logPath), "Loading 100%"; got != want {
		t.Fatalf("redrawn log = %q, want %q", got, want)
	}
}

func TestVTSinkKeepsLatestTerminalLines(t *testing.T) {
	sink, logPath := newTestVTSink(t, 3, 20, 2)

	writeVT(t, sink, "line1\r\nline2\r\nline3\r\nline4\r\nline5")

	if got, want := readLog(t, logPath), "line3\nline4\nline5"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestVTSinkAlternateScreen(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 30, 3)

	writeVT(t, sink, "main screen")
	writeVT(t, sink, "\x1b[?1049hfull-screen app")
	if got, want := readLog(t, logPath), "full-screen app"; got != want {
		t.Fatalf("alternate screen log = %q, want %q", got, want)
	}
	writeVT(t, sink, "\x1b[?1049l")
	if got, want := readLog(t, logPath), "main screen"; got != want {
		t.Fatalf("restored main screen log = %q, want %q", got, want)
	}
}

func TestVTSinkWideAndCombinedCharacters(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 20, 3)

	writeVT(t, sink, "中文 e\u0301 👩‍💻")

	if got, want := readLog(t, logPath), "中文 é 👩‍💻"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestVTSinkJoinsSoftWrapsAndPreservesHardBreaks(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 8, 4)

	writeVT(t, sink, "abcdefghijk\r\nhard break")

	if got, want := readLog(t, logPath), "abcdefghijk\nhard break"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestVTSinkPreservesSpacesAtSoftWrapBoundary(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 4, 3)

	writeVT(t, sink, "abc def")

	if got, want := readLog(t, logPath), "abc def"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestVTSinkResizeReflowsWithoutLosingLogicalLine(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 12, 3)
	writeVT(t, sink, "abcdefghij")

	sink.Resize(5, 3)
	if got, want := readLog(t, logPath), "abcdefghij"; got != want {
		t.Fatalf("narrow log = %q, want %q", got, want)
	}
	sink.Resize(12, 3)
	if got, want := readLog(t, logPath), "abcdefghij"; got != want {
		t.Fatalf("expanded log = %q, want %q", got, want)
	}
}

func TestVTSinkDrainsTerminalResponses(t *testing.T) {
	sink, _ := newTestVTSink(t, 10, 20, 3)
	done := make(chan error, 1)
	go func() {
		_, err := sink.Write([]byte("\x1b[6n\x1b[c\x1b]10;?\x07"))
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("VT write blocked while producing terminal responses")
	}
}

func TestVTSinkFlushesAfterIdle(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 20, 3)
	if _, err := sink.Write([]byte("idle snapshot")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if readLog(t, logPath) == "idle snapshot" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("VT snapshot was not persisted after the flush interval")
}

func TestVTSinkFlushesAfterNewline(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 20, 3)
	if _, err := sink.Write([]byte("complete line\r\n")); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if readLog(t, logPath) == "complete line" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("VT snapshot was not persisted after a newline")
}

func TestVTSinkBlockedPersistenceDoesNotBlockOutput(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 20, 3)
	lock, err := os.OpenFile(logPath+".rotate.lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	var stdout strings.Builder
	writer := io.MultiWriter(&stdout, bestEffortLogWriter{writer: sink})
	if _, err := writer.Write([]byte("first ")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * vtFlushInterval)

	done := make(chan error, 1)
	go func() {
		_, err := writer.Write([]byte("second"))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("PTY output blocked behind log persistence")
	}
	if got, want := stdout.String(), "first second"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestVTSinkCloseFlushesPendingSnapshot(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "proxy.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	sink := newVTSink(file, logPath, 10, 20, 3)
	if _, err := sink.Write([]byte("final snapshot")); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if got, want := readLog(t, logPath), "final snapshot"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestVTSinkResize(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 20, 4)
	writeVT(t, sink, "one\r\ntwo")

	sink.Resize(10, 2)

	if sink.emulator.Width() != 10 || sink.emulator.Height() != 2 {
		t.Fatalf("size = %dx%d, want 10x2", sink.emulator.Width(), sink.emulator.Height())
	}
	if got := readLog(t, logPath); !strings.Contains(got, "two") {
		t.Fatalf("resized log lost current content: %q", got)
	}
}

func TestVTSinkReopensAfterRotation(t *testing.T) {
	sink, logPath := newTestVTSink(t, 10, 30, 3)
	writeVT(t, sink, "before")

	rotator := pkg.NewLogRotator(&pkg.LogRotateConfig{MaxAge: 1, MaxBackups: 5, Compress: false})
	if err := rotator.ForceRotate(logPath); err != nil {
		t.Fatal(err)
	}
	writeVT(t, sink, " after")

	if got := readLog(t, logPath); got != "before after" {
		t.Fatalf("reopened log = %q, want %q", got, "before after")
	}
	backups, err := rotator.GetBackupFiles(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("got %d backups, want 1", len(backups))
	}
	if got := readLog(t, backups[0]); got != "before" {
		t.Fatalf("backup = %q, want %q", got, "before")
	}
}

func TestVTSinkCloseClosesReopenedFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "proxy.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	sink := newVTSink(file, logPath, 10, 20, 3)
	writeVT(t, sink, "before")

	rotator := pkg.NewLogRotator(&pkg.LogRotateConfig{MaxAge: 1, MaxBackups: 5, Compress: false})
	if err := rotator.ForceRotate(logPath); err != nil {
		t.Fatal(err)
	}
	writeVT(t, sink, " after")
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	if sink.file != nil || sink.emulator != nil {
		t.Fatal("expected sink to release the log file and emulator")
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := sink.Write([]byte("closed")); err == nil {
		t.Fatal("expected write after close to fail")
	}
}

func TestVTSinkDefaultsAndIgnoresInvalidResize(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "proxy.log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	sink := newVTSink(file, logPath, 0, 0, 0)
	t.Cleanup(func() { _ = sink.Close() })

	if sink.maxLines != 1 || sink.emulator.Width() != 80 || sink.emulator.Height() != 24 {
		t.Fatalf("defaults = lines:%d size:%dx%d", sink.maxLines, sink.emulator.Width(), sink.emulator.Height())
	}
	sink.Resize(0, 10)
	if sink.emulator.Width() != 80 || sink.emulator.Height() != 24 {
		t.Fatalf("invalid resize changed size to %dx%d", sink.emulator.Width(), sink.emulator.Height())
	}
}
