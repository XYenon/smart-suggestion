package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func GetCurrentSessionID() string {
	if sessionID := os.Getenv("SMART_SUGGESTION_SESSION_ID"); sessionID != "" {
		return sessionID
	}

	if ttyName := GetTTYName(); ttyName != "" {
		return ttyName
	}

	return fmt.Sprintf("pid_%d", os.Getpid())
}

var execCommand = exec.Command

func GetTTYName() string {
	if tty := os.Getenv("TTY"); tty != "" {
		if parts := strings.Split(tty, "/"); len(parts) > 0 {
			return strings.ReplaceAll(parts[len(parts)-1], ".", "_")
		}
	}

	cmd := execCommand("tty")
	output, err := cmd.Output()
	if err == nil {
		ttyPath := strings.TrimSpace(string(output))
		if parts := strings.Split(ttyPath, "/"); len(parts) > 0 {
			deviceName := parts[len(parts)-1]
			deviceName = strings.ReplaceAll(deviceName, ".", "_")
			deviceName = strings.ReplaceAll(deviceName, ":", "_")
			return deviceName
		}
	}

	return ""
}

func GetSessionBasedLogFile(baseLogFile, sessionID string) string {
	if sessionID == "" {
		return baseLogFile
	}
	dir := filepath.Dir(baseLogFile)
	base := filepath.Base(baseLogFile)
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return filepath.Join(dir, fmt.Sprintf("%s.%s%s", base, sessionID, ext))
}
