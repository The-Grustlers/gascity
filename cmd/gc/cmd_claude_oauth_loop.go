package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type claudeOAuthLoopRunner func(ctx context.Context, args, env []string, stdout, stderr io.Writer) error

type claudeOAuthLoopLaunch struct {
	firstArgs     []string
	followupArgs  []string
	initialPrompt string
}

func newClaudeOAuthLoopCmd(stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "claude-oauth-loop [claude flags...]",
		Short:              "Run an interactive prompt loop backed by claude -p and Claude OAuth env",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
				return cmd.Help()
			}
			return runClaudeOAuthLoop(cmd.Context(), args, os.Stdin, stdout, stderr)
		},
	}
	return cmd
}

func runClaudeOAuthLoop(ctx context.Context, launchArgs []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return runClaudeOAuthLoopWithDeps(ctx, launchArgs, stdin, stdout, stderr, os.Getenv, os.Environ, os.ReadFile, runClaudeOAuthLoopClaude)
}

func runClaudeOAuthLoopWithDeps(
	ctx context.Context,
	launchArgs []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	getenv func(string) string,
	environ func() []string,
	readFile func(string) ([]byte, error),
	runner claudeOAuthLoopRunner,
) error {
	token, tokenFile, tokenErr := resolveClaudeOAuthLoopToken(getenv, readFile)
	if tokenErr != nil {
		fmt.Fprintf(stderr, "gc claude-oauth-loop: %v\n", tokenErr) //nolint:errcheck // best-effort stderr
	}
	if token == "" {
		fmt.Fprintln(stderr, "gc claude-oauth-loop: missing CLAUDE_CODE_OAUTH_TOKEN or readable CLAUDE_CODE_OAUTH_TOKEN_FILE") //nolint:errcheck
	}

	launch := prepareClaudeOAuthLoopLaunch(launchArgs)
	currentArgs := append([]string(nil), launch.firstArgs...)
	followupArgs := append([]string(nil), launch.followupArgs...)
	childEnv := claudeOAuthLoopChildEnv(environ(), token, tokenFile)
	promptPrefix := claudeOAuthLoopReadyPrefix(getenv)
	fmt.Fprintf(stdout, "Claude OAuth loop ready (claude -p)\n%s", promptPrefix) //nolint:errcheck

	if launch.initialPrompt != "" {
		runClaudeOAuthLoopPrompt(ctx, currentArgs, launch.initialPrompt, childEnv, stdout, stderr, runner)
		currentArgs = append([]string(nil), followupArgs...)
		fmt.Fprint(stdout, promptPrefix) //nolint:errcheck
	}

	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		prompt := strings.TrimSpace(scanner.Text())
		if prompt == "" {
			fmt.Fprint(stdout, promptPrefix) //nolint:errcheck
			continue
		}
		if claudeOAuthLoopQuit(prompt) {
			return nil
		}

		runClaudeOAuthLoopPrompt(ctx, currentArgs, prompt, childEnv, stdout, stderr, runner)
		currentArgs = append([]string(nil), followupArgs...)
		fmt.Fprint(stdout, promptPrefix) //nolint:errcheck
	}
	return scanner.Err()
}

func runClaudeOAuthLoopPrompt(
	ctx context.Context,
	claudeArgs []string,
	prompt string,
	env []string,
	stdout io.Writer,
	stderr io.Writer,
	runner claudeOAuthLoopRunner,
) {
	tracked := &lastByteWriter{w: stdout}
	args := claudeOAuthLoopCommandArgs(claudeArgs, prompt)
	if err := runner(ctx, args, env, tracked, stderr); err != nil {
		fmt.Fprintf(stderr, "gc claude-oauth-loop: claude failed: %v\n", err) //nolint:errcheck
	}
	if tracked.wrote && tracked.last != '\n' {
		fmt.Fprintln(stdout) //nolint:errcheck
	}
}

