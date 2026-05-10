package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoBeadsHealth_FileProvider(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityFlag = dir
	defer func() { cityFlag = "" }()
	t.Setenv("GC_BEADS", "file")

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(false, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Beads provider: healthy") {
		t.Errorf("should show healthy message: %s", stdout.String())
	}
}

func TestDoBeadsHealth_FileProviderQuiet(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	cityFlag = dir
	defer func() { cityFlag = "" }()
	t.Setenv("GC_BEADS", "file")

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(true, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("quiet mode should produce no stdout, got: %s", stdout.String())
	}
}

func TestDoBeadsHealth_ExecProviderHealthy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := writeTestScript(t, "", 0, "")
	cityFlag = dir
	defer func() { cityFlag = "" }()
	t.Setenv("GC_BEADS", "exec:"+script)

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(false, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Beads provider: healthy") {
		t.Errorf("should show healthy message: %s", stdout.String())
	}
}

func TestDoBeadsHealth_ExecProviderUnhealthy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Script always fails → health and recover both fail.
	script := writeTestScript(t, "", 1, "server down")
	cityFlag = dir
	defer func() { cityFlag = "" }()
	t.Setenv("GC_BEADS", "exec:"+script)

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(false, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "recovery failed") {
		t.Errorf("stderr should mention recovery failure: %s", stderr.String())
	}
}

func TestDoBeadsHealth_BdSkip(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	MaterializeBuiltinPacks(dir) //nolint:errcheck
	cityFlag = dir
	defer func() { cityFlag = "" }()
	t.Setenv("GC_BEADS", "bd")
	t.Setenv("GC_DOLT", "skip")

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(false, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Beads provider: healthy") {
		t.Errorf("GC_DOLT=skip should pass: %s", stdout.String())
	}
}

func TestDoBeadsHealth_RejectsInheritedRigLocalDoltDatabase(t *testing.T) {
	cityDir := t.TempDir()
	rigDir := filepath.Join(t.TempDir(), "rabble")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigDir, ".beads", "dolt", "rb", ".dolt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(fmt.Sprintf(`[workspace]
name = "test-city"

[[rigs]]
name = "rabble"
path = %q
prefix = "rb"
`, rigDir)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigDir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, ".beads", "config.yaml"), []byte("issue_prefix: rb\ngc.endpoint_origin: inherited_city\ngc.endpoint_status: verified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, ".beads", "metadata.json"), []byte("{\"backend\":\"dolt\",\"dolt_database\":\"rb\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCityFlag := cityFlag
	cityFlag = cityDir
	t.Cleanup(func() { cityFlag = oldCityFlag })
	t.Setenv("GC_BEADS", "file")

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(false, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; stdout = %s; stderr = %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "split-brain guard") || !strings.Contains(stderr.String(), "rig-local Dolt database") {
		t.Fatalf("stderr should mention split-brain local database, got: %s", stderr.String())
	}
}

func TestDoBeadsHealth_AllowsInheritedRigRootDoltMetadataOnly(t *testing.T) {
	cityDir := t.TempDir()
	rigDir := filepath.Join(t.TempDir(), "rabble")
	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigDir, ".beads", "dolt", ".dolt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(fmt.Sprintf(`[workspace]
name = "test-city"

[[rigs]]
name = "rabble"
path = %q
prefix = "rb"
`, rigDir)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigDir, ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, ".beads", "config.yaml"), []byte("issue_prefix: rb\ngc.endpoint_origin: inherited_city\ngc.endpoint_status: verified\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigDir, ".beads", "metadata.json"), []byte("{\"backend\":\"dolt\",\"dolt_database\":\"rb\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCityFlag := cityFlag
	cityFlag = cityDir
	t.Cleanup(func() { cityFlag = oldCityFlag })
	t.Setenv("GC_BEADS", "file")

	var stdout, stderr bytes.Buffer
	code := doBeadsHealth(false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Beads provider: healthy") {
		t.Fatalf("should show healthy message: %s", stdout.String())
	}
}
