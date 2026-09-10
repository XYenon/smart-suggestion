package pkg

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

	return lr.withLockedLog(logFilePath, func() error {
		fileInfo, err := os.Stat(logFilePath)
		if err != nil {
			return fmt.Errorf("failed to stat log file %s: %w", logFilePath, err)
		}
		if fileInfo.Size() < lr.config.MaxSize {
			return nil
		}
		return lr.rotateFile(logFilePath)
	})
}

// rotateFile performs the actual file rotation
func (lr *LogRotator) rotateFile(logFilePath string) error {
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	backupPath, err := uniqueBackupPath(dir, name, ext)
	if err != nil {
		return err
	}
	defer os.Remove(backupReservationPath(backupPath))

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

// compressFile compresses the source file to the destination using gzip.
// The destination is replaced only after a complete gzip stream is written,
// so a failed compress cannot leave a partial .gz that cleanup would treat
// as a real backup.
func (lr *LogRotator) compressFile(srcPath, dstPath string) (err error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open source file %s: %w", srcPath, err)
	}
	defer src.Close()
	return lr.compressReader(src, dstPath)
}

func (lr *LogRotator) compressReader(src io.Reader, dstPath string) (err error) {
	// Create the temp file in the destination directory so the final rename
	// stays on the same filesystem and cannot fail with EXDEV.
	tmpFile, err := os.CreateTemp(filepath.Dir(dstPath), filepath.Base(dstPath)+".tmp.")
	if err != nil {
		return fmt.Errorf("failed to create temporary compressed file for %s: %w", dstPath, err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	gzipWriter := gzip.NewWriter(tmpFile)
	if _, err = io.Copy(gzipWriter, src); err != nil {
		_ = gzipWriter.Close()
		return fmt.Errorf("failed to compress file: %w", err)
	}
	if err = gzipWriter.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer for %s: %w", dstPath, err)
	}
	if err = tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary compressed file %s: %w", tmpPath, err)
	}
	if err = os.Rename(tmpPath, dstPath); err != nil {
		return fmt.Errorf("failed to finalize compressed file %s: %w", dstPath, err)
	}
	return nil
}

func backupReservationPath(backupPath string) string {
	return backupPath + ".reserving"
}

func logRotateLockPath(logFilePath string) string {
	return logFilePath + ".rotate.lock"
}

func (lr *LogRotator) withLockedLog(logFilePath string, fn func() error) error {
	missing, err := logMissing(logFilePath)
	if err != nil || missing {
		return err
	}
	return WithLogRotateLock(logFilePath, func() error {
		missing, err := logMissing(logFilePath)
		if err != nil || missing {
			return err
		}
		return fn()
	})
}

func logMissing(logFilePath string) (bool, error) {
	exists, err := fileExists(logFilePath)
	if err != nil {
		return false, fmt.Errorf("failed to stat log file %s: %w", logFilePath, err)
	}
	return !exists, nil
}

// WithLogRotateLock takes an exclusive per-log lock for rotate → compress →
// cleanup and for live writers that truncate/rewrite the file.
func WithLogRotateLock(logFilePath string, fn func() error) error {
	return withLogLock(logFilePath, syscall.LOCK_EX, fn)
}

// WithLogReadLock takes a shared per-log lock so readers see a complete
// snapshot instead of a file that is mid-truncate or mid-rewrite.
func WithLogReadLock(logFilePath string, fn func() error) error {
	return withLogLock(logFilePath, syscall.LOCK_SH, fn)
}

func withLogLock(logFilePath string, how int, fn func() error) error {
	lockPath := logRotateLockPath(logFilePath)
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open rotation lock %s: %w", lockPath, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		return fmt.Errorf("failed to acquire rotation lock %s: %w", lockPath, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return fn()
}

func uniqueBackupPath(dir, name, ext string) (string, error) {
	return uniqueBackupPathAt(dir, name, ext, time.Now())
}

func uniqueBackupPathAt(dir, name, ext string, now time.Time) (string, error) {
	timestamp := now.Format("20060102-150405")
	for i := 0; ; i++ {
		suffix := timestamp
		if i > 0 {
			suffix = fmt.Sprintf("%s-%d", timestamp, i)
		}
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%s%s", name, suffix, ext))
		reservation := backupReservationPath(candidate)

		// Claim the candidate with a sidecar. Using the final backup path
		// itself would make findBackupFiles/cleanupOldBackups treat a
		// zero-byte reservation as a real backup.
		f, err := os.OpenFile(reservation, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", fmt.Errorf("failed to reserve backup name %s: %w", candidate, err)
		}
		_ = f.Close()

		inUse, err := backupNameInUse(candidate)
		if err != nil || inUse {
			_ = os.Remove(reservation)
			if err != nil {
				return "", err
			}
			continue
		}
		return candidate, nil
	}
}

