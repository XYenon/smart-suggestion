package debug

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/xyenon/smart-suggestion/internal/paths"
)

var (
	enabled   bool
	mu        sync.RWMutex
	logger    *log.Logger
	logFile   *os.File
	initOnce  sync.Once
	initError error
)

func Enable(e bool) {
	mu.Lock()
	defer mu.Unlock()
	enabled = e
	if e {
		initOnce.Do(initLogger)
	}
}

func Enabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return enabled
}

func initLogger() {
	logFilePath := filepath.Join(paths.GetCacheDir(), "debug.log")
	if err := os.MkdirAll(filepath.Dir(logFilePath), 0755); err != nil {
		initError = fmt.Errorf("failed to create cache directory: %w", err)
		return
	}

	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		initError = fmt.Errorf("failed to open debug log file: %w", err)
		return
	}

	logFile = f
	logger = log.New(f, "", 0)
}

func Log(message string, data map[string]any) {
	if !Enabled() {
		return
	}

	if initError != nil {
		fmt.Fprintf(os.Stderr, "Debug logging failed to initialize: %v\n", initError)
		mu.Lock()
		enabled = false
		mu.Unlock()
		return
	}

	mu.RLock()
	l := logger
	mu.RUnlock()

	if l == nil {
		return
	}

	logEntry := map[string]any{
		"date": time.Now().Format(time.RFC3339),
		"log":  message,
	}
	for k, v := range data {
		logEntry[k] = v
	}

	jsonData, err := json.Marshal(logEntry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to marshal debug log: %v\n", err)
		return
	}

	l.Println(string(jsonData))
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
		logger = nil
	}
}
