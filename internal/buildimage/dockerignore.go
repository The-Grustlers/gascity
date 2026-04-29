package buildimage

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Pattern is a single .dockerignore line in parsed form.
type Pattern struct {
	pattern string // the literal/glob portion (after stripping ! and trailing /)
	negate  bool   // line started with "!"
	dirOnly bool   // line ended with "/"
}

// LoadDockerignore reads and parses a .dockerignore file at the given path.
// Returns nil patterns if the file doesn't exist (not an error — absence is
// the same as "no extra exclusions").
//
// Supports the common subset of Docker's .dockerignore semantics:
//   - Lines starting with "#" are comments; blank lines are skipped.
//   - Trailing "/" marks the entry as directory-only.
//   - Leading "!" is a re-include (negation) override.
//   - Glob patterns via filepath.Match plus a "**/" recursive prefix.
//   - Plain (non-glob) patterns also match anything inside that prefix.
//
// We intentionally don't pull in github.com/moby/patternmatcher to keep the
// dep tree small; if richer semantics are needed later this is the place to
// swap in.
func LoadDockerignore(path string) ([]Pattern, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var patterns []Pattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := Pattern{}
		if strings.HasPrefix(line, "!") {
			p.negate = true
			line = strings.TrimPrefix(line, "!")
		}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		p.pattern = line
		patterns = append(patterns, p)
	}
	return patterns, scanner.Err()
}

// MatchPatterns reports whether rel should be excluded. Last matching pattern
// wins (Docker semantics): a later "!foo" can re-include an earlier "foo".
// rel uses forward slashes, relative to the build context root.
func MatchPatterns(patterns []Pattern, rel string, isDir bool) bool {
	excluded := false
	for _, p := range patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if patternMatches(p.pattern, rel) {
			excluded = !p.negate
		}
	}
	return excluded
}

// patternMatches checks one pattern against one path. Supports:
//   - exact match
//   - filepath.Match glob
//   - "**/X" (recursive base-name match)
//   - prefix match for non-glob patterns (so "foo" excludes "foo/bar")
func patternMatches(pattern, rel string) bool {
	// Recursive base-name match: "**/foo" or "**/*.log".
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		if ok, _ := filepath.Match(suffix, filepath.Base(rel)); ok {
			return true
		}
		// also allow matching at root
		if ok, _ := filepath.Match(suffix, rel); ok {
			return true
		}
	}
	// Glob match.
	if ok, _ := filepath.Match(pattern, rel); ok {
		return true
	}
	// Plain prefix match for non-glob patterns: "foo" matches "foo" and
	// "foo/anything" so directory-style exclusions work without trailing "/".
	if !strings.ContainsAny(pattern, "*?[") {
		if rel == pattern || strings.HasPrefix(rel, pattern+"/") {
			return true
		}
	}
	return false
}
