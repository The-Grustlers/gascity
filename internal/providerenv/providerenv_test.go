package providerenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeManagedSessionEnvPreservesRawClaudeOAuthTokenSSOT(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "raw-oauth-token")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN_FILE", "/tmp/stale-oauth-file")

	got := MergeManagedSessionEnv(map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN":      "stale-session-token",
		"CLAUDE_CODE_OAUTH_TOKEN_FILE": "/tmp/stale-session-file",
		"EXTRA_PROVIDER_ENV":           "kept",
	})

	if got["CLAUDE_CODE_OAUTH_TOKEN"] != "raw-oauth-token" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN = %q, want raw OAuth token from env", got["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if got["CLAUDE_CODE_OAUTH_TOKEN_FILE"] != "" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN_FILE = %q, want empty because raw env token is the SSOT", got["CLAUDE_CODE_OAUTH_TOKEN_FILE"])
	}
	if got["EXTRA_PROVIDER_ENV"] != "kept" {
		t.Fatalf("EXTRA_PROVIDER_ENV = %q, want kept", got["EXTRA_PROVIDER_ENV"])
	}
}

func TestMergeManagedSessionEnvSuppressesOAuthMirroredAnthropicKey(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "oauth-token")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY_FILE", "")

	got := MergeManagedSessionEnv(map[string]string{
		"ANTHROPIC_API_KEY": "oauth-token",
	})

	if got["CLAUDE_CODE_OAUTH_TOKEN"] != "oauth-token" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN = %q, want raw OAuth token", got["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if got["ANTHROPIC_API_KEY"] != "" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want explicit unset", got["ANTHROPIC_API_KEY"])
	}
	if got["ANTHROPIC_API_KEY_FILE"] != "" {
		t.Fatalf("ANTHROPIC_API_KEY_FILE = %q, want explicit unset", got["ANTHROPIC_API_KEY_FILE"])
	}
}

func TestMergeManagedSessionEnvKeepsRealAnthropicAPIKeyFile(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "oauth-token")
	apiKeyFile := filepath.Join(dir, "anthropic-key")
	if err := os.WriteFile(tokenFile, []byte("oauth-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	if err := os.WriteFile(apiKeyFile, []byte("sk-ant-real\n"), 0o600); err != nil {
		t.Fatalf("write api key file: %v", err)
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN_FILE", tokenFile)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY_FILE", apiKeyFile)

	got := MergeManagedSessionEnv(nil)

	if got["ANTHROPIC_API_KEY"] != "sk-ant-real" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want real API key from file", got["ANTHROPIC_API_KEY"])
	}
	if got["ANTHROPIC_API_KEY_FILE"] != apiKeyFile {
		t.Fatalf("ANTHROPIC_API_KEY_FILE = %q, want %q", got["ANTHROPIC_API_KEY_FILE"], apiKeyFile)
	}
}
