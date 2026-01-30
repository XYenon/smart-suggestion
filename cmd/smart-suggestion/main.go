package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xyenon/smart-suggestion/internal/debug"
	"github.com/xyenon/smart-suggestion/internal/paths"
	"github.com/xyenon/smart-suggestion/internal/provider"
	"github.com/xyenon/smart-suggestion/internal/proxy"
	"github.com/xyenon/smart-suggestion/internal/session"
	"github.com/xyenon/smart-suggestion/internal/shellcontext"
	"github.com/xyenon/smart-suggestion/internal/updater"
	"github.com/xyenon/smart-suggestion/pkg"
)

const defaultSystemPrompt = `You are a professional SRE engineer with decades of experience, proficient in all shell commands.

Your tasks:
    - First, you must reason about the user's intent in <reasoning> tags. This reasoning will not be shown to the user.
        Your reasoning process should follow these steps:
        1. What is the user's real intention behind the recent input context?
        2. Did the last few commands solve the intention? Why or why not?
        3. Based on the latest information, how can you solve the user's intention?
    - After reasoning, you will either complete the command or provide a new command that you think the user is trying to type.
    - You need to predict what command the user wants to input next based on shell history and scrollback.

RULES FOR FINAL OUTPUT (MANDATORY - MUST BE FOLLOWED EXACTLY):
    - YOU MUST start your response with EITHER an equal sign (=) for new commands OR a plus sign (+) for completions. NO EXCEPTIONS!
    - If you return a completely new command that the user didn't start typing, ALWAYS prefix with an equal sign (=). THIS IS CRUCIAL!
    - If you return a completion for the user's partially typed command, ALWAYS prefix with a plus sign (+).
    - MAKE SURE TO ONLY INCLUDE THE REST OF THE COMPLETION AFTER THE PLUS SIGN!
    - NEVER include any leading or trailing characters except the required prefix and command/completion.
    - ONLY respond with either a completion OR a new command, NOT BOTH.
    - YOUR RESPONSE MUST START WITH EITHER = OR + AND NOTHING ELSE!
    - NEVER start with both symbols or any other characters!
    - NO NEWLINES ALLOWED IN YOUR RESPONSE!
    - DO NOT ADD ANY ADDITIONAL TEXT, COMMENTS, OR EXPLANATIONS!
    - YOUR RESPONSE WILL BE DIRECTLY EXECUTED IN THE USER'S SHELL, SO ACCURACY IS CRITICAL.
    - FAILURE TO FOLLOW THESE FORMATTING RULES WILL RESULT IN YOUR RESPONSE BEING REJECTED.

Example of your full response format:
<reasoning>
1. The user wants to see the logs for a pod that is in a CrashLoopBackOff state.
2. The previous command 'kubectl get pods' listed the pods and their statuses, but did not show the logs.
3. The next logical step is to use 'kubectl logs' on the failing pod to diagnose the issue.
</reasoning>
=kubectl -n my-namespace logs pod-name-aaa`

