//go:build unix

package proxy

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/xyenon/smart-suggestion/internal/debug"
	"github.com/xyenon/smart-suggestion/pkg"
)

const vtFlushInterval = 50 * time.Millisecond

type vtSink struct {
	file     *os.File
	filePath string
	maxLines int
	emulator *vt.Emulator
	closing  bool
	mu       sync.Mutex
	fileMu   sync.Mutex
	flushCh  chan struct{}
	flushNow chan struct{}
	closeCh  chan struct{}
	flushWG  sync.WaitGroup
	drainWG  sync.WaitGroup
}

func newVTSink(file *os.File, filePath string, maxLines, width, height int) *vtSink {
	if maxLines <= 0 {
		maxLines = 1
	}
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	emulator := vt.NewEmulator(width, height)
	emulator.SetScrollbackSize(maxLines)
	sink := &vtSink{
		file:     file,
		filePath: filePath,
		maxLines: maxLines,
		emulator: emulator,
		flushCh:  make(chan struct{}, 1),
		flushNow: make(chan struct{}, 1),
		closeCh:  make(chan struct{}),
	}
	sink.drainWG.Add(1)
	go func() {
		defer sink.drainWG.Done()
		_, _ = io.Copy(io.Discard, emulator)
	}()
	sink.flushWG.Add(1)
	go sink.flushLoop()
	return sink
}

func (w *vtSink) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emulator == nil || w.closing {
		return 0, io.ErrClosedPipe
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			debug.Log("Virtual terminal write panicked", map[string]any{"error": fmt.Sprint(recovered)})
			n = len(p)
			err = nil
		}
	}()
	if _, err := w.emulator.Write(p); err != nil {
		return 0, err
	}
	w.requestFlush(bytes.IndexByte(p, '\n') >= 0)
	return len(p), nil
}

func (w *vtSink) Resize(width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emulator == nil || w.closing {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			debug.Log("Virtual terminal resize panicked", map[string]any{
				"error":  fmt.Sprint(recovered),
				"width":  width,
				"height": height,
			})
		}
	}()
	w.emulator.Resize(width, height)
	w.requestFlush(false)
}

func (w *vtSink) Close() error {
	w.mu.Lock()
	if w.emulator == nil {
		w.mu.Unlock()
		return nil
	}
	if w.closing {
		w.mu.Unlock()
		w.flushWG.Wait()
		return nil
	}
	w.closing = true
	w.mu.Unlock()

	close(w.closeCh)
	w.flushWG.Wait()
	flushErr := w.flush()

	w.mu.Lock()
	emulator := w.emulator
	w.emulator = nil
	responsePipe, ok := emulator.InputPipe().(io.Closer)
	var closeErr error
	if ok {
		closeErr = responsePipe.Close()
	} else {
		closeErr = emulator.Close()
	}
	w.mu.Unlock()

	w.drainWG.Wait()
	w.fileMu.Lock()
	file := w.file
	w.file = nil
	w.fileMu.Unlock()

	if fileErr := file.Close(); fileErr != nil && flushErr == nil && closeErr == nil {
		return fileErr
	}
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func (w *vtSink) requestFlush(immediate bool) {
	if immediate {
		select {
		case w.flushNow <- struct{}{}:
		default:
		}
		return
	}
	select {
	case w.flushCh <- struct{}{}:
	default:
	}
}

func (w *vtSink) flushLoop() {
	defer w.flushWG.Done()
	var timer *time.Timer
	var timerCh <-chan time.Time
	for {
		select {
		case <-w.flushCh:
			if timer == nil {
				timer = time.NewTimer(vtFlushInterval)
				timerCh = timer.C
			}
		case <-w.flushNow:
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer = nil
				timerCh = nil
			}
			select {
			case <-w.flushCh:
			default:
			}
			w.flushBestEffort()
		case <-timerCh:
			w.flushBestEffort()
			timer = nil
			timerCh = nil
		case <-w.closeCh:
			if timer != nil {
				timer.Stop()
			}
			return
		}
	}
}

func (w *vtSink) flushBestEffort() {
	if err := w.flush(); err != nil {
		debug.Log("Failed to persist virtual terminal snapshot", map[string]any{"error": err.Error()})
	}
}

func (w *vtSink) flush() error {
	w.mu.Lock()
	if w.emulator == nil {
		w.mu.Unlock()
		return nil
	}
	content := w.snapshotLocked()
	w.mu.Unlock()

	w.fileMu.Lock()
	defer w.fileMu.Unlock()
	return pkg.WithLogRotateLock(w.filePath, func() error {
		return w.persistSnapshotLocked(content)
	})
}

func (w *vtSink) persistSnapshotLocked(content string) error {
	if err := w.reopenIfRotated(); err != nil {
		return err
	}
	if err := w.file.Truncate(0); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, 0); err != nil {
		return err
	}
	_, err := io.WriteString(w.file, content)
	return err
}

func (w *vtSink) snapshotLocked() string {
	physicalLines := w.emulator.PhysicalLines()
	for len(physicalLines) > 0 && physicalLines[len(physicalLines)-1].String() == "" {
		physicalLines = physicalLines[:len(physicalLines)-1]
	}
	if len(physicalLines) > w.maxLines {
		physicalLines = physicalLines[len(physicalLines)-w.maxLines:]
	}

	logicalLines := make([]string, 0, len(physicalLines))
	var logicalLine strings.Builder
	for _, line := range physicalLines {
		logicalLine.WriteString(line.String())
		if !line.Wrapped() {
			logicalLines = append(logicalLines, logicalLine.String())
			logicalLine.Reset()
		}
	}
	if logicalLine.Len() > 0 {
		logicalLines = append(logicalLines, logicalLine.String())
	}
	return strings.Join(logicalLines, "\n")
}

func (w *vtSink) reopenIfRotated() error {
	info, err := os.Stat(w.filePath)
	switch {
	case err == nil:
		current, statErr := w.file.Stat()
		if statErr == nil && os.SameFile(current, info) {
			return nil
		}
	case !os.IsNotExist(err):
		return err
	}

	if w.file != nil {
		_ = w.file.Close()
	}
	f, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to reopen session log file after rotation: %w", err)
	}
	w.file = f
	return nil
}
