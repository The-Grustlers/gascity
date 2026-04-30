package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestResolveClaudeOAuthLoopTokenPrefersRawEnv(t *testing.T) {
	token, tokenFile, err := resolveClaudeOAuthLoopToken(
		func(key string) string {
			switch key {
			case "CLAUDE_CODE_OAUTH_TOKEN":
				return " raw-token "
			case "CLAUDE_CODE_OAUTH_TOKEN_FILE":
				return "/tmp/token"
			default:
				return ""
			}
		},
		func(string) ([]byte, error) {
			t.Fatal("readFile should not be called when raw token is set")
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("resolveClaudeOAuthLoopToken returned error: %v", err)
	}
	if token != "raw-token" {
		t.Fatalf("token = %q, want raw-token", token)
	}
	if tokenFile != "/tmp/token" {
		t.Fatalf("tokenFile = %q, want /tmp/token", tokenFile)
	}
}

func TestResolveClaudeOAuthLoopTokenReadsFileFallback(t *testing.T) {
	token, tokenFile, err := resolveClaudeOAuthLoopToken(
		func(key string) string {
			if key == "CLAUDE_CODE_OAUTH_TOKEN_FILE" {
				return "/tmp/token"
			}
			return ""
		},
		func(path string) ([]byte, error) {
			if path != "/tmp/token" {
				t.Fatalf("path = %q, want /tmp/token", path)
			}
			return []byte(" file-token \n"), nil
		},
	)
	if err != nil {
		t.Fatalf("resolveClaudeOAuthLoopToken returned error: %v", err)
	}
	if token != "file-token" {
		t.Fatalf("token = %q, want file-token", token)
	}
	if tokenFile != "/tmp/token" {
		t.Fatalf("tokenFile = %q, want /tmp/token", tokenFile)
	}
}

func TestResolveClaudeOAuthLoopTokenReportsUnreadableFile(t *testing.T) {
	wantErr := errors.New("permission denied")
	token, tokenFile, err := resolveClaudeOAuthLoopToken(
		func(key string) string {
			if key == "CLAUDE_CODE_OAUTH_TOKEN_FILE" {
				return "/tmp/token"
			}
			return ""
		},
		func(string) ([]byte, error) { return nil, wantErr },
	)
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
	if tokenFile != "/tmp/token" {
		t.Fatalf("tokenFile = %q, want /tmp/token", tokenFile)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wrapped %v", err, wantErr)
	}
}

func TestClaudeOAuthLoopChildEnvInjectsOAuthAndStripsAnthropicWhenTokenSet(t *testing.T) {
	got := envMap(claudeOAuthLoopChildEnv([]string{
		"PATH=/usr/bin",
		"CLAUDE_CODE_OAUTH_TOKEN=old",
		"CLAUDE_CODE_OAUTH_TOKEN_FILE=/tmp/old",
		"ANTHROPIC_API_KEY=bad",
		"ANTHROPIC_API_KEY_FILE=/tmp/bad",
		"ANTHROPIC_AUTH_TOKEN=bad",
		"OTHER=value",
	}, "fresh-token", "/tmp/fresh"))

	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_API_KEY_FILE", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_AUTH_TOKEN_FILE"} {
		if _, ok := got[key]; ok {
			t.Fatalf("%s should be stripped when OAuth token is set", key)
		}
	}
	if got["CLAUDE_CODE_OAUTH_TOKEN"] != "fresh-token" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN = %q, want fresh-token", got["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if got["CLAUDE_CODE_OAUTH_TOKEN_FILE"] != "/tmp/fresh" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN_FILE = %q, want /tmp/fresh", got["CLAUDE_CODE_OAUTH_TOKEN_FILE"])
	}
	if got["OTHER"] != "value" {
		t.Fatalf("OTHER = %q, want value", got["OTHER"])
	}
}