func preferCompressedBackup(paths []string) string {
	for _, path := range paths {
		if strings.HasSuffix(path, ".gz") {
			return path
		}
	}
	return paths[0]
}

func keepOneBackup(paths []string) string {
	kept := preferCompressedBackup(paths)
	for _, path := range paths {
		if path != kept {
			os.Remove(path)
		}
	}
	return kept
}

func backupNameInUse(candidate string) (bool, error) {
	inUse, err := fileExists(candidate)
	if err != nil || inUse {
		return inUse, err
	}
	return fileExists(candidate + ".gz")
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func backupFilePattern(name, ext string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `-([0-9]{8}-[0-9]{6})(?:-([1-9][0-9]*))?` + regexp.QuoteMeta(ext) + `(?:\.gz)?$`)
}

func rotationTempFilePattern(name, ext string) *regexp.Regexp {
	return regexp.MustCompile(`^` + regexp.QuoteMeta(name) + `-[0-9]{8}-[0-9]{6}(?:-[1-9][0-9]*)?` + regexp.QuoteMeta(ext) + `(?:\.reserving|\.gz\.tmp\..+)$`)
}

func parseBackupStamp(filename, name, ext string) (stamp time.Time, seq int, ok bool) {
	m := backupFilePattern(name, ext).FindStringSubmatch(filename)
	if m == nil {
		return time.Time{}, 0, false
	}
	stamp, err := time.ParseInLocation("20060102-150405", m[1], time.Local)
	if err != nil {
		return time.Time{}, 0, false
	}
	if m[2] != "" {
		seq, err = strconv.Atoi(m[2])
		if err != nil {
			return time.Time{}, 0, false
		}
	}
	return stamp, seq, true
}

func findBackupFiles(dir, name, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read backup directory %s: %w", dir, err)
	}

	pattern := backupFilePattern(name, ext)
	backups := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() && pattern.MatchString(entry.Name()) {
			backups = append(backups, filepath.Join(dir, entry.Name()))
		}
	}
	return backups, nil
}

// cleanupStaleRotationFiles removes leftover reservation and gzip temp files.
// It must only run while holding the per-log rotation lock, so matching files
// belong to a previous crashed rotation of this log, not a live one.
func cleanupStaleRotationFiles(dir, name, ext string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	pattern := rotationTempFilePattern(name, ext)
	for _, entry := range entries {
		if entry.IsDir() || !pattern.MatchString(entry.Name()) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// cleanupOldBackups removes old backup files based on MaxBackups and MaxAge settings
func (lr *LogRotator) cleanupOldBackups(logFilePath string) error {
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	cleanupStaleRotationFiles(dir, name, ext)

	matches, err := findBackupFiles(dir, name, ext)
	if err != nil {
		return fmt.Errorf("failed to find backup files for %s: %w", logFilePath, err)
	}

	type backupKey struct {
		when time.Time
		seq  int
	}
	type backupFile struct {
		path string
		when time.Time
		seq  int
	}

	grouped := make(map[backupKey][]string)
	cutoffTime := time.Now().AddDate(0, 0, -lr.config.MaxAge)

	for _, match := range matches {
		if match == logFilePath {
			continue
		}

		stamp, seq, ok := parseBackupStamp(filepath.Base(match), name, ext)
		if !ok {
			continue
		}

		if stamp.Before(cutoffTime) {
			os.Remove(match)
			continue
		}

		key := backupKey{when: stamp, seq: seq}
		grouped[key] = append(grouped[key], match)
	}

	var backups []backupFile
	for key, paths := range grouped {
		backups = append(backups, backupFile{
			path: keepOneBackup(paths),
			when: key.when,
			seq:  key.seq,
		})
	}

	// Newest rotation first. Same-second backups use the numeric suffix.
	sort.Slice(backups, func(i, j int) bool {
		if backups[i].when.Equal(backups[j].when) {
			return backups[i].seq > backups[j].seq
		}
		return backups[i].when.After(backups[j].when)
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
	return lr.withLockedLog(logFilePath, func() error {
		return lr.rotateFile(logFilePath)
	})
}

// GetBackupFiles returns a list of backup files for the given log file
func (lr *LogRotator) GetBackupFiles(logFilePath string) ([]string, error) {
	dir := filepath.Dir(logFilePath)
	base := filepath.Base(logFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)

	matches, err := findBackupFiles(dir, name, ext)
	if err != nil {
		return nil, fmt.Errorf("failed to find backup files for %s: %w", logFilePath, err)
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