// getExampleHistory returns conversation examples as message history
func getExampleHistory() []provider.Message {
	return []provider.Message{
		// Example 1: New command for listing files
		{Role: "user", Content: "list files in current directory"},
		{Role: "assistant", Content: `<reasoning>
1. The user wants to list files.
2. No previous command.
3. 'ls' is the command for listing files.
</reasoning>
=ls`},

		// Example 2: New command after viewing log file details
		{Role: "user", Content: "Shell history: 'ls -l /tmp/smart-suggestion.log'"},
		{Role: "assistant", Content: `<reasoning>
1. The user just listed details of a log file. A common next step is to view the content of that file.
2. Listing the file does not show its content.
3. The 'cat' command can be used to display the file content.
</reasoning>
=cat /tmp/smart-suggestion.log`},

		// Example 3: New command for CrashLoopBackOff pods
		{Role: "user", Content: `Scrollback:
# k -n my-namespace get pod
NAME           READY   STATUS             RESTARTS         AGE
pod-name-aaa   2/3     CrashLoopBackOff   358 (111s ago)   30h
pod-name-bbb   2/3     CrashLoopBackOff   358 (3m8s ago)   30h`},
		{Role: "assistant", Content: `<reasoning>
1. The user is checking pods in a Kubernetes namespace.
2. The pods are in 'CrashLoopBackOff', indicating a problem. The user likely wants to see the logs to debug.
3. The command 'kubectl logs' will show the logs for 'pod-name-aaa'.
</reasoning>
=kubectl -n my-namespace logs pod-name-aaa`},

		// Example 4: New command for pending pod
		{Role: "user", Content: `Scrollback:
# k -n my-namespace get pod
NAME           READY   STATUS             RESTARTS         AGE
pod-name-aaa   3/3     Running            0                30h
pod-name-bbb   0/3     Pending            0                30h`},
		{Role: "assistant", Content: `<reasoning>
1. The user is checking pods. One pod is 'Pending'.
2. The 'get pod' command doesn't say why it's pending.
3. 'kubectl describe pod' will give more details about why the pod is pending.
</reasoning>
=kubectl -n my-namespace describe pod pod-name-bbb`},

		// Example 5: New command for NotReady node
		{Role: "user", Content: `Scrollback:
# k get node
NAME      STATUS   ROLES    AGE   VERSION
node-aaa  Ready    <none>   3h    v1.25.3
node-bbb  NotReady <none>   3h    v1.25.3`},
		{Role: "assistant", Content: `<reasoning>
1. The user is checking Kubernetes nodes. One node is 'NotReady'.
2. 'get node' does not show the reason for the 'NotReady' status.
3. 'kubectl describe node' will provide detailed events and information about the node's status.
</reasoning>
=kubectl describe node node-bbb`},

		// Example 6: Completion for cd command
		{Role: "user", Content: "cd /tm"},
		{Role: "assistant", Content: `<reasoning>
1. The user wants to change directory to a temporary folder.
2. The user has typed '/tm' which is likely an abbreviation for '/tmp'.
3. Completing with 'p' will form '/tmp'.
</reasoning>
+p`},

		// Example 7: Completion for kubectl command
		{Role: "user", Content: `Scrollback:
# k -n my-namespace get pod
NAME           READY   STATUS             RESTARTS         AGE
pod-name-aaa   3/3     Running            0                30h
pod-name-bbb   0/3     Pending            0                30h
User input: k -n`},
		{Role: "assistant", Content: `<reasoning>
1. The user is checking pods. One pod is 'Pending'. They started typing a command.
2. 'get pod' was useful but now they want to investigate 'pod-name-bbb'.
3. I will complete the command to describe the pending pod.
</reasoning>
+ my-namespace describe pod pod-name-bbb`},
	}
}

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
	OS        = "unknown"
	Arch      = "unknown"
)

var (
	providerName    string
	input           string
	systemPrompt    string
	dbg             bool
	outputFile      string
	sendContext     bool
	proxyLogFile    string
	sessionID       string
	scrollbackLines int
	scrollbackFile  string

	logRotator *pkg.LogRotator
)

var exitFunc = os.Exit
var buildContextInfoFunc = shellcontext.BuildContextInfo
var runProxyFunc = proxy.RunProxy
var checkUpdateFunc = updater.CheckUpdate
var installUpdateFunc = updater.InstallUpdate
var selectProviderFunc = selectProvider

func init() {
	config := pkg.DefaultLogRotateConfig()
	config.MaxSize = 5 * 1024 * 1024 // 5MB
	config.MaxBackups = 3
	config.Compress = true
	config.MaxAge = 7

	logRotator = pkg.NewLogRotator(config)
}

func resolveSystemPrompt() string {
	if systemPrompt == "" {
		return defaultSystemPrompt
	}
	return systemPrompt
}

