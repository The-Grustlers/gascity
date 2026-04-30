package providerenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeManagedSessionEnvPreservesFileBackedCredentialSSOT(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "oauth-token")
	if err := os.WriteFile(tokenFile, []byte("file-backed-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN_FILE", tokenFile)

	got := MergeManagedSessionEnv(map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": "stale-session-token",
		"EXTRA_PROVIDER_ENV":      "kept",
	})

	if got["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN = %q, want empty because file path is the managed SSOT", got["CLAUDE_CODE_OAUTH_TOKEN"])
	}
	if got["CLAUDE_CODE_OAUTH_TOKEN_FILE"] != tokenFile {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN_FILE = %q, want %q", got["CLAUDE_CODE_OAUTH_TOKEN_FILE"], tokenFile)
	}
	if got["EXTRA_PROVIDER_ENV"] != "kept" {
		t.Fatalf("EXTRA_PROVIDER_ENV = %q, want kept", got["EXTRA_PROVIDER_ENV"])
	}
}

func TestMergeManagedSessionEnvSuppressesOAuthMirroredAnthropicKey(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "oauth-token")
	if err := os.WriteFile(tokenFile, []byte("oauth-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN_FILE", tokenFile)
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY_FILE", tokenFile)

	got := MergeManagedSessionEnv(map[string]string{
		"ANTHROPIC_API_KEY":      "oauth-token",
		"ANTHROPIC_API_KEY_FILE": tokenFile,
	})

	if got["CLAUDE_CODE_OAUTH_TOKEN"] != "" {
		t.Fatalf("CLAUDE_CODE_OAUTH_TOKEN = %q, want empty because file path is the managed SSOT", got["CLAUDE_CODE_OAUTH_TOKEN"])
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
