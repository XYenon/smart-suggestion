package session

import (
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGetCurrentSessionID(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_SESSION_ID", "test-session")
	if got := GetCurrentSessionID(); got != "test-session" {
		t.Errorf("expected %q, got %q", "test-session", got)
	}
}

func TestGetTTYName_Exec(t *testing.T) {
	t.Setenv("TTY", "") // Ensure env var doesn't take precedence

	oldExecCommand := execCommand
	defer func() { execCommand = oldExecCommand }()

	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("echo", "/dev/pts/mock")
	}

	if got := GetTTYName(); got != "mock" {
		t.Errorf("expected mock, got %q", got)
	}
}

func TestGetCurrentSessionID_FromTTY(t *testing.T) {
	t.Setenv("SMART_SUGGESTION_SESSION_ID", "")
	t.Setenv("TTY", "/dev/pts/123")
	if got := GetCurrentSessionID(); got != "123" {
		t.Errorf("expected %q, got %q", "123", got)
	}
}

func TestGetTTYName(t *testing.T) {
	t.Setenv("TTY", "/dev/pts/0")
	if got := GetTTYName(); got != "0" {
		t.Errorf("expected %q, got %q", "0", got)
	}
}

func TestGetTTYName_Complex(t *testing.T) {
	t.Setenv("TTY", "/dev/tty.usbmodem123")
	if got := GetTTYName(); got != "tty_usbmodem123" {
		t.Errorf("expected %q, got %q", "tty_usbmodem123", got)
	}
}

func TestGetTTYName_Command(t *testing.T) {
	t.Setenv("TTY", "")
	got := GetTTYName()
	// We don't know what it will return, but it should hit the command path
	t.Logf("GetTTYName returned %q", got)
}

func TestGetSessionBasedLogFile(t *testing.T) {
	cases := []struct {
		name      string
		base      string
		sessionID string
		expected  string
	}{
		{
			name:      "simple",
			base:      filepath.Join("tmp", "log.txt"),
			sessionID: "123",
			expected:  filepath.Join("tmp", "log.123.txt"),
		},
		{
			name:      "no extension",
			base:      filepath.Join("tmp", "log"),
			sessionID: "abc",
			expected:  filepath.Join("tmp", "log.abc"),
		},
		{
			name:      "empty session",
			base:      filepath.Join("tmp", "log.txt"),
			sessionID: "",
			expected:  filepath.Join("tmp", "log.txt"),
		},
		{
			name:      "multiple extensions",
			base:      filepath.Join("tmp", "log.tar.gz"),
			sessionID: "123",
			expected:  filepath.Join("tmp", "log.tar.123.gz"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetSessionBasedLogFile(tc.base, tc.sessionID)
			if got != tc.expected {
				// Path separators might differ on OS, but we used forward slashes in test cases.
				// filepath.Join handles OS separators.
				// Let's normalize expected for the OS if needed, but for simple paths it should be fine.
				// Actually, we should construct expected using filepath.Join for robustness.
				t.Errorf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