func buildPrompt(scrollbackLines int, scrollbackFile string, sendContext bool) string {
	basePrompt := resolveSystemPrompt()
	if !sendContext {
		return basePrompt
	}

	contextInfo, err := buildContextInfoFunc(scrollbackLines, scrollbackFile)
	if err != nil {
		debug.Log("Failed to build context info", map[string]any{
			"error": err.Error(),
		})
		return basePrompt
	}

	return basePrompt + "\n\n" + contextInfo
}

func selectProvider(ctx *cobra.Command) (provider.Provider, error) {
	switch strings.ToLower(providerName) {
	case "openai":
		return provider.NewOpenAIProvider()
	case "azure_openai":
		return provider.NewAzureOpenAIProvider()
	case "anthropic":
		return provider.NewAnthropicProvider()
	case "gemini":
		return provider.NewGeminiProvider(ctx.Context())
	default:
		return nil, fmt.Errorf("unsupported provider: %s (valid: openai, azure_openai, anthropic, gemini)", providerName)
	}
}

func writeSuggestion(outputFile string, suggestion string) error {
	if outputFile == "-" || outputFile == "/dev/stdout" {
		_, err := fmt.Fprint(os.Stdout, suggestion)
		if err != nil {
			return fmt.Errorf("failed to write suggestion to stdout: %w", err)
		}
		return nil
	}

	if err := os.WriteFile(outputFile, []byte(suggestion), 0644); err != nil {
		return fmt.Errorf("failed to write suggestion to file: %w", err)
	}
	return nil
}

func buildRootCmd() *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "smart-suggestion",
		Short: "AI-powered smart suggestions for shell commands",
		RunE:  runSuggest,
	}

	rootCmd.Flags().StringVarP(&providerName, "provider", "p", "", "AI provider (openai, azure_openai, anthropic, gemini)")
	rootCmd.Flags().StringVarP(&input, "input", "i", "", "User input")
	rootCmd.Flags().StringVarP(&systemPrompt, "system", "s", "", "System prompt (optional, uses default if not provided)")
	rootCmd.Flags().BoolVarP(&dbg, "debug", "d", false, "Enable debug logging")
	rootCmd.Flags().StringVarP(&outputFile, "output", "o", "-", "Output file path")
	rootCmd.Flags().BoolVarP(&sendContext, "context", "c", false, "Include context information")
	rootCmd.Flags().IntVar(&scrollbackLines, "scrollback-lines", 100, "Number of scrollback lines to send")
	rootCmd.Flags().StringVar(&scrollbackFile, "scrollback-file", "", "Path to scrollback file (Ghostty integration)")

	var proxyCmd = &cobra.Command{
		Use:   "proxy",
		Short: "Start shell proxy mode to record commands and output",
		Run:   runProxy,
	}
	proxyCmd.Flags().StringVarP(&proxyLogFile, "log-file", "l", paths.GetDefaultProxyLogFile(), "Proxy log file path")
	proxyCmd.Flags().StringVarP(&sessionID, "session-id", "", "", "Session ID for log isolation (auto-generated if not provided)")
	proxyCmd.Flags().BoolVarP(&dbg, "debug", "d", false, "Enable debug logging")
	proxyCmd.Flags().IntVar(&scrollbackLines, "scrollback-lines", 100, "Number of scrollback lines to keep in log")

	var rotateCmd = &cobra.Command{
		Use:   "rotate-logs",
		Short: "Rotate log files to prevent them from growing too large",
		RunE:  runRotateLogs,
	}
	rotateCmd.Flags().StringVarP(&proxyLogFile, "log-file", "l", "", "Log file path to rotate (required)")
	rotateCmd.Flags().BoolVarP(&dbg, "debug", "d", false, "Enable debug logging")
	rotateCmd.MarkFlagRequired("log-file")

	var updateCmd = &cobra.Command{
		Use:   "update",
		Short: "Update smart-suggestion to the latest version",
		Run:   runUpdate,
	}
	updateCmd.Flags().BoolP("check-only", "c", false, "Only check for updates, don't install")

	var versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Smart Suggestion %s\n", Version)
			fmt.Printf("Build Time: %s\n", BuildTime)
			fmt.Printf("Git Commit: %s\n", GitCommit)
			fmt.Printf("OS: %s\n", OS)
			fmt.Printf("Arch: %s\n", Arch)
		},
	}

	rootCmd.AddCommand(proxyCmd, rotateCmd, updateCmd, versionCmd)

	return rootCmd
}

