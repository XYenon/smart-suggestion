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
	"github.com/xyenon/smart-suggestion/pkg"
	"golang.org/x/term"
)

type ProxyOptions struct {
	LogFile         string
	SessionID       string
	ScrollbackLines int
}

var execCommand = exec.Command

func RunProxy(shell string, opts ProxyOptions) error {
	return RunProxyWithIO(shell, opts, os.Stdin, os.Stdout)
}

func RunProxyWithIO(shell string, opts ProxyOptions, stdin io.Reader, stdout io.Writer) error {
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
	width, height := terminalSize(stdin)
	ptmx, err := pty.StartWithSize(c, &pty.Winsize{
		Rows: uint16(height),
		Cols: uint16(width),
	})
	if err != nil {
		return fmt.Errorf("failed to start PTY: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

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

	scrollbackLines := opts.ScrollbackLines
	if scrollbackLines <= 0 {
		scrollbackLines = 100
	}
	vtLogWriter := newVTSink(logFile, sessionLogFile, scrollbackLines, width, height)
	defer vtLogWriter.Close()

	resize := func() {
		f, ok := stdin.(*os.File)
		if !ok {
			return
		}
		if err := pty.InheritSize(f, ptmx); err != nil {
			debug.Log("Error resizing pty", map[string]any{"error": err.Error()})
			return
		}
		width, height = terminalSize(stdin)
		vtLogWriter.Resize(width, height)
	}
	resize()

	resizeCh := make(chan os.Signal, 1)
	resizeDone := make(chan struct{})
	var resizeWG sync.WaitGroup
	resizeWG.Add(1)
	signal.Notify(resizeCh, syscall.SIGWINCH)
	go func() {
		defer resizeWG.Done()
		for {
			select {
			case <-resizeCh:
				resize()
			case <-resizeDone:
				return
			}
		}
	}()
	defer func() {
		signal.Stop(resizeCh)
		close(resizeDone)
		resizeWG.Wait()
	}()

	teeWriter := io.MultiWriter(stdout, bestEffortLogWriter{writer: vtLogWriter})

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Only wait for pty→stdout goroutine to determine session end
	var outWG sync.WaitGroup
	outWG.Add(1)

	// stdin → pty: not used as exit condition, allowed to block in background
	go func() {
		_, err := io.Copy(ptmx, stdin)
		if err != nil {
			debug.Log("Error copying stdin to pty", map[string]any{"error": err.Error()})
		}
	}()

	// pty → stdout & log: ends when shell exits and pty EOF
	go func() {
		defer outWG.Done()
		_, err := io.Copy(teeWriter, ptmx)
		if err != nil {
			debug.Log("Error copying pty to output", map[string]any{"error": err.Error()})
		}
	}()

	done := make(chan struct{})
	go func() {
		outWG.Wait()
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
		// Close pty to unblock goroutines when receiving signal
		_ = ptmx.Close()
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

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another instance is already running")
	}

	if err := file.Truncate(0); err != nil {
		cleanupProcessLock(file, lockPath)
		return nil, fmt.Errorf("failed to truncate lock file: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		cleanupProcessLock(file, lockPath)
		return nil, fmt.Errorf("failed to rewind lock file: %w", err)
	}

	if _, err := file.WriteString(fmt.Sprintf("%d\n", os.Getpid())); err != nil {
		cleanupProcessLock(file, lockPath)
		return nil, fmt.Errorf("failed to write PID to lock file: %w", err)
	}
	if err := file.Sync(); err != nil {
		cleanupProcessLock(file, lockPath)
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
}

func sessionLockPathForLog(baseLogPath, sessionLogPath string) string {
	sessionID := sessionIDFromLog(baseLogPath, sessionLogPath)
	baseLock := strings.TrimSuffix(baseLogPath, filepath.Ext(baseLogPath)) + ".lock"
	return getSessionBasedLockFile(baseLock, sessionID)
}

func sessionIDFromLog(baseLogPath, sessionLogPath string) string {
	base := filepath.Base(baseLogPath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	sessionBase := filepath.Base(sessionLogPath)
	prefix := name + "."
	if !strings.HasPrefix(sessionBase, prefix) || !strings.HasSuffix(sessionBase, ext) {
		return ""
	}
	return sessionBase[len(prefix) : len(sessionBase)-len(ext)]
}

func sessionLockHeld(lockPath string) bool {
	file, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return true
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return false
}

func staleIdleSessionLog(baseLogPath, path string, cutoff time.Time) bool {
	if sessionLockHeld(sessionLockPathForLog(baseLogPath, path)) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.ModTime().Before(cutoff)
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

		if filename == filepath.Base(baseLogPath) || strings.HasSuffix(filename, ".lock") {
			continue
		}

		fullPath := filepath.Join(dir, filename)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if !info.ModTime().Before(cutoff) || sessionLockHeld(sessionLockPathForLog(baseLogPath, fullPath)) {
			continue
		}
		_ = pkg.WithLogRotateLock(fullPath, func() error {
			if staleIdleSessionLog(baseLogPath, fullPath, cutoff) {
				os.Remove(fullPath)
			}
			return nil
		})
	}

	return nil
}

func terminalSize(stdin io.Reader) (width, height int) {
	const defaultWidth, defaultHeight = 80, 24
	f, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return defaultWidth, defaultHeight
	}
	width, height, err := term.GetSize(int(f.Fd()))
	if err != nil || width <= 0 || height <= 0 {
		return defaultWidth, defaultHeight
	}
	return width, height
}

type bestEffortLogWriter struct {
	writer io.Writer
}

func (w bestEffortLogWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		debug.Log("Failed to capture PTY output", map[string]any{"error": err.Error()})
	}
	return len(p), nil
}
