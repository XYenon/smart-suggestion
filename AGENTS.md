# Project Overview

`smart-suggestion` is a Zsh plugin that provides AI-powered command suggestions. It integrates directly into the shell to predict the next command or complete the current one based on rich context, including shell history, aliases, and terminal output (via a proxy mode).

## Technology Stack

- **Core Logic**: Go (1.24+)
- **Shell Integration**: Zsh script (`smart-suggestion.plugin.zsh`)
- **AI Providers**: Support for OpenAI, Anthropic (Claude), Google Gemini, Azure OpenAI.
- **CLI Framework**: `cobra`
- **Terminal Interaction**: `creack/pty` for proxy mode.

# Architecture

The project consists of three main components:

1.  **Zsh Plugin (`smart-suggestion.plugin.zsh`)**:
    - Handles user interaction (keybindings, display).
    - Manages configuration and environment variables.
    - Invokes the Go binary to fetch suggestions.
    - Handles the result (replacing buffer or showing autosuggestion).

2.  **Go Binary (`cmd/smart-suggestion/main.go`)**:
    - **`suggest` (default)**: The core command.
        - Gathers context (if not provided via args).
        - Communicates with AI providers.
        - Parses AI response based on strict rules (`=` for new command, `+` for completion).
    - **`proxy`**: Runs a shell session wrapped in a PTY to capture stdout/stderr. This allows the AI to "see" what happened in the terminal (e.g., error messages).
    - **`update`**: Self-update mechanism.
    - **`rotate-logs`**: Manages log file sizes.

3.  **AI Integration**:
    - The system prompt enforces a strict protocol for responses to ensure they can be safely executed or displayed by the shell.

## Context Acquisition

To provide relevant suggestions, the tool gathers context from the user's shell environment. The **Shell Buffer** (what is currently visible on screen) is acquired using the following priority strategies:

1.  **Tmux**: Checks for `TMUX` env var. Uses `tmux capture-pane -pS -`.
2.  **Kitty**: Checks for `KITTY_LISTEN_ON` env var. Uses `kitten @ get-text --extent all`.
3.  **Session Proxy Log**: If running in the tool's own proxy mode (with a session ID), reads from the session-specific log file.
4.  **Default Proxy Log**: Reads from the global proxy log file.
5.  **GNU Screen**: Checks for `STY` env var. Uses `screen -X hardcopy`.

It also captures:
- **Shell History**: Passed via environment variable `SMART_SUGGESTION_HISTORY`.
- **Aliases**: Passed via environment variable `SMART_SUGGESTION_ALIASES`.
- **System Info**: OS, User, CWD, Shell, Terminal type.

## Data Flow

1.  User triggers suggestion (default `Ctrl+O`).
2.  Zsh plugin captures current buffer, cursor position.
3.  Zsh plugin collects history and aliases.
4.  Zsh plugin invokes `smart-suggestion --provider ... --input ...`.
5.  Go binary constructs a prompt including the system instructions and collected context.
6.  AI Provider returns a prediction.
7.  Go binary parses the response (`=` or `+`).
8.  Zsh plugin applies the suggestion to the user's terminal.

# Directory Structure

- `cmd/smart-suggestion/`: Main entry point for the Go binary.
- `internal/`: Private application code.
    - `provider/`: AI provider implementations (OpenAI, Anthropic, etc.).
    - `proxy/`: Terminal proxy logic using PTY.
    - `shellcontext/`: Logic to gather shell history, aliases, and system info.
    - `updater/`: Version checking and self-update logic.
    - `debug/`: Debug logging utilities.
- `pkg/`: Public library code (e.g., `logrotate`).
- `smart-suggestion.plugin.zsh`: The Zsh plugin script.
- `build.sh`: Script to build the Go binary.
- `install.sh`: User-facing installation script.

# Build and Test

## Build

To build the binary manually:

```bash
./build.sh
# Output binary: ./smart-suggestion
```

Or using Go directly:

```bash
go build -o smart-suggestion ./cmd/smart-suggestion/main.go
```

## Test

Standard Go testing is used. To run all tests:

```bash
go test ./...
```

# Configuration

Configuration is handled via `~/.config/smart-suggestion/config.zsh` or environment variables.

Key variables:
- `SMART_SUGGESTION_AI_PROVIDER`: `openai`, `anthropic`, `gemini`, `azure_openai`.
- `SMART_SUGGESTION_KEY`: Trigger key (default `^o`).
- `SMART_SUGGESTION_PROXY_MODE`: `true`/`false`. Enables the PTY wrapper for better context.
- `SMART_SUGGESTION_DEBUG`: Enable debug logging to `~/.cache/smart-suggestion/debug.log`.

# Development Conventions

- **System Prompt**: The system prompt in `main.go` is critical. It defines the contract between the AI and the shell. Any changes to the AI logic must ensure this contract (`=`/`+` prefixes) is maintained.
- **Error Handling**: Errors in the binary are printed to stderr. The Zsh plugin captures stderr to show user-friendly messages.
- **Dependencies**: Use `go mod` for dependency management.