func TestClaudeOAuthLoopRunsPrintModeWithPreservedLaunchArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var calls []struct {
		args []string
		env  map[string]string
	}
	getenv := func(key string) string {
		switch key {
		case "CLAUDE_CODE_OAUTH_TOKEN_FILE":
			return "/tmp/oauth"
		case "GC_READY_PROMPT_PREFIX":
			return "ready> "
		default:
			return ""
		}
	}
	environ := func() []string {
		return []string{
			"PATH=/usr/bin",
			"ANTHROPIC_API_KEY=bad",
			"CLAUDE_CODE_OAUTH_TOKEN_FILE=/tmp/stale",
		}
	}
	readFile := func(path string) ([]byte, error) {
		if path != "/tmp/oauth" {
			t.Fatalf("path = %q, want /tmp/oauth", path)
		}
		return []byte("fresh-token\n"), nil
	}
	runner := func(_ context.Context, args, env []string, stdout, _ io.Writer) error {
		calls = append(calls, struct {
			args []string
			env  map[string]string
		}{
			args: append([]string(nil), args...),
			env:  envMap(env),
		})
		_, err := io.WriteString(stdout, "PONG\n")
		return err
	}

	err := runClaudeOAuthLoopWithDeps(
		context.Background(),
		[]string{"--dangerously-skip-permissions", "--session-id", "sid-123", "--settings", "/tmp/settings.json"},
		strings.NewReader("ping\n\n/exit\n"),
		&stdout,
		&stderr,
		getenv,
		environ,
		readFile,
		runner,
	)
	if err != nil {
		t.Fatalf("runClaudeOAuthLoopWithDeps returned error: %v", err)
	}

	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	wantArgs := []string{"-p", "--dangerously-skip-permissions", "--session-id", "sid-123", "ping"}
	if !reflect.DeepEqual(calls[0].args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", calls[0].args, wantArgs)
	}
	if calls[0].env["CLAUDE_CODE_OAUTH_TOKEN"] != "fresh-token" {
		t.Fatalf("child token = %q, want fresh-token", calls[0].env["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if _, ok := calls[0].env["ANTHROPIC_API_KEY"]; ok {
		t.Fatal("ANTHROPIC_API_KEY should be stripped from child env")
	}
	if got := stdout.String(); !strings.Contains(got, "Claude OAuth loop ready (claude -p)\nready> PONG\nready> ready> ") {
		t.Fatalf("stdout = %q", got)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestClaudeOAuthLoopCommandArgs(t *testing.T) {
	got := claudeOAuthLoopCommandArgs([]string{"--resume", "sid"}, "hello")
	want := []string{"-p", "--resume", "sid", "hello"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestPrepareClaudeOAuthLoopLaunchStripsSettingsAndExtractsInitialPrompt(t *testing.T) {
	got := prepareClaudeOAuthLoopLaunch([]string{
		"--dangerously-skip-permissions",
		"--effort", "max",
		"--settings", "/tmp/settings.json",
		"--session-id", "sid-123",
		"startup prompt",
	})
	wantFirst := []string{"--dangerously-skip-permissions", "--effort", "max", "--session-id", "sid-123"}
	if !reflect.DeepEqual(got.firstArgs, wantFirst) {
		t.Fatalf("firstArgs = %#v, want %#v", got.firstArgs, wantFirst)
	}
	wantFollowup := []string{"--dangerously-skip-permissions", "--effort", "max", "--resume", "sid-123"}
	if !reflect.DeepEqual(got.followupArgs, wantFollowup) {
		t.Fatalf("followupArgs = %#v, want %#v", got.followupArgs, wantFollowup)
	}
	if got.initialPrompt != "startup prompt" {
		t.Fatalf("initialPrompt = %q, want startup prompt", got.initialPrompt)
	}
}

func TestClaudeOAuthLoopRunsInitialPromptThenResumesFollowups(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var gotArgs [][]string
	runner := func(_ context.Context, args, _ []string, stdout, _ io.Writer) error {
		gotArgs = append(gotArgs, append([]string(nil), args...))
		_, err := io.WriteString(stdout, "ok\n")
		return err
	}

	err := runClaudeOAuthLoopWithDeps(
		context.Background(),
		[]string{"--session-id", "sid-123", "startup prompt"},
		strings.NewReader("next prompt\n/exit\n"),
		&stdout,
		&stderr,
		func(key string) string {
			if key == "CLAUDE_CODE_OAUTH_TOKEN" {
				return "token"
			}
			return ""
		},
		func() []string { return nil },
		func(string) ([]byte, error) { return nil, errors.New("unused") },
		runner,
	)
	if err != nil {
		t.Fatalf("runClaudeOAuthLoopWithDeps returned error: %v", err)
	}
	want := [][]string{
		{"-p", "--session-id", "sid-123", "startup prompt"},
		{"-p", "--resume", "sid-123", "next prompt"},
	}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args = %#v, want %#v", gotArgs, want)
	}
	if stderr.String() != "" {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func envMap(entries []string) map[string]string {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}
