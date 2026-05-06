// Package safefs centralises the filesystem hardening rules used by bag's
// archive and codec tools. The goal is to make it hard to accidentally
// follow attacker-controlled symlinks or write outside an intended root.
//
// The rules:
//
//  1. Path traversal: reject names that are absolute or contain "..".
//  2. Symlink-traversal: before writing into a directory tree, walk every
//     intermediate path component and refuse if any of them is a symlink.
//  3. Leaf opens: always pass O_NOFOLLOW so a leaf symlink can't redirect
//     a write to /etc/passwd. Combined with O_EXCL (fresh) or O_TRUNC
//     (overwrite-allowed), this matches gzip/tar/unzip's safer modes.
//
// These rules don't fully eliminate TOCTOU: between the lstat and the open
// an attacker can replace a directory with a symlink. The mitigation is
// O_NOFOLLOW on the leaf — that closes the obvious win. For full safety
// we'd switch to openat-based traversal, which Go doesn't expose
// portably; that's a known follow-up tracked in FUTURE.md.
package safefs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrPathEscape is returned when a path is absolute or contains a ".." segment.
var ErrPathEscape = errors.New("path escapes output directory")

// RefusePathTraversal returns ErrPathEscape if name is absolute or contains
// any ".." segment after splitting on "/".
func RefusePathTraversal(name string) error {
	if name == "" {
		return nil
	}
	if filepath.IsAbs(name) {
		return fmt.Errorf("%w: %q is absolute", ErrPathEscape, name)
	}
	for _, seg := range strings.Split(filepath.ToSlash(name), "/") {
		if seg == ".." {
			return fmt.Errorf("%w: %q contains '..'", ErrPathEscape, name)
		}
	}
	return nil
}

// ErrSymlinkInPath is returned when a parent component of the target path
// is itself a symlink. We refuse to write through such a path because an
// archive could have used it to redirect a sibling write.
var ErrSymlinkInPath = errors.New("refusing to descend through symlink")

// EnsureNoSymlinkInPath walks the directory components of target between
// root (exclusive) and target (exclusive of the leaf) and returns
// ErrSymlinkInPath if any existing intermediate component is a symlink.
// Non-existent components are fine — they'll be created.
//
// root is the user's extraction directory (or "." for cwd-relative
// extracts). Components above root — for instance /var on macOS, where
// /var is a system symlink to /private/var — are NOT checked, because
// the user explicitly chose root and an attacker controls nothing
// outside it.
//
// The leaf component is NOT checked here; use O_NOFOLLOW on the open to
// guard the leaf.
func EnsureNoSymlinkInPath(root, target string) error {
	rel, err := relativeWithin(root, target)
	if err != nil {
		return err
	}
	dir := filepath.Dir(rel)
	if dir == "" || dir == "." {
		return nil
	}
	cur := filepath.Clean(root)
	for _, p := range strings.Split(dir, string(os.PathSeparator)) {
		if p == "" || p == "." {
			continue
		}
		cur = filepath.Join(cur, p)
		info, err := os.Lstat(cur)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkInPath, cur)
		}
	}
	return nil
}

// relativeWithin returns target's path relative to root, with both
// resolved to absolute paths first. Errors if target is outside root.
func relativeWithin(root, target string) (string, error) {
	rootAbs, err := absPath(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := absPath(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(filepath.Clean(rootAbs), filepath.Clean(targetAbs))
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("target %q escapes root %q", target, root)
	}
	return rel, nil
}

func absPath(p string) (string, error) {
	if filepath.IsAbs(p) {
		return p, nil
	}
	return filepath.Abs(p)
}

// CreateExcl opens path for writing, creating it. Fails with EEXIST if the
// file already exists, with ELOOP if the leaf is a symlink. Use this when
// you want a fresh write — gzip without -f, sed temp files, etc.
func CreateExcl(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, perm)
}

// CreateTrunc opens path for writing, truncating if it exists. Fails with
// ELOOP if the leaf is a symlink. Use this when you want to allow
// overwriting a regular file but not redirect through a symlink — gzip
// with -f, archive extraction, etc.
func CreateTrunc(path string, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, perm)
}

// MkdirAllNoSymlinkLeaf is os.MkdirAll, except that if the leaf
// component already exists as a symlink we refuse, and intermediate
// path components between root and path are walked for symlinks.
// (os.MkdirAll silently accepts an existing symlink-to-directory
// which is a tar-slip vector.)
func MkdirAllNoSymlinkLeaf(root, path string, perm os.FileMode) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrSymlinkInPath, path)
		}
		if info.IsDir() {
			return nil
		}
		return fmt.Errorf("not a directory: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := EnsureNoSymlinkInPath(root, path); err != nil {
		return err
	}
	return os.MkdirAll(path, perm)
}
