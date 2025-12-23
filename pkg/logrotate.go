package pkg

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LogRotateConfig holds configuration for log rotation
type LogRotateConfig struct {
	// MaxSize is the maximum size in bytes before rotation (default: 10MB)
	MaxSize int64
	// MaxBackups is the maximum number of backup files to keep (default: 5)
	MaxBackups int
	// Compress determines if rotated files should be compressed (default: true)
	Compress bool
	// MaxAge is the maximum age in days to keep backup files (default: 30)
	MaxAge int
}

// DefaultLogRotateConfig returns default configuration
func DefaultLogRotateConfig() *LogRotateConfig {
	return &LogRotateConfig{
		MaxSize:    10 * 1024 * 1024, // 10MB
		MaxBackups: 5,
		Compress:   true,
		MaxAge:     30,
	}
}

// LogRotator handles log file rotation
type LogRotator struct {
	config *LogRotateConfig
	mutex  sync.Mutex
}

// NewLogRotator creates a new log rotator with the given configuration
func NewLogRotator(config *LogRotateConfig) *LogRotator {
	if config == nil {
		config = DefaultLogRotateConfig()
	}
	return &LogRotator{
		config: config,
	}
}

// CheckAndRotate checks if the log file needs rotation and performs it if necessary
func (lr *LogRotator) CheckAndRotate(logFilePath string) error {
	lr.mutex.Lock()
	defer lr.mutex.Unlock()

	// Check if file exists and get its size
	fileInfo, err := os.Stat(logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, no need to rotate
			return nil
		}
		return fmt.Errorf("failed to stat log file %s: %w", logFilePath, err)
	}

	// Check if rotation is needed
	if fileInfo.Size() < lr.config.MaxSize {
		return nil
	}

	// Perform rotation
	return lr.rotateFile(logFilePath)
}

// rotateFile performs the actual file rotation
func (lr *LogRotator) rotateFile(logFilePath string) error {
	// Generate timestamp for the backup file
	timestamp := time.Now().Format("20060102-150405")

	// Create backup filename
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	backupPath := filepath.Join(dir, fmt.Sprintf("%s-%s%s", name, timestamp, ext))

	// Move current log file to backup
	if err := os.Rename(logFilePath, backupPath); err != nil {
		return fmt.Errorf("failed to rename log file %s to %s: %w", logFilePath, backupPath, err)
	}

	// Compress the backup file if enabled
	if lr.config.Compress {
		compressedPath := backupPath + ".gz"
		if err := lr.compressFile(backupPath, compressedPath); err != nil {
			// Log the error but don't fail the rotation
			fmt.Fprintf(os.Stderr, "Warning: failed to compress backup file %s: %v\n", backupPath, err)
		} else {
			// Remove the uncompressed file
			os.Remove(backupPath)
			backupPath = compressedPath
		}
	}

	// Clean up old backup files
	if err := lr.cleanupOldBackups(logFilePath); err != nil {
		// Log the error but don't fail the rotation
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup old backups for %s: %v\n", logFilePath, err)
	}

	return nil
}

// compressFile compresses the source file to the destination using gzip
func (lr *LogRotator) compressFile(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dstPath, err)
	}
	defer dstFile.Close()

	gzipWriter := gzip.NewWriter(dstFile)
	defer gzipWriter.Close()

	if _, err := io.Copy(gzipWriter, srcFile); err != nil {
		return fmt.Errorf("failed to compress file: %w", err)
	}

	return nil
}

// cleanupOldBackups removes old backup files based on MaxBackups and MaxAge settings
func (lr *LogRotator) cleanupOldBackups(logFilePath string) error {
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Find all backup files
	pattern := filepath.Join(dir, fmt.Sprintf("%s-*%s*", name, ext))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to find backup files with pattern %s: %w", pattern, err)
	}

	// Create a list of backup files with their info
	type backupFile struct {
		path    string
		modTime time.Time
	}

	var backups []backupFile
	cutoffTime := time.Now().AddDate(0, 0, -lr.config.MaxAge)

	for _, match := range matches {
		// Skip the current log file
		if match == logFilePath {
			continue
		}

		fileInfo, err := os.Stat(match)
		if err != nil {
			continue
		}

		// Remove files older than MaxAge
		if fileInfo.ModTime().Before(cutoffTime) {
			os.Remove(match)
			continue
		}

		backups = append(backups, backupFile{
			path:    match,
			modTime: fileInfo.ModTime(),
		})
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})

	// Remove excess backup files
	if len(backups) > lr.config.MaxBackups {
		for i := lr.config.MaxBackups; i < len(backups); i++ {
			os.Remove(backups[i].path)
		}
	}

	return nil
}

// ForceRotate forces rotation of the specified log file regardless of size
func (lr *LogRotator) ForceRotate(logFilePath string) error {
	lr.mutex.Lock()
	defer lr.mutex.Unlock()

	// Check if file exists
	if _, err := os.Stat(logFilePath); err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, nothing to rotate
		}
		return fmt.Errorf("failed to stat log file %s: %w", logFilePath, err)
	}

	return lr.rotateFile(logFilePath)
}

// GetBackupFiles returns a list of backup files for the given log file
func (lr *LogRotator) GetBackupFiles(logFilePath string) ([]string, error) {
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	// Find all backup files
	pattern := filepath.Join(dir, fmt.Sprintf("%s-*%s*", name, ext))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to find backup files with pattern %s: %w", pattern, err)
	}

	// Filter out the current log file
	var backups []string
	for _, match := range matches {
		if match != logFilePath {
			backups = append(backups, match)
		}
	}

	return backups, nil
}

// ParseSizeString parses size strings like "10MB", "1GB", "500KB"
func ParseSizeString(sizeStr string) (int64, error) {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	var multiplier int64 = 1
	var numStr string

	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		numStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "B") {
		multiplier = 1
		numStr = strings.TrimSuffix(sizeStr, "B")
	} else {
		// Assume bytes if no suffix
		numStr = sizeStr
	}

	num, err := strconv.ParseInt(strings.TrimSpace(numStr), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size format: %s", sizeStr)
	}

	return num * multiplier, nil
}
