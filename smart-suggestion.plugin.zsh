# shellcheck shell=zsh

: ${SMART_SUGGESTION_CONFIG:="${XDG_CONFIG_HOME:-$HOME/.config}/smart-suggestion/config.zsh"}
if [[ -f "${SMART_SUGGESTION_CONFIG}" ]]; then
    source "${SMART_SUGGESTION_CONFIG}"
fi

# Default key binding
(( ! ${+SMART_SUGGESTION_KEY} )) &&
    typeset -g SMART_SUGGESTION_KEY='^o'

# Configuration options
(( ! ${+SMART_SUGGESTION_SEND_CONTEXT} )) &&
    typeset -g SMART_SUGGESTION_SEND_CONTEXT=true

(( ! ${+SMART_SUGGESTION_DEBUG} )) &&
    typeset -g SMART_SUGGESTION_DEBUG=false

(( ! ${+SMART_SUGGESTION_HISTORY_LINES} )) &&
    typeset -g SMART_SUGGESTION_HISTORY_LINES=10

(( ! ${+SMART_SUGGESTION_SCROLLBACK_LINES} )) &&
    typeset -g SMART_SUGGESTION_SCROLLBACK_LINES=${SMART_SUGGESTION_BUFFER_LINES:-100}

# Proxy mode configuration - now enabled by default
(( ! ${+SMART_SUGGESTION_PROXY_MODE} )) &&
    typeset -g SMART_SUGGESTION_PROXY_MODE=true

# Auto-update configuration
(( ! ${+SMART_SUGGESTION_AUTO_UPDATE} )) &&
    typeset -g SMART_SUGGESTION_AUTO_UPDATE=true

# Update interval configuration in days
(( ! ${+SMART_SUGGESTION_UPDATE_INTERVAL} )) &&
    typeset -g SMART_SUGGESTION_UPDATE_INTERVAL=7

# New option to select AI provider
if [[ -z "$SMART_SUGGESTION_AI_PROVIDER" ]]; then
    if [[ -n "$OPENAI_API_KEY" ]]; then
        typeset -g SMART_SUGGESTION_AI_PROVIDER="openai"
    elif [[ -n "$AZURE_OPENAI_API_KEY" && -n "$AZURE_OPENAI_RESOURCE_NAME" && -n "$AZURE_OPENAI_DEPLOYMENT_NAME" ]]; then
        typeset -g SMART_SUGGESTION_AI_PROVIDER="azure_openai"
    elif [[ -n "$ANTHROPIC_API_KEY" ]]; then
        typeset -g SMART_SUGGESTION_AI_PROVIDER="anthropic"
    elif [[ -n "$GEMINI_API_KEY" ]]; then
        typeset -g SMART_SUGGESTION_AI_PROVIDER="gemini"
    else
        echo "No AI provider selected. Please set either OPENAI_API_KEY, AZURE_OPENAI_API_KEY (with AZURE_OPENAI_RESOURCE_NAME and AZURE_OPENAI_DEPLOYMENT_NAME), ANTHROPIC_API_KEY, or GEMINI_API_KEY."
        return 1
    fi
fi

: ${SMART_SUGGESTION_CACHE_DIR:="${XDG_CACHE_HOME:-$HOME/.cache}/smart-suggestion"}
mkdir -p "$SMART_SUGGESTION_CACHE_DIR"

if [[ "$SMART_SUGGESTION_DEBUG" == 'true' ]]; then
    touch "${SMART_SUGGESTION_CACHE_DIR}/debug.log"
fi

# Detect binary path
if [[ -z "$SMART_SUGGESTION_BINARY" ]]; then
    _smart_suggestion_candidates=(
        "${0:a:h}/smart-suggestion"
        "${SMART_SUGGESTION_CONFIG:h}/smart-suggestion"
    )
    for _smart_suggestion_bin in "${_smart_suggestion_candidates[@]}"; do
        if [[ -f "$_smart_suggestion_bin" ]]; then
            typeset -g SMART_SUGGESTION_BINARY="$_smart_suggestion_bin"
            break
        fi
    done
    unset _smart_suggestion_candidates _smart_suggestion_bin
    if [[ -z "$SMART_SUGGESTION_BINARY" ]]; then
        echo "No available smart-suggestion binary found. Please ensure that it is installed correctly or set SMART_SUGGESTION_BINARY to a valid binary path."
        return 1
    fi
else
    if [[ ! -f "$SMART_SUGGESTION_BINARY" ]]; then
        echo "smart-suggestion binary not found at $SMART_SUGGESTION_BINARY."
        return 1
    fi
fi

function _run_smart_suggestion_proxy() {
    if [[ $- == *i* ]]; then
        exec "$SMART_SUGGESTION_BINARY" proxy --scrollback-lines "$SMART_SUGGESTION_SCROLLBACK_LINES"
    fi
}

