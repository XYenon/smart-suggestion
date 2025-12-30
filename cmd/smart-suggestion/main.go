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
=kubectl -n my-namespace logs pod-name-aaa

IMPORTANT EXAMPLES OF NEW COMMANDS (MUST USE =):
    * User input: 'list files in current directory';
      Your response:
<reasoning>
1. The user wants to list files.
2. No previous command.
3. 'ls' is the command for listing files.
</reasoning>
=ls
    * Shell history: 'ls -l /tmp/smart-suggestion.log';
      Your response:
<reasoning>
1. The user just listed details of a log file. A common next step is to view the content of that file.
2. Listing the file does not show its content.
3. The 'cat' command can be used to display the file content.
</reasoning>
=cat /tmp/smart-suggestion.log
    * Scrollback:
        # k -n my-namespace get pod
        NAME           READY   STATUS             RESTARTS         AGE
        pod-name-aaa   2/3     CrashLoopBackOff   358 (111s ago)   30h
        pod-name-bbb   2/3     CrashLoopBackOff   358 (3m8s ago)   30h
      Your response:
<reasoning>
1. The user is checking pods in a Kubernetes namespace.
2. The pods are in 'CrashLoopBackOff', indicating a problem. The user likely wants to see the logs to debug.
3. The command 'kubectl logs' will show the logs for 'pod-name-aaa'.
</reasoning>
=kubectl -n my-namespace logs pod-name-aaa
    * Scrollback:
        # k -n my-namespace get pod
        NAME           READY   STATUS             RESTARTS         AGE
        pod-name-aaa   3/3     Running            0                30h
        pod-name-bbb   0/3     Pending            0                30h
      Your response:
<reasoning>
1. The user is checking pods. One pod is 'Pending'.
2. The 'get pod' command doesn't say why it's pending.
3. 'kubectl describe pod' will give more details about why the pod is pending.
</reasoning>
=kubectl -n my-namespace describe pod pod-name-bbb
    * Scrollback:
        # k get node
        NAME      STATUS   ROLES    AGE   VERSION
        node-aaa  Ready    <none>   3h    v1.25.3
        node-bbb  NotReady <none>   3h    v1.25.3
      Your response:
<reasoning>
1. The user is checking Kubernetes nodes. One node is 'NotReady'.
2. 'get node' does not show the reason for the 'NotReady' status.
3. 'kubectl describe node' will provide detailed events and information about the node's status.
</reasoning>
=kubectl describe node node-bbb

IMPORTANT EXAMPLES OF COMPLETIONS (MUST USE +):
    * User input: 'cd /tm';
      Your response:
<reasoning>
1. The user wants to change directory to a temporary folder.
2. The user has typed '/tm' which is likely an abbreviation for '/tmp'.
3. Completing with 'p' will form '/tmp'.
</reasoning>
+p
    * Scrollback:
        # k -n my-namespace get pod
        NAME           READY   STATUS             RESTARTS         AGE
        pod-name-aaa   3/3     Running            0                30h
        pod-name-bbb   0/3     Pending            0                30h
	  User input: 'k -n'
      Your response:
