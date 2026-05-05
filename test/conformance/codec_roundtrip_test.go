package conformance

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// TestCodecRoundtrips runs a 4-way compress/decompress matrix for each
// codec (gzip, bzip2, xz, zstd) against the system tool and bag's
// drop-in. We test:
//
//	system-compress  -> system-decompress  (sanity for the test setup)
//	system-compress  -> bag-decompress     (bag reads what system writes)
//	bag-compress     -> system-decompress  (system reads what bag writes)
//	bag-compress     -> bag-decompress     (closure)
//
// Each cell runs against a small text payload and a 64 KB random binary
// payload. Bytes must round-trip exactly.
func TestCodecRoundtrips(t *testing.T) {
	type codec struct {
		name        string // shown in subtest names
		realCompress []string
		realDecompress []string
		bagCompress []string
		bagDecompress []string
	}

	bag := bagBinForCodec(t)

	codecs := []codec{
		{
			name:           "gzip",
			realCompress:   []string{"gzip", "-c"},
			realDecompress: []string{"gunzip", "-c"},
			bagCompress:    []string{bag, "gzip", "-c"},
			bagDecompress:  []string{bag, "gunzip", "-c"},
		},
		{
			name:           "bzip2",
			realCompress:   []string{"bzip2", "-c"},
			realDecompress: []string{"bunzip2", "-c"},
			bagCompress:    []string{bag, "bzip2", "-c"},
			bagDecompress:  []string{bag, "bunzip2", "-c"},
		},
		{
			name:           "xz",
			realCompress:   []string{"xz", "-c"},
			realDecompress: []string{"unxz", "-c"},
			bagCompress:    []string{bag, "xz", "-c"},
			bagDecompress:  []string{bag, "unxz", "-c"},
		},
		{
			name:           "zstd",
			realCompress:   []string{"zstd", "-c"},
			realDecompress: []string{"unzstd", "-c"},
			bagCompress:    []string{bag, "zstd", "-c"},
			bagDecompress:  []string{bag, "unzstd", "-c"},
		},
	}

	binPayload := randomBytes(64 * 1024)
	payloads := map[string][]byte{
		"text": []byte(strings.Repeat("the quick brown fox jumps over the lazy dog\n", 50)),
		"bin":  binPayload,
	}

	for _, c := range codecs {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if !haveBinary(c.realCompress[0]) {
				t.Skipf("system %s not available", c.realCompress[0])
			}
			if !haveBinary(c.realDecompress[0]) {
				t.Skipf("system %s not available", c.realDecompress[0])
			}
			for _, mode := range []struct {
				label string
				comp  []string
				decomp []string
			}{
				{"sys_to_sys", c.realCompress, c.realDecompress},
				{"sys_to_bag", c.realCompress, c.bagDecompress},
				{"bag_to_sys", c.bagCompress, c.realDecompress},
				{"bag_to_bag", c.bagCompress, c.bagDecompress},
			} {
				mode := mode
				for payloadName, payload := range payloads {
					payloadName := payloadName
					payload := payload
					t.Run(mode.label+"/"+payloadName, func(t *testing.T) {
						compressed, err := pipeCmd(mode.comp, payload)
						if err != nil {
							t.Fatalf("compress with %v: %v", mode.comp, err)
						}
						decompressed, err := pipeCmd(mode.decomp, compressed)
						if err != nil {
							t.Fatalf("decompress with %v: %v", mode.decomp, err)
						}
						if !bytes.Equal(decompressed, payload) {
							t.Errorf("roundtrip mismatch (in=%d, out=%d)", len(payload), len(decompressed))
						}
					})
				}
			}
		})
	}
}

func bagBinForCodec(t *testing.T) string {
	t.Helper()
	if sharedBag.path == "" {
		t.Fatalf("bag binary not built")
	}
	return sharedBag.path
}

func haveBinary(name string) bool {
	if name == sharedBag.path {
		return true
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// pipeCmd runs argv with stdin=in, returns stdout. Errors include stderr.
func pipeCmd(argv []string, in []byte) ([]byte, error) {
	ctx, cancel := newTimeout()
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%v: %v: %s", argv, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = io.ReadFull(rand.Reader, b)
	return b
}