# Extract Ghostty scrollback file path from input
function _extract_ghostty_scrollback_file() {
    local input="$1"

    # Match Ghostty temp file: /<tmpdir>/<22-char-base64>/screen.txt
    # Limit prefix to common temp directories: /tmp, /var, /private
    if [[ "$input" =~ (/(tmp|var|private)/[^[:space:]]+/[A-Za-z0-9_-]{22}/screen\.txt) ]]; then
        local file="${match[1]}"
        [[ -f "$file" ]] && echo "$file"
    fi
}

function _fetch_suggestions() {
    local scrollback_file="$1"

    # Source config file and export all variables
    if [[ -f "${SMART_SUGGESTION_CONFIG}" ]]; then
        set -a
        source "${SMART_SUGGESTION_CONFIG}"
        set +a
    fi

    # Prepare debug flag
    local debug_flag=""
    if [[ "$SMART_SUGGESTION_DEBUG" == 'true' ]]; then
        debug_flag="--debug"
    fi

    # Prepare context flag
    local context_flag=""
    if [[ "$SMART_SUGGESTION_SEND_CONTEXT" == 'true' ]]; then
        context_flag="--context"
    fi

    # Capture shell context to avoid spawning interactive shells in Go binary
    local shell_aliases=$(alias 2>/dev/null)
    local shell_history=$(fc -ln -$SMART_SUGGESTION_HISTORY_LINES 2>/dev/null)

    # Prepare scrollback file args (use array for proper argument handling)
    local scrollback_file_args=()
    [[ -n "$scrollback_file" ]] && scrollback_file_args=(--scrollback-file "$scrollback_file")

    # Call the Go binary with proper arguments
    SMART_SUGGESTION_ALIASES="$shell_aliases" \
    SMART_SUGGESTION_HISTORY="$shell_history" \
    "$SMART_SUGGESTION_BINARY" \
        --provider "$SMART_SUGGESTION_AI_PROVIDER" \
        --input "$input" \
        --output - \
        --scrollback-lines "$SMART_SUGGESTION_SCROLLBACK_LINES" \
        "${scrollback_file_args[@]}" \
        $debug_flag \
        $context_flag \
        2> "${SMART_SUGGESTION_CACHE_DIR}/error"

    return $?
}


