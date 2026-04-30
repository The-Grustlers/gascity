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

var pathOnlyFileBackedCredentials = map[string]bool{
	"CLAUDE_CODE_OAUTH_TOKEN": true,
}

var oauthIncompatibleAnthropicCredentials = map[string]bool{
	"ANTHROPIC_API_KEY":    true,
	"ANTHROPIC_AUTH_TOKEN": true,
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
	path := credentialFilePathEnv(key)
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func credentialFilePathEnv(key string) string {
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
	return path
}

func fileBackedCredentialValues() map[string]string {
	values := make(map[string]string, len(fileBackedCredentials))
	for key, fileKey := range fileBackedCredentials {
		values[key] = CredentialFromFileEnv(fileKey)
	}
	suppressOAuthMirroredAnthropicCredentials(values)
	return values
}

func suppressOAuthMirroredAnthropicCredentials(values map[string]string) {
	oauth := strings.TrimSpace(values["CLAUDE_CODE_OAUTH_TOKEN"])
	if oauth == "" {
		return
	}
	for key := range oauthIncompatibleAnthropicCredentials {
		if strings.TrimSpace(values[key]) == oauth {
			values[key] = ""
		}
	}
}

func activeFileBackedCredentialFiles(values map[string]string) map[string]string {
	files := make(map[string]string, len(values))
	for key, fileKey := range fileBackedCredentials {
		if strings.TrimSpace(values[key]) == "" {
			continue
		}
		if path := credentialFilePathEnv(fileKey); path != "" {
			files[fileKey] = path
		}
	}
	return files
}

func shouldProjectFileBackedCredentialValue(key string) bool {
	return !pathOnlyFileBackedCredentials[key]
}

func knownCredentialFileKey(fileKey string) bool {
	for _, candidate := range fileBackedCredentials {
		if fileKey == candidate {
			return true
		}
	}
	return false
}

func shouldSkipCredentialEnv(key, value string, fileValues map[string]string) bool {
	if knownCredentialFileKey(key) {
		activeFiles := activeFileBackedCredentialFiles(fileValues)
		return activeFiles[key] == ""
	}
	if fileValues[key] != "" {
		return true
	}
	if oauthIncompatibleAnthropicCredentials[key] {
		if oauth := strings.TrimSpace(fileValues["CLAUDE_CODE_OAUTH_TOKEN"]); oauth != "" && strings.TrimSpace(value) == oauth {
			return true
		}
	}
	return false
}

func applyOAuthIncompatibleCredentialUnsets(env map[string]string, fileValues map[string]string) {
	oauth := strings.TrimSpace(fileValues["CLAUDE_CODE_OAUTH_TOKEN"])
	oauthFile := credentialFilePathEnv("CLAUDE_CODE_OAUTH_TOKEN_FILE")
	if oauth == "" && oauthFile == "" {
		return
	}
	for key := range oauthIncompatibleAnthropicCredentials {
		if strings.TrimSpace(fileValues[key]) != "" {
			continue
		}
		if value := strings.TrimSpace(env[key]); value == "" || (oauth != "" && value == oauth) {
			env[key] = ""
		}
		fileKey := fileBackedCredentials[key]
		if value := strings.TrimSpace(env[fileKey]); value == "" || (oauthFile != "" && value == oauthFile) {
			env[fileKey] = ""
		}
	}
}

// MergeFileBackedCredentials overlays only file-backed credential values onto
// env. Use this in planner/reconciler paths where adding the full managed
// baseline would churn runtime fingerprints.
func MergeFileBackedCredentials(env map[string]string) map[string]string {
	fileValues := fileBackedCredentialValues()
	out := make(map[string]string, len(env)+len(fileBackedCredentials)*2)
	for key, value := range env {
		if shouldSkipCredentialEnv(key, value, fileValues) {
			continue
		}
		out[key] = value
	}
	activeFiles := activeFileBackedCredentialFiles(fileValues)
	for key, value := range fileValues {
		if value != "" && shouldProjectFileBackedCredentialValue(key) {
			out[key] = value
		}
		if fileKey := fileBackedCredentials[key]; activeFiles[fileKey] != "" {
			out[fileKey] = activeFiles[fileKey]
		}
	}
	applyOAuthIncompatibleCredentialUnsets(out, fileValues)
	return out
}

// MergeManagedSessionEnv overlays explicit provider/session env on top of the
// managed baseline while preserving file-backed credentials as the SSOT.
func MergeManagedSessionEnv(env map[string]string) map[string]string {
	out := ManagedSessionBaseline()
	fileValues := fileBackedCredentialValues()
	for key, value := range env {
		expanded := os.ExpandEnv(value)
		if shouldSkipCredentialEnv(key, expanded, fileValues) {
			continue
		}
		out[key] = expanded
	}
	for key, value := range fileValues {
		if value != "" && shouldProjectFileBackedCredentialValue(key) {
			out[key] = value
		}
	}
	for fileKey, value := range activeFileBackedCredentialFiles(fileValues) {
		out[fileKey] = value
	}
	applyOAuthIncompatibleCredentialUnsets(out, fileValues)
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
		if value != "" && shouldProjectFileBackedCredentialValue(key) {
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
			if shouldSkipCredentialEnv(key, val, credentialFiles) {
				continue
			}
			m[key] = val
		}
	}
	for key, value := range credentialFiles {
		if value != "" && shouldProjectFileBackedCredentialValue(key) {
			m[key] = value
		}
	}
	for fileKey, value := range activeFileBackedCredentialFiles(credentialFiles) {
		m[fileKey] = value
	}
	applyOAuthIncompatibleCredentialUnsets(m, credentialFiles)
	for k, v := range telemetry.OTELEnvMap() {
		m[k] = v
	}
	m["CLAUDECODE"] = ""
	m["CLAUDE_CODE_ENTRYPOINT"] = ""
	return m
}