func main() {
	rootCmd := buildRootCmd()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		exitFunc(1)
	}
}

func runSuggest(cmd *cobra.Command, args []string) error {
	debug.Enable(dbg)

	if providerName == "" {
		return fmt.Errorf("required flag \"provider\" not set")
	}
	if input == "" {
		return fmt.Errorf("required flag \"input\" not set")
	}

	completePrompt := buildPrompt(scrollbackLines, scrollbackFile, sendContext)
	providerClient, err := selectProviderFunc(cmd)

	if err != nil {
		debug.Log("Error occurred", map[string]any{
			"error":    err.Error(),
			"provider": providerName,
			"input":    input,
		})

		return fmt.Errorf("error fetching suggestions from %s API: %w", providerName, err)
	}

	suggestion, err := providerClient.FetchWithHistory(cmd.Context(), input, completePrompt, getExampleHistory())
	if err != nil {
		debug.Log("Error occurred", map[string]any{
			"error":    err.Error(),
			"provider": providerName,
			"input":    input,
		})

		return fmt.Errorf("error fetching suggestions from %s API: %w", providerName, err)
	}

	finalSuggestion := provider.ParseAndExtractCommand(suggestion)

	debug.Log("Successfully fetched suggestion", map[string]any{
		"provider":          providerName,
		"input":             input,
		"original_response": suggestion,
		"parsed_suggestion": finalSuggestion,
	})

	if err := writeSuggestion(outputFile, finalSuggestion); err != nil {
		return err
	}
	return nil
}

func runProxy(cmd *cobra.Command, args []string) {
	debug.Enable(dbg)

	sessID := sessionID
	if sessID == "" {
		sessID = session.GetCurrentSessionID()
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	logFile := proxyLogFile
	if logFile == "" {
		logFile = paths.GetDefaultProxyLogFile()
	}

	err := runProxyFunc(shell, proxy.ProxyOptions{
		LogFile:         logFile,
		SessionID:       sessID,
		ScrollbackLines: scrollbackLines,
	})
	if err != nil {
		fmt.Printf("Proxy error: %v\n", err)
	}
}

func runRotateLogs(cmd *cobra.Command, args []string) error {
	debug.Enable(dbg)

	debug.Log("Rotating log file", map[string]any{
		"log_file": proxyLogFile,
	})

	if err := logRotator.ForceRotate(proxyLogFile); err != nil {
		return fmt.Errorf("failed to rotate log file: %w", err)
	}

	fmt.Printf("Successfully rotated log file: %s\n", proxyLogFile)
	return nil
}

func runUpdate(cmd *cobra.Command, args []string) {
	checkOnly, _ := cmd.Flags().GetBool("check-only")
	fmt.Println("Checking for updates...")
	latest, url, err := checkUpdateFunc(Version)
	if err != nil {
		fmt.Printf("Check failed: %v\n", err)
		return
	}
	if url == "" {
		fmt.Println("Smart Suggestion is already up to date!")
		if checkOnly {
			exitFunc(0)
		}
		return
	}
	if checkOnly {
		fmt.Printf("New version %s available.\n", latest)
		exitFunc(1)
		return
	}
	fmt.Printf("New version %s available. Installing...\n", latest)
	if err := installUpdateFunc(url); err != nil {
		fmt.Printf("Install failed: %v\n", err)
	} else {
		fmt.Println("Successfully updated!")
	}
}
