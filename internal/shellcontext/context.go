package shellcontext

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/yetone/smart-suggestion/internal/debug"
	"github.com/yetone/smart-suggestion/internal/paths"
	"github.com/yetone/smart-suggestion/internal/session"
)

var execCommand = exec.Command

func BuildContextInfo() (string, error) {
	var parts []string

	currentUser := os.Getenv("USER")
	if currentUser == "" {
		currentUser = "unknown"
	}

	currentDir, err := os.Getwd()
	if err != nil {
		currentDir = "unknown"
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "unknown"
	}

	term := os.Getenv("TERM")
	if term == "" {
		term = "unknown"
	}

	sysInfo := getSystemInfo()
	userID := getUserID()
	unameInfo := getUnameInfo()

	parts = append(parts, fmt.Sprintf("# Context:\nYou are user %s with id %s in directory %s. Your shell is %s and your terminal is %s running on %s. %s",
		currentUser, userID, currentDir, shell, term, unameInfo, sysInfo))

	if aliases, err := getAliases(); err == nil && aliases != "" {
		parts = append(parts, "\n# This is the alias defined in your shell:\n", aliases)
	} else if err != nil {
		debug.Log("Failed to get aliases", map[string]any{"error": err.Error()})
	}

	if history, err := getHistory(); err == nil && history != "" {
		parts = append(parts, "\n# Shell history:\n", history)
	} else if err != nil {
		debug.Log("Failed to get history", map[string]any{"error": err.Error()})
	}

	if buffer, err := getShellBuffer(); err == nil && buffer != "" {
		parts = append(parts, "\n# Shell buffer:\n", buffer)
	} else if err != nil {
		debug.Log("Failed to get shell buffer", map[string]any{"error": err.Error()})
	}

	return strings.Join(parts, ""), nil
}

func getSystemInfo() string {
	if runtime.GOOS == "darwin" {
		out, err := execCommand("sw_vers").Output()
		if err != nil {
			return "Your system is macOS."
		}
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		var processed []string
		for _, line := range lines {
			processed = append(processed, strings.ReplaceAll(line, " ", "."))
		}
		return fmt.Sprintf("Your system is %s.", strings.Join(processed, "."))
	}

	releaseFiles := []string{"/etc/os-release", "/etc/lsb-release", "/etc/redhat-release"}
	var content []string

	for _, file := range releaseFiles {
		data, err := os.ReadFile(file)
		if err == nil {
			content = append(content, string(data))
		}
	}

	if len(content) == 0 {
		return "Your system is Linux."
	}

	allContent := strings.Join(content, " ")
	processedContent := strings.ReplaceAll(strings.TrimSpace(allContent), " ", ",")
	return fmt.Sprintf("Your system is %s.", processedContent)
}

func getUserID() string {
	out, err := execCommand("id").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func getUnameInfo() string {
	out, err := execCommand("uname", "-a").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func getAliases() (string, error) {
	aliases := os.Getenv("SMART_SUGGESTION_ALIASES")
	if aliases != "" {
		return strings.TrimSpace(aliases), nil
	}
	return "", nil
}

func getHistory() (string, error) {
	history := os.Getenv("SMART_SUGGESTION_HISTORY")
	if history != "" {
		return strings.TrimSpace(history), nil
	}
	return "", nil
}

func getShellBuffer() (string, error) {
	content, err := doGetShellBuffer()
	if err != nil {
		return "", err
	}
	return readLatestLines(content, 100)
}

func doGetShellBuffer() (string, error) {
	defaultProxyLogFile := paths.GetDefaultProxyLogFile()

	if os.Getenv("TMUX") != "" {
		cmd := execCommand("tmux", "capture-pane", "-pS", "-")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output)), nil
		}
		debug.Log("Failed to get tmux buffer", map[string]any{"error": err.Error()})
	}

	if os.Getenv("KITTY_LISTEN_ON") != "" {
		cmd := execCommand("kitten", "@", "get-text", "--extent", "all")
		output, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(output)), nil
		}
		debug.Log("Failed to get kitty scrollback buffer", map[string]any{"error": err.Error()})
	}

	currentSessionID := session.GetCurrentSessionID()
	if currentSessionID != "" {
		sessionLogFile := session.GetSessionBasedLogFile(defaultProxyLogFile, currentSessionID)
		content, err := readLatestProxyContent(sessionLogFile)
		if err == nil {
			return content, nil
		}
		debug.Log("Failed to read session proxy log", map[string]any{
			"error":      err.Error(),
			"file":       sessionLogFile,
			"session_id": currentSessionID,
		})
	}

	content, err := readLatestProxyContent(defaultProxyLogFile)
	if err == nil {
		return content, nil
	}
	debug.Log("Failed to read base proxy log", map[string]any{
		"error": err.Error(),
		"file":  defaultProxyLogFile,
	})

	content, err = getScreenBuffer()
	if err == nil {
		return content, nil
	}

	content, err = getTerminalBufferWithTput()
	if err == nil {
		return content, nil
	}

	return "", fmt.Errorf("no terminal buffer available - not in tmux/screen session and no proxy log found")
}

func readLatestLines(content string, maxLines int) (string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n"), nil
}

func readLatestProxyContent(logFile string) (string, error) {
	file, err := os.Open(logFile)
	if err != nil {
		return "", fmt.Errorf("failed to open proxy log file: %w", err)
	}
	defer file.Close()

	const maxLines = 50
	scanner := bufio.NewScanner(file)
	var lines []string

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxLines {
			lines = lines[1:]
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read proxy log file: %w", err)
	}

	return strings.Join(lines, "\n"), nil
}

func getScreenBuffer() (string, error) {
	if os.Getenv("STY") == "" {
		return "", fmt.Errorf("not in a screen session")
	}

	screenBufferFile := filepath.Join(os.TempDir(), "screen_buffer.txt")
	cmd := execCommand("screen", "-X", "hardcopy", screenBufferFile)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to capture screen buffer: %w", err)
	}

	content, err := os.ReadFile(screenBufferFile)
	if err != nil {
		return "", fmt.Errorf("failed to read screen buffer: %w", err)
	}

	os.Remove(screenBufferFile)

	return strings.TrimSpace(string(content)), nil
}

func getTerminalBufferWithTput() (string, error) {
	rowsCmd := execCommand("tput", "lines")
	rowsOutput, err := rowsCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get terminal rows: %w", err)
	}

	rows, err := strconv.Atoi(strings.TrimSpace(string(rowsOutput)))
	if err != nil {
		return "", fmt.Errorf("failed to parse terminal rows: %w", err)
	}

	return "", fmt.Errorf("tput method not fully implemented (terminal has %d rows)", rows)
}
