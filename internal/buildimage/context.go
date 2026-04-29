package buildimage

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gastownhall/gascity/internal/citylayout"
)

// Options configures the build context assembly.
type Options struct {
	// CityPath is the resolved city directory on disk.
	CityPath string
	// OutputDir is where to write the build context (Dockerfile + workspace/).
	OutputDir string
	// BaseImage is the Docker base image. Default: "gc-agent:latest".
	BaseImage string
	// Tag is the image tag for docker build.
	Tag string
	// RigPaths maps rig name → local repo path for baking rig content.
	RigPaths map[string]string
}

// Manifest records what was baked into the image for debugging.
type Manifest struct {
	Version   int       `json:"version"`
	CityName  string    `json:"city_name"`
	Built     time.Time `json:"built"`
	BaseImage string    `json:"base_image"`
}

// excludedPaths returns true for paths that should never be baked, regardless
// of any user-supplied .dockerignore. These are sane defaults: runtime state,
// secrets, and the embedded beads database (multi-GB; pods access via the
// host dolt server, not via a baked-in copy).
func excludedPath(rel string) bool {
	// Runtime state directory — sockets, watchers, ephemeral state.
	if rel == citylayout.RuntimeRoot+"/runtime" ||
		strings.HasPrefix(rel, citylayout.RuntimeRoot+"/runtime/") {
		return true
	}
	// Controller sockets / locks / event log (huge + transient).
	if rel == ".gc/controller.lock" || rel == ".gc/controller.sock" ||
		rel == ".gc/events.jsonl" {
		return true
	}
	// Agent registry (runtime state).
	if rel == ".gc/agents" || strings.HasPrefix(rel, ".gc/agents/") {
		return true
	}
	// Per-session tmp scratch.
	if rel == ".gc/tmp" || strings.HasPrefix(rel, ".gc/tmp/") {
		return true
	}
	// Embedded beads database — multi-GB; pods access via the dolt server on
	// the host (BEADS_DOLT_SERVER_HOST/PORT), never via a local copy.
	if rel == ".beads" || strings.HasPrefix(rel, ".beads/") {
		return true
	}
	// Secrets: match exact base names and specific extensions, not substrings.
	base := filepath.Base(rel)
	ext := filepath.Ext(base)
	if base == ".env" || base == "credentials.json" || base == "credentials.yaml" ||
		base == "credentials.yml" || ext == ".secret" || ext == ".pem" || ext == ".key" {
		return true
	}
	return false
}

// AssembleContext builds the Docker build context directory.
// It creates outputDir/workspace/ with city content and outputDir/Dockerfile.
func AssembleContext(opts Options) error {
	if opts.CityPath == "" {
		return fmt.Errorf("city path is required")
	}
	if opts.OutputDir == "" {
		return fmt.Errorf("output dir is required")
	}
	if opts.BaseImage == "" {
		opts.BaseImage = "gc-agent:latest"
	}

	wsDir := filepath.Join(opts.OutputDir, "workspace")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}

	// Honor a user-supplied .dockerignore at the city root (industry-standard
	// Docker pattern). Applied in addition to the built-in excludedPath rules.
	cityPatterns, err := LoadDockerignore(filepath.Join(opts.CityPath, ".dockerignore"))
	if err != nil {
		return fmt.Errorf("loading city .dockerignore: %w", err)
	}

	// Copy city directory contents into workspace, excluding runtime state.
	if err := copyDirFiltered(opts.CityPath, wsDir, cityPatterns); err != nil {
		return fmt.Errorf("copying city to workspace: %w", err)
	}

	// Copy rig paths into workspace. Each rig may have its own .dockerignore.
	for rigName, rigPath := range opts.RigPaths {
		rigDst := filepath.Join(wsDir, rigName)
		rigPatterns, err := LoadDockerignore(filepath.Join(rigPath, ".dockerignore"))
		if err != nil {
			return fmt.Errorf("loading rig %q .dockerignore: %w", rigName, err)
		}
		if err := copyDirFiltered(rigPath, rigDst, rigPatterns); err != nil {
			return fmt.Errorf("copying rig %q: %w", rigName, err)
		}
	}

	// Write prebaked manifest.
	cityName := filepath.Base(opts.CityPath)
	manifest := Manifest{
		Version:   1,
		CityName:  cityName,
		Built:     time.Now().UTC(),
		BaseImage: opts.BaseImage,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, ".gc-prebaked"), manifestData, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}

	// Generate Dockerfile.
	dockerfile := GenerateDockerfile(opts.BaseImage)
	if err := os.WriteFile(filepath.Join(opts.OutputDir, "Dockerfile"), dockerfile, 0o644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}

	return nil
}

// copyDirFiltered copies src directory to dst, skipping excluded paths.
// Built-in excludedPath rules apply unconditionally; userPatterns is the
// parsed .dockerignore (may be nil).
//
// Symlinks are handled gracefully: a symlink-to-file is recreated as a
// symlink in dst (preserving the link), and a symlink-to-directory is
// recreated as a symlink WITHOUT recursing through it (avoids the
// "copy_file_range: is a directory" failure mode that filepath.Walk's
// default symlink-following behavior produced).
func copyDirFiltered(src, dst string, userPatterns []Pattern) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}

		fullRel, err := filepath.Rel(filepath.Dir(src), path)
		if err != nil {
			return err
		}
		// filepath.Walk uses os.PathSeparator; normalize for pattern matching
		// which expects forward slashes (Docker convention).
		relForward := filepath.ToSlash(rel)
		if excludedPath(rel) || excludedPath(fullRel) ||
			MatchPatterns(userPatterns, relForward, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)

		// Handle symlinks: recreate them in dst rather than following. This
		// is what users intuitively expect ("copy the directory") and avoids
		// crashing on symlink-to-directory entries (e.g., python venv lib64,
		// gc-materialized .claude/skills/<name> → packs).
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", path, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// If the destination already exists (rare in a fresh build dir),
			// remove it so Symlink doesn't fail with EEXIST.
			_ = os.Remove(target)
			if err := os.Symlink(linkTarget, target); err != nil {
				return fmt.Errorf("symlink %s -> %s: %w", target, linkTarget, err)
			}
			// Symlink to dir: don't recurse (Walk already handles ModeSymlink
			// by not descending, but be explicit).
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file.
func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
