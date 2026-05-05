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

// TestZipRoundtrips: 4-way matrix (sys/bag) for zip create + unzip extract.
func TestZipRoundtrips(t *testing.T) {
	bag := bagBinForCodec(t)
	if !haveBinary("zip") || !haveBinary("unzip") {
		t.Skip("system zip/unzip not available")
	}

	for _, m := range []struct {
		label  string
		zipper []string
		unzipper []string
	}{
		{"sys_to_sys", []string{"zip"}, []string{"unzip"}},
		{"sys_to_bag", []string{"zip"}, []string{bag, "unzip"}},
		{"bag_to_sys", []string{bag, "zip"}, []string{"unzip"}},
		{"bag_to_bag", []string{bag, "zip"}, []string{bag, "unzip"}},
	} {
		m := m
		t.Run(m.label, func(t *testing.T) {
			work := t.TempDir()
			src := filepath.Join(work, "src")
			tree := map[string]string{
				"a.txt":         "AAA\n",
				"sub/inner.txt": "deep\n",
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

			archive := filepath.Join(work, "out.zip")

			// Create. zip / bag zip both want CWD-relative paths to keep
			// entry names consistent across implementations.
			argv := append(append([]string{}, m.zipper...), "-qr", archive, "src")
			if err := runShellInDir(argv, work); err != nil {
				t.Fatalf("zip with %v: %v", argv, err)
			}

			// Extract.
			dest := filepath.Join(work, "dest")
			os.MkdirAll(dest, 0o755)
			argv = append(append([]string{}, m.unzipper...), "-q", archive, "-d", dest)
			if err := runShell(argv); err != nil {
				t.Fatalf("unzip with %v: %v", argv, err)
			}

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
		})
	}
}

func runShellInDir(argv []string, cwd string) error {
	ctx, cancel := newTimeout()
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = cwd
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%v: %v: %s", argv, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
