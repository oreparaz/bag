package conformance

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestTarRoundtrips builds a small directory tree, then runs a 4-way
// matrix (sys-create + sys-extract, sys+bag, bag+sys, bag+bag) for each
// compression mode tar supports. After extraction we compare the
// reconstructed tree byte-for-byte against the original.
func TestTarRoundtrips(t *testing.T) {
	bag := bagBinForCodec(t)
	if !haveBinary("tar") {
		t.Skip("system tar not available")
	}

	type variant struct {
		name string
		// Compression flag suffix appended to "-cf"/"-xf".
		extraCreate  []string
		extraExtract []string
		// system tools each compression depends on, e.g. gzip for -z.
		needs []string
	}
	variants := []variant{
		{"plain", nil, nil, nil},
		{"gzip", []string{"-z"}, []string{"-z"}, []string{"gzip"}},
		{"bzip2", []string{"-j"}, []string{"-j"}, []string{"bzip2"}},
		{"xz", []string{"-J"}, []string{"-J"}, []string{"xz"}},
		{"zstd", []string{"--zstd"}, []string{"--zstd"}, []string{"zstd"}},
	}

	for _, v := range variants {
		v := v
		t.Run(v.name, func(t *testing.T) {
			for _, n := range v.needs {
				if !haveBinary(n) {
					t.Skipf("system %s not available", n)
				}
			}
			runMatrix(t, bag, v.extraCreate, v.extraExtract)
		})
	}
}

func runMatrix(t *testing.T, bag string, createExtra, extractExtra []string) {
	t.Helper()
	for _, m := range []struct {
		label   string
		creator []string
		extractor []string
	}{
		{"sys_to_sys", []string{"tar"}, []string{"tar"}},
		{"sys_to_bag", []string{"tar"}, []string{bag, "tar"}},
		{"bag_to_sys", []string{bag, "tar"}, []string{"tar"}},
		{"bag_to_bag", []string{bag, "tar"}, []string{bag, "tar"}},
	} {
		m := m
		t.Run(m.label, func(t *testing.T) {
			work := t.TempDir()
			src := filepath.Join(work, "src")
			tree := map[string]string{
				"file.txt":      "AAA bbb CCC\n",
				"dir/inner.txt": "deeper\n",
				"empty.txt":     "",
			}
			for path, content := range tree {
				full := filepath.Join(src, path)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			archive := filepath.Join(work, "out.tar")

			// Create.
			argv := append(append([]string{}, m.creator...),
				append([]string{"-cf", archive}, createExtra...)...)
			argv = append(argv, "-C", work, "src")
			if err := runShell(argv); err != nil {
				t.Fatalf("create with %v: %v", argv, err)
			}

			// Extract.
			dest := filepath.Join(work, "dest")
			os.MkdirAll(dest, 0o755)
			argv = append(append([]string{}, m.extractor...),
				append([]string{"-xf", archive}, extractExtra...)...)
			argv = append(argv, "-C", dest)
			if err := runShell(argv); err != nil {
				t.Fatalf("extract with %v: %v", argv, err)
			}

			// Compare.
			for path, want := range tree {
				got, err := os.ReadFile(filepath.Join(dest, "src", path))
				if err != nil {
					t.Errorf("missing %q: %v", path, err)
					continue
				}
				if !bytes.Equal(got, []byte(want)) {
					t.Errorf("%q: got %q want %q", path, got, want)
				}
			}
			// Make sure we didn't extract anything extra.
			extras := walkExtras(t, filepath.Join(dest, "src"), tree)
			if len(extras) > 0 {
				t.Errorf("unexpected extras: %v", extras)
			}
		})
	}
}

func runShell(argv []string) error {
	ctx, cancel := newTimeout()
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %v: %s", argv, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func walkExtras(t *testing.T, root string, want map[string]string) []string {
	t.Helper()
	wantSet := map[string]bool{}
	for k := range want {
		wantSet[filepath.ToSlash(k)] = true
	}
	var extras []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if !wantSet[rel] {
			extras = append(extras, rel)
		}
		return nil
	})
	return extras
}
