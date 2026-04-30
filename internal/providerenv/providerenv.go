package providerenv

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/telemetry"
)

var credentialEnvPrefixes = []string{
	"ANTHROPIC_",
	"GEMINI_",
	"GOOGLE_",
	"OPENAI_",
}

// IsCredentialEnv reports whether key carries provider credentials that managed
// agent runtimes need to inherit from the supervisor environment.
func IsCredentialEnv(key string) bool {
	for _, prefix := range credentialEnvPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// CredentialFromFileEnv resolves KEY_FILE style credential indirection.
func CredentialFromFileEnv(key string) string {
	path := strings.TrimSpace(os.Getenv(key))
	if path == "" {
		return ""
	}
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~/") {
		if home := os.Getenv("HOME"); home != "" {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// ManagedSessionBaseline returns the environment baseline shared by all local
// managed agent/session starts. File-backed credentials are expanded here so a
// single token file remains the operator-facing SSOT.
func ManagedSessionBaseline() map[string]string {
	m := make(map[string]string)
	if v := os.Getenv("PATH"); v != "" {
		m["PATH"] = v
	}
	if v := os.Getenv("HOME"); v != "" {
		m["HOME"] = v
	}

	credentialFiles := map[string]string{
		"CLAUDE_CODE_OAUTH_TOKEN": CredentialFromFileEnv("CLAUDE_CODE_OAUTH_TOKEN_FILE"),
		"ANTHROPIC_API_KEY":       CredentialFromFileEnv("ANTHROPIC_API_KEY_FILE"),
		"ANTHROPIC_AUTH_TOKEN":    CredentialFromFileEnv("ANTHROPIC_AUTH_TOKEN_FILE"),
	}
	for _, key := range []string{"USER", "LOGNAME", "CLAUDE_CONFIG_DIR", "CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN_FILE"} {
		if v := os.Getenv(key); v != "" {
			if credentialFiles[key] != "" {
				continue
			}
			m[key] = v
		}
	}
	for key, value := range credentialFiles {
		if value != "" {
			m[key] = value
		}
	}

	for _, key := range []string{"LANG", "LC_ALL", "LC_CTYPE"} {
		if v := os.Getenv(key); v != "" {
			m[key] = v
		}
	}
	if _, ok := m["LC_ALL"]; !ok {
		m["LC_ALL"] = ""
	}
	if _, ok := m["LC_CTYPE"]; !ok {
		m["LC_CTYPE"] = ""
	}
	if m["LANG"] == "" && m["LC_ALL"] == "" && m["LC_CTYPE"] == "" {
		m["LANG"] = "en_US.UTF-8"
	}

	if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
		m["XDG_CONFIG_HOME"] = v
	} else if home := os.Getenv("HOME"); home != "" {
		m["XDG_CONFIG_HOME"] = filepath.Join(home, ".config")
	}
	if v := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); v != "" {
		m["XDG_STATE_HOME"] = v
	} else if home := os.Getenv("HOME"); home != "" {
		m["XDG_STATE_HOME"] = filepath.Join(home, ".local", "state")
	}

	for _, entry := range os.Environ() {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || val == "" {
			continue
		}
		if strings.HasPrefix(key, "GC_") || IsCredentialEnv(key) {
			if credentialFiles[key] != "" {
				continue
			}
			m[key] = val
		}
	}
	for key, value := range credentialFiles {
		if value != "" {
			m[key] = value
		}
	}
	for k, v := range telemetry.OTELEnvMap() {
		m[k] = v
	}
	m["CLAUDECODE"] = ""
	m["CLAUDE_CODE_ENTRYPOINT"] = ""
	return m
}
