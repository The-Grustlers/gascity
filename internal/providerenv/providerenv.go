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

var fileBackedCredentials = map[string]string{
	"CLAUDE_CODE_OAUTH_TOKEN": "CLAUDE_CODE_OAUTH_TOKEN_FILE",
	"ANTHROPIC_API_KEY":       "ANTHROPIC_API_KEY_FILE",
	"ANTHROPIC_AUTH_TOKEN":    "ANTHROPIC_AUTH_TOKEN_FILE",
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

func fileBackedCredentialValues() map[string]string {
	values := make(map[string]string, len(fileBackedCredentials))
	for key, fileKey := range fileBackedCredentials {
		values[key] = CredentialFromFileEnv(fileKey)
	}
	return values
}

// MergeFileBackedCredentials overlays only file-backed credential values onto
// env. Use this in planner/reconciler paths where adding the full managed
// baseline would churn runtime fingerprints.
func MergeFileBackedCredentials(env map[string]string) map[string]string {
	out := make(map[string]string, len(env)+len(fileBackedCredentials)*2)
	for key, value := range env {
		out[key] = value
	}
	for key, fileKey := range fileBackedCredentials {
		if value := CredentialFromFileEnv(fileKey); value != "" {
			out[key] = value
			if path := strings.TrimSpace(os.Getenv(fileKey)); path != "" {
				out[fileKey] = os.ExpandEnv(path)
			}
		}
	}
	return out
}

// MergeManagedSessionEnv overlays explicit provider/session env on top of the
// managed baseline while preserving file-backed credentials as the SSOT.
func MergeManagedSessionEnv(env map[string]string) map[string]string {
	out := ManagedSessionBaseline()
	fileValues := fileBackedCredentialValues()
	for key, value := range env {
		expanded := os.ExpandEnv(value)
		if fileValues[key] != "" {
			continue
		}
		out[key] = expanded
	}
	for key, value := range fileValues {
		if value != "" {
			out[key] = value
		}
	}
	for _, fileKey := range fileBackedCredentials {
		if value := os.Getenv(fileKey); strings.TrimSpace(value) != "" {
			out[fileKey] = os.ExpandEnv(value)
		}
	}
	return out
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

	credentialFiles := fileBackedCredentialValues()
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
