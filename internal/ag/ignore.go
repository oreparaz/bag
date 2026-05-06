package ag

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ignorePattern is a single line from .gitignore / .ignore. We support a
// pragmatic subset:
//
//   - leading "!" (negation) is recognized but ignored — for v1 we
//     don't honor un-ignore semantics
//   - trailing "/" matches directories only
//   - patterns containing "/" are matched against the relative path
//   - patterns without "/" match against any basename in the tree
//   - "**" is not specially handled (treated like normal *)
//
// This covers the vast majority of real-world .gitignore files. Full
// gitignore semantics are tracked in FUTURE.md.
type ignorePattern struct {
	raw     string
	pattern string
	dirOnly bool
	rooted  bool
}

func (p ignorePattern) matches(rel string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	if p.rooted {
		clean := filepath.ToSlash(filepath.Clean(rel))
		if ok, _ := filepath.Match(p.pattern, clean); ok {
			return true
		}
		// Also accept matches against any directory prefix —
		// "foo/bar" should match "foo/bar/baz/x" because the entire
		// "foo/bar" subtree is ignored.
		if strings.HasPrefix(clean, p.pattern+"/") {
			return true
		}
		return false
	}
	// Basename match for any path component.
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p2 := range parts {
		if ok, _ := filepath.Match(p.pattern, p2); ok {
			return true
		}
	}
	return false
}

// loadIgnoreFile reads patterns out of one .gitignore-style file. Returns
// an empty slice if the file is missing; doesn't surface other errors
// because ignore parsing is best-effort.
func loadIgnoreFile(path string) []ignorePattern {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []ignorePattern
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "!") {
			// Negation — skip for v1.
			continue
		}
		p := ignorePattern{raw: line}
		if strings.HasSuffix(line, "/") {
			p.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if strings.HasPrefix(line, "/") {
			p.rooted = true
			line = strings.TrimPrefix(line, "/")
		} else if strings.Contains(line, "/") {
			p.rooted = true
		}
		p.pattern = line
		out = append(out, p)
	}
	return out
}

// matchIgnore is a one-shot matcher used for ad-hoc --ignore patterns
// (no leading "!" handling — same semantics as a basename-or-path glob).
func matchIgnore(pattern, rel string, isDir bool) bool {
	dirOnly := strings.HasSuffix(pattern, "/")
	if dirOnly {
		pattern = strings.TrimSuffix(pattern, "/")
		if !isDir {
			return false
		}
	}
	if strings.Contains(pattern, "/") {
		clean := filepath.ToSlash(filepath.Clean(rel))
		if ok, _ := filepath.Match(pattern, clean); ok {
			return true
		}
		return strings.HasPrefix(clean, pattern+"/")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range parts {
		if ok, _ := filepath.Match(pattern, p); ok {
			return true
		}
	}
	return false
}