function _show_loading_animation() {
    local pid=$1
    local interval=0.1
    local animation_chars=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")
    local i=1

    cleanup() {
        kill $pid
        tput -S <<<"bicr ed rc cnorm"
        touch "${SMART_SUGGESTION_CACHE_DIR}/canceled"
    }
    trap cleanup SIGINT

    tput -S <<<"sc civis"
    while kill -0 $pid 2>/dev/null; do
        # Display current animation frame
        zle -R "${animation_chars[i]} Press <Ctrl-c> to cancel"

        # Update index, make sure it starts at 1
        i=$(( (i + 1) % ${#animation_chars[@]} ))

        if [[ $i -eq 0 ]]; then
            i=1
        fi

        sleep $interval
    done

    tput cnorm
    trap - SIGINT
}

function _do_smart_suggestion() {
    ##### Get input
    rm -f "${SMART_SUGGESTION_CACHE_DIR}/canceled"
    rm -f "${SMART_SUGGESTION_CACHE_DIR}/error"

    local scrollback_file=""

    # Ghostty: extract scrollback file path from buffer
    if [[ -n "$GHOSTTY_RESOURCES_DIR" ]]; then
        scrollback_file=$(_extract_ghostty_scrollback_file "$BUFFER")
        if [[ -n "$scrollback_file" ]]; then
            # Remove the file path from buffer
            BUFFER="${BUFFER%$scrollback_file}"
            BUFFER="${BUFFER## }"
        fi
    fi

    local input=$(echo "${BUFFER:0:$CURSOR}" | tr '\n' ';')

    _zsh_autosuggest_clear

    ##### Fetch message
    exec {OUTPUT_FD}< <(_fetch_suggestions "$scrollback_file" & echo $!)
    read pid <&$OUTPUT_FD

    _show_loading_animation $pid
    local response_code=$?

    if [[ "$SMART_SUGGESTION_DEBUG" == 'true' ]]; then
        if command -v jq >/dev/null 2>&1; then
            jq -n --arg date "$(date)" --arg log "Fetched message" --arg input "$input" --arg response_code "$response_code" \
                '{date: $date, log: $log, input: $input, response_code: $response_code}' >> "${SMART_SUGGESTION_CACHE_DIR}/debug.log"
        else
            echo "{\"date\":\"$(date)\",\"log\":\"Fetched message\",\"input\":\"$input\",\"response_code\":\"$response_code\"}" >> "${SMART_SUGGESTION_CACHE_DIR}/debug.log"
        fi
    fi

    if [[ -f "${SMART_SUGGESTION_CACHE_DIR}/canceled" ]]; then
        _zsh_autosuggest_clear
        return 1
    fi

    local message
    read -u $OUTPUT_FD -d '' message || true
    exec {OUTPUT_FD}<&-
    if [[ -z "$message" ]]; then
        _zsh_autosuggest_clear
        echo $(cat "${SMART_SUGGESTION_CACHE_DIR}/error" 2>/dev/null || echo "No suggestion available at this time. Please try again later.")
        return 1
    fi

    ##### Process response

    local first_char=${message:0:1}
    local suggestion=${message:1:${#message}}

    ##### And now, let's actually show the suggestion to the user!

    if [[ "$first_char" == '=' ]]; then
        # Reset user input
        BUFFER=""
        CURSOR=0

        zle -U "$suggestion"
    elif [[ "$first_char" == '+' ]]; then
        _zsh_autosuggest_suggest "$suggestion"
        zle -R
    fi
}

function _check_smart_suggestion_updates() {
    # Check if SMART_SUGGESTION_UPDATE_INTERVAL is a positive integer
    if [[ "$SMART_SUGGESTION_UPDATE_INTERVAL" -le 0 ]]; then
        echo "SMART_SUGGESTION_UPDATE_INTERVAL must be a positive integer. Will be reset to default value."
        SMART_SUGGESTION_UPDATE_INTERVAL=7
    fi

    local update_file="$(dirname $SMART_SUGGESTION_BINARY)/.last_update_check"
    local current_time=$(date +%s)
    local update_interval=$((SMART_SUGGESTION_UPDATE_INTERVAL * 24 * 3600))  # Convert days to seconds

    # Check if we should check for updates
    if [[ -f "$update_file" ]]; then
        local last_check=$(cat "$update_file" 2>/dev/null || echo "0")
        local time_diff=$((current_time - last_check))

        if [[ $time_diff -lt $update_interval ]]; then
            return 0  # Too soon to check again
        fi
    fi

    # Update the last check time
    echo "$current_time" > "$update_file"

    # Check for updates in background
    ("$SMART_SUGGESTION_BINARY" update --check-only 2>/dev/null && \
        echo "Smart Suggestion update available! Run 'smart-suggestion update' to update." || true) &
}

function smart-suggestion() {
    echo "Smart Suggestion is now active. Press $SMART_SUGGESTION_KEY to get suggestions."
    echo ""
    echo "Configurations:"
    echo "    - SMART_SUGGESTION_KEY: Key to press to get suggestions (default: ^o, value: $SMART_SUGGESTION_KEY)."
    echo "    - SMART_SUGGESTION_SEND_CONTEXT: If \`true\`, smart-suggestion will send context information (whoami, shell, pwd, etc.) to the AI model (default: true, value: $SMART_SUGGESTION_SEND_CONTEXT)."
    echo "    - SMART_SUGGESTION_AI_PROVIDER: AI provider to use ('openai', 'azure_openai', 'anthropic', or 'gemini', value: $SMART_SUGGESTION_AI_PROVIDER)."
    echo "    - SMART_SUGGESTION_DEBUG: Enable debug logging (default: false, value: $SMART_SUGGESTION_DEBUG)."
    echo "    - SMART_SUGGESTION_HISTORY_LINES: Number of history lines to send (default: 10, value: $SMART_SUGGESTION_HISTORY_LINES)."
    echo "    - SMART_SUGGESTION_SCROLLBACK_LINES: Number of scrollback lines to send (default: 100, value: $SMART_SUGGESTION_SCROLLBACK_LINES)."
    echo "    - SMART_SUGGESTION_AUTO_UPDATE: Enable automatic update checking (default: true, value: $SMART_SUGGESTION_AUTO_UPDATE)."
    echo "    - SMART_SUGGESTION_UPDATE_INTERVAL: Days between update checks (default: 7, value: $SMART_SUGGESTION_UPDATE_INTERVAL)."
    echo "    - SMART_SUGGESTION_BINARY: Path to the smart-suggestion binary (value: $SMART_SUGGESTION_BINARY)."
}

zle -N _do_smart_suggestion
bindkey "$SMART_SUGGESTION_KEY" _do_smart_suggestion

if [[ -z "$SMART_SUGGESTION_PROXY_ACTIVE" && "$SMART_SUGGESTION_PROXY_MODE" == "true" && -z "$TMUX" && -z "$KITTY_LISTEN_ON" && -z "$GHOSTTY_RESOURCES_DIR" ]]; then
    _run_smart_suggestion_proxy
fi

# Add update check to plugin initialization
if [[ "$SMART_SUGGESTION_AUTO_UPDATE" == "true" ]]; then
    _check_smart_suggestion_updates
fi