<reasoning>
1. The user is checking pods. One pod is 'Pending'. They started typing a command.
2. 'get pod' was useful but now they want to investigate 'pod-name-bbb'.
3. I will complete the command to describe the pending pod.
</reasoning>
+ my-namespace describe pod pod-name-bbb`

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

func init() {
	config := pkg.DefaultLogRotateConfig()
	config.MaxSize = 5 * 1024 * 1024 // 5MB
	config.MaxBackups = 3
	config.Compress = true
	config.MaxAge = 7

	logRotator = pkg.NewLogRotator(config)
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "smart-suggestion",
		Short: "AI-powered smart suggestions for shell commands",
		Run:   runSuggest,
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
		Run:   runRotateLogs,
	}
	rotateCmd.Flags().StringVarP(&proxyLogFile, "log-file", "l", paths.GetDefaultProxyLogFile(), "Log file path to rotate (required)")
	rotateCmd.Flags().BoolVarP(&dbg, "debug", "d", false, "Enable debug logging")

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

	if len(os.Args) > 1 && os.Args[1] != "proxy" && os.Args[1] != "rotate-logs" && os.Args[1] != "version" && os.Args[1] != "update" {
		rootCmd.MarkFlagRequired("provider")
		rootCmd.MarkFlagRequired("input")
	}

	if len(os.Args) > 1 && os.Args[1] == "rotate-logs" {
		rotateCmd.MarkFlagRequired("log-file")
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runSuggest(cmd *cobra.Command, args []string) {
	debug.Enable(dbg)

	if systemPrompt == "" {
		systemPrompt = defaultSystemPrompt
	}

	completePrompt := systemPrompt
	if sendContext {
		contextInfo, err := shellcontext.BuildContextInfo(scrollbackLines, scrollbackFile)
		if err != nil {
			debug.Log("Failed to build context info", map[string]any{
				"error": err.Error(),
			})
		} else {
			completePrompt = systemPrompt + "\n\n" + contextInfo
		}
	}

	var p provider.Provider
	var err error

	switch strings.ToLower(providerName) {
	case "openai":
		p, err = provider.NewOpenAIProvider()
	case "azure_openai":
		p, err = provider.NewAzureOpenAIProvider()
	case "anthropic":
		p, err = provider.NewAnthropicProvider()
	case "gemini":
		p, err = provider.NewGeminiProvider()
	default:
		err = fmt.Errorf("unsupported provider: %s (valid: openai, azure_openai, anthropic, gemini)", providerName)
	}

	if err != nil {
		debug.Log("Error occurred", map[string]any{
			"error":    err.Error(),
			"provider": providerName,
			"input":    input,
		})

		fmt.Fprintf(os.Stderr, "Error fetching suggestions from %s API: %v\n", providerName, err)
		os.Exit(1)
	}

	suggestion, err := p.Fetch(cmd.Context(), input, completePrompt)
	if err != nil {
		debug.Log("Error occurred", map[string]any{
			"error":    err.Error(),
			"provider": providerName,
			"input":    input,
		})

		fmt.Fprintf(os.Stderr, "Error fetching suggestions from %s API: %v\n", providerName, err)
		os.Exit(1)
	}

	finalSuggestion := provider.ParseAndExtractCommand(suggestion)

	debug.Log("Successfully fetched suggestion", map[string]any{
		"provider":          providerName,
		"input":             input,
		"original_response": suggestion,
		"parsed_suggestion": finalSuggestion,
	})

	if outputFile == "-" || outputFile == "/dev/stdout" {
		if _, err := fmt.Fprint(os.Stdout, finalSuggestion); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write suggestion to stdout: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := os.WriteFile(outputFile, []byte(finalSuggestion), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write suggestion to file: %v\n", err)
			os.Exit(1)
		}
	}
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

	err := proxy.RunProxy(shell, proxy.ProxyOptions{
		LogFile:         logFile,
		SessionID:       sessID,
		ScrollbackLines: scrollbackLines,
	})
	if err != nil {
		fmt.Printf("Proxy error: %v\n", err)
	}
}

func runRotateLogs(cmd *cobra.Command, args []string) {
	debug.Enable(dbg)

	if proxyLogFile == "" {
		fmt.Fprintf(os.Stderr, "Error: --log-file is required\n")
		os.Exit(1)
	}

	debug.Log("Rotating log file", map[string]any{
		"log_file": proxyLogFile,
	})

	if err := logRotator.ForceRotate(proxyLogFile); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to rotate log file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully rotated log file: %s\n", proxyLogFile)
}

func runUpdate(cmd *cobra.Command, args []string) {
	checkOnly, _ := cmd.Flags().GetBool("check-only")
	fmt.Println("Checking for updates...")
	latest, url, err := updater.CheckUpdate(Version)
	if err != nil {
		fmt.Printf("Check failed: %v\n", err)
		return
	}
	if url == "" {
		fmt.Println("Smart Suggestion is already up to date!")
		if checkOnly {
			os.Exit(0)
		}
		return
	}
	fmt.Printf("New version %s available. Installing...\n", latest)
	if checkOnly {
		os.Exit(1)
	}
	if err := updater.InstallUpdate(url); err != nil {
		fmt.Printf("Install failed: %v\n", err)
	} else {
		fmt.Println("Successfully updated!")
	}
}
