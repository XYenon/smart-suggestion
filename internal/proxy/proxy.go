//go:build unix

package proxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/xyenon/smart-suggestion/internal/debug"
	"github.com/xyenon/smart-suggestion/internal/session"
	"golang.org/x/term"
)

type ProxyOptions struct {
	LogFile     string
	SessionID   string
	BufferLines int
}

var execCommand = exec.Command

func RunProxy(shell string, opts ProxyOptions) error {
	return RunProxyWithIO(shell, opts, os.Stdin, os.Stdout)
}

func RunProxyWithIO(shell string, opts ProxyOptions, stdin io.Reader, stdout io.Writer) error {
	if os.Getenv("SMART_SUGGESTION_PROXY_ACTIVE") != "" {
		debug.Log("Already inside a proxy session, preventing nesting", map[string]any{
			"existing_proxy_pid": os.Getenv("SMART_SUGGESTION_PROXY_ACTIVE"),
		})
		return nil
	}

	sessionLogFile := session.GetSessionBasedLogFile(opts.LogFile, opts.SessionID)
	baseLockFile := strings.TrimSuffix(opts.LogFile, filepath.Ext(opts.LogFile)) + ".lock"
	sessionLockFile := getSessionBasedLockFile(baseLockFile, opts.SessionID)

	lockFile, err := createProcessLock(sessionLockFile)
	if err != nil {
		debug.Log("Failed to create process lock", map[string]any{
			"error":      err.Error(),
			"lock_path":  sessionLockFile,
			"session_id": opts.SessionID,
		})
		return fmt.Errorf("failed to create process lock: %w", err)
	}
	defer cleanupProcessLock(lockFile, sessionLockFile)

	os.Setenv("SMART_SUGGESTION_SESSION_ID", opts.SessionID)
	os.Setenv("SMART_SUGGESTION_PROXY_ACTIVE", fmt.Sprintf("%d", os.Getpid()))

	if err := cleanupOldSessionLogs(opts.LogFile, 24*time.Hour); err != nil {
		debug.Log("Failed to cleanup old session logs", map[string]any{"error": err.Error()})
	}

	debug.Log("Starting shell proxy mode with PTY", map[string]any{
		"log_file":   sessionLogFile,
		"lock_file":  sessionLockFile,
		"session_id": opts.SessionID,
		"pid":        os.Getpid(),
	})

	c := execCommand(shell)
	ptmx, err := pty.Start(c)
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			if f, ok := stdin.(*os.File); ok {
				if err := pty.InheritSize(f, ptmx); err != nil {
					debug.Log("Error resizing pty", map[string]any{"error": err.Error()})
				}
			}
		}
	}()
	ch <- syscall.SIGWINCH
	defer func() { signal.Stop(ch); close(ch) }()

	var oldState *term.State
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		oldState, err = term.MakeRaw(int(f.Fd()))
		if err != nil {
			debug.Log("Failed to set raw mode", map[string]any{"error": err.Error()})
			return fmt.Errorf("failed to set raw mode: %w", err)
		}
		defer func() {
			if oldState != nil {
				_ = term.Restore(int(f.Fd()), oldState)
			}
		}()
	} else {
		debug.Log("Stdin is not a terminal, skipping raw mode", map[string]any{})
	}

	if _, err := os.Stat(sessionLogFile); err == nil {
		if err := os.Remove(sessionLogFile); err != nil {
			debug.Log("Failed to delete session log file", map[string]any{
				"error":      err.Error(),
				"log_file":   sessionLogFile,
				"session_id": opts.SessionID,
			})
		}
	}

	logFile, err := os.OpenFile(sessionLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open session log file: %w", err)
	}
	defer logFile.Close()

	bufferLines := opts.BufferLines
	if bufferLines <= 0 {
		bufferLines = 100
	}
	limitedLogWriter := newLineLimitedWriter(logFile, sessionLogFile, bufferLines)

	teeWriter := io.MultiWriter(stdout, limitedLogWriter)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, err := io.Copy(ptmx, stdin)
		if err != nil {
			debug.Log("Error copying stdin to pty", map[string]any{"error": err.Error()})
		}
	}()

	go func() {
		defer wg.Done()
		_, err := io.Copy(teeWriter, ptmx)
		if err != nil {
			debug.Log("Error copying pty to output", map[string]any{"error": err.Error()})
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		debug.Log("PTY session completed", map[string]any{"log_file": opts.LogFile})
	case sig := <-sigCh:
		debug.Log("Received signal, shutting down", map[string]any{
			"signal":   sig.String(),
			"log_file": opts.LogFile,
		})
	}

	_ = c.Wait()

	return nil
}

func getSessionBasedLockFile(baseLockFile, sessionID string) string {
	if sessionID == "" {
		return baseLockFile
	}
	dir := filepath.Dir(baseLockFile)
	base := filepath.Base(baseLockFile)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%s%s", base, sessionID, ext))
}

func createProcessLock(lockPath string) (*os.File, error) {
	dir := filepath.Dir(lockPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			if isProcessRunning(lockPath) {
				return nil, fmt.Errorf("another instance is already running")
			}
			os.Remove(lockPath)
			file, err = os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
			if err != nil {
				return nil, fmt.Errorf("failed to create lock file: %w", err)
			}
		} else {
			return nil, fmt.Errorf("failed to create lock file: %w", err)
		}
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		os.Remove(lockPath)
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	pid := os.Getpid()
	if _, err := file.WriteString(fmt.Sprintf("%d\n", pid)); err != nil {
		file.Close()
		os.Remove(lockPath)
		return nil, fmt.Errorf("failed to write PID to lock file: %w", err)
	}

	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(lockPath)
		return nil, fmt.Errorf("failed to sync lock file: %w", err)
	}

	return file, nil
}

func isProcessRunning(lockPath string) bool {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func cleanupProcessLock(file *os.File, lockPath string) {
	if file != nil {
		syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		file.Close()
	}
	os.Remove(lockPath)
}

func cleanupOldSessionLogs(baseLogPath string, maxAge time.Duration) error {
	dir := filepath.Dir(baseLogPath)
	base := filepath.Base(baseLogPath)

	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}

	pattern := fmt.Sprintf("%s.*%s", base, ext)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	cutoff := time.Now().Add(-maxAge)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if matched, _ := filepath.Match(pattern, filename); !matched {
			continue
		}

		if filename == filepath.Base(baseLogPath) {
			continue
		}

		fullPath := filepath.Join(dir, filename)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if info.ModTime().Before(cutoff) {
			os.Remove(fullPath)
		}
	}

	return nil
}

type lineLimitedWriter struct {
	file     *os.File
	filePath string
	maxLines int
	lines    []string
	buf      []byte
	mu       sync.Mutex
}

func newLineLimitedWriter(file *os.File, filePath string, maxLines int) *lineLimitedWriter {
	return &lineLimitedWriter{
		file:     file,
		filePath: filePath,
		maxLines: maxLines,
		lines:    make([]string, 0, maxLines),
	}
}

func (w *lineLimitedWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)

	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}

		line := string(w.buf[:idx+1])
		w.buf = w.buf[idx+1:]

		w.lines = append(w.lines, line)
		if len(w.lines) > w.maxLines {
			w.lines = w.lines[1:]
		}
	}

	if err := w.flush(); err != nil {
		return len(p), err
	}

	return len(p), nil
}

func (w *lineLimitedWriter) flush() error {
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, 0); err != nil {
		return err
	}
	for _, line := range w.lines {
		if _, err := w.file.WriteString(line); err != nil {
			return err
		}
	}
	return nil
}