func runClaudeOAuthLoopClaude(ctx context.Context, args, env []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

func resolveClaudeOAuthLoopToken(getenv func(string) string, readFile func(string) ([]byte, error)) (token, tokenFile string, err error) {
	token = strings.TrimSpace(getenv("CLAUDE_CODE_OAUTH_TOKEN"))
	tokenFile = strings.TrimSpace(getenv("CLAUDE_CODE_OAUTH_TOKEN_FILE"))
	if token != "" || tokenFile == "" {
		return token, tokenFile, nil
	}

	data, readErr := readFile(tokenFile)
	if readErr != nil {
		return "", tokenFile, fmt.Errorf("read CLAUDE_CODE_OAUTH_TOKEN_FILE %q: %w", tokenFile, readErr)
	}
	return strings.TrimSpace(string(data)), tokenFile, nil
}

func claudeOAuthLoopReadyPrefix(getenv func(string) string) string {
	if prefix := getenv("GC_READY_PROMPT_PREFIX"); strings.TrimSpace(prefix) != "" {
		return prefix
	}
	return "\u276f "
}

func claudeOAuthLoopQuit(prompt string) bool {
	switch strings.TrimSpace(strings.ToLower(prompt)) {
	case "/exit", "/quit", "exit", "quit":
		return true
	default:
		return false
	}
}

func claudeOAuthLoopCommandArgs(launchArgs []string, prompt string) []string {
	args := make([]string, 0, 1+len(launchArgs)+1)
	args = append(args, "-p")
	args = append(args, launchArgs...)
	args = append(args, prompt)
	return args
}

func prepareClaudeOAuthLoopLaunch(launchArgs []string) claudeOAuthLoopLaunch {
	args, positional := claudeOAuthLoopStripSettingsAndPositionals(launchArgs)
	initialPrompt := strings.TrimSpace(strings.Join(positional, " "))
	return claudeOAuthLoopLaunch{
		firstArgs:     args,
		followupArgs:  claudeOAuthLoopResumeArgs(args),
		initialPrompt: initialPrompt,
	}
}

func claudeOAuthLoopStripSettingsAndPositionals(launchArgs []string) ([]string, []string) {
	args := make([]string, 0, len(launchArgs))
	var positional []string
	for i := 0; i < len(launchArgs); i++ {
		arg := launchArgs[i]
		if arg == "--settings" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--settings=") {
			continue
		}
		if claudeOAuthLoopFlagTakesValue(arg) {
			args = append(args, arg)
			if !strings.Contains(arg, "=") && i+1 < len(launchArgs) {
				i++
				args = append(args, launchArgs[i])
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			args = append(args, arg)
			continue
		}
		positional = append(positional, arg)
	}
	return args, positional
}

func claudeOAuthLoopFlagTakesValue(arg string) bool {
	name := arg
	if before, _, ok := strings.Cut(arg, "="); ok {
		name = before
	}
	switch name {
	case "--add-dir",
		"--allowedTools",
		"--append-system-prompt",
		"--disallowedTools",
		"--effort",
		"--fallback-model",
		"--input-format",
		"--max-turns",
		"--mcp-config",
		"--model",
		"--output-format",
		"--permission-mode",
		"--permission-prompt-tool",
		"--resume",
		"--session-id":
		return true
	default:
		return false
	}
}

func claudeOAuthLoopResumeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--session-id" {
			out = append(out, "--resume")
			if i+1 < len(args) {
				i++
				out = append(out, args[i])
			}
			continue
		}
		if strings.HasPrefix(arg, "--session-id=") {
			_, value, _ := strings.Cut(arg, "=")
			out = append(out, "--resume", value)
			continue
		}
		out = append(out, arg)
	}
	return out
}

func claudeOAuthLoopChildEnv(base []string, token, tokenFile string) []string {
	filterAnthropic := strings.TrimSpace(token) != ""
	out := make([]string, 0, len(base)+2)
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			out = append(out, entry)
			continue
		}
		switch key {
		case "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN_FILE":
			continue
		case "ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY_FILE", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN_FILE":
			if filterAnthropic {
				continue
			}
		}
		out = append(out, entry)
	}
	if strings.TrimSpace(token) != "" {
		out = append(out, "CLAUDE_CODE_OAUTH_TOKEN="+token)
	}
	if strings.TrimSpace(tokenFile) != "" {
		out = append(out, "CLAUDE_CODE_OAUTH_TOKEN_FILE="+tokenFile)
	}
	return out
}

type lastByteWriter struct {
	w     io.Writer
	wrote bool
	last  byte
}

func (w *lastByteWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		w.wrote = true
		w.last = p[len(p)-1]
	}
	return w.w.Write(p)
}
