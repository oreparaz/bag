package cmpressor

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/oreparaz/bag/internal/compress"
)

// runCmpressor runs Main with the given Tool definition, feeding stdin and
// capturing stdout.
func runCmpressor(t *testing.T, tool Tool, stdin []byte, args ...string) (int, []byte) {
	t.Helper()
	rOut, wOut, _ := os.Pipe()
	oldOut := os.Stdout
	os.Stdout = wOut
	if stdin != nil {
		oldIn := os.Stdin
		rIn, wIn, _ := os.Pipe()
		os.Stdin = rIn
		go func() { wIn.Write(stdin); wIn.Close() }()
		defer func() { os.Stdin = oldIn }()
	}
	exit := Main(tool, args)
	wOut.Close()
	os.Stdout = oldOut
	out, _ := io.ReadAll(rOut)
	return exit, out
}

// roundTripStdin compresses the given payload with formatTool then immediately
// decompresses through the matching reader. Verifies bytes equal.
func roundTripStdin(t *testing.T, format compress.Format, payload []byte) {
	t.Helper()
	encTool := Tool{Name: "enc", Format: format}
	exit, encoded := runCmpressor(t, encTool, payload, "-c")
	if exit != 0 {
		t.Fatalf("compress exit=%d", exit)
	}
	r, err := compress.NewReader(format, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decompress reader: %v", err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("decompress read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("roundtrip mismatch (format=%s, len in=%d, len out=%d)", format, len(payload), len(got))
	}
}

func TestRoundTripGzip(t *testing.T)  { roundTripStdin(t, compress.FormatGzip, []byte("hello world\n")) }
func TestRoundTripBzip2(t *testing.T) { roundTripStdin(t, compress.FormatBzip2, []byte("hello world\n")) }
func TestRoundTripXZ(t *testing.T)    { roundTripStdin(t, compress.FormatXZ, []byte("hello world\n")) }
func TestRoundTripZstd(t *testing.T)  { roundTripStdin(t, compress.FormatZstd, []byte("hello world\n")) }

func TestRoundTripBinaryGzip(t *testing.T) {
	data := make([]byte, 65536)
	for i := range data {
		data[i] = byte(i * 7)
	}
	roundTripStdin(t, compress.FormatGzip, data)
}

func TestRoundTripBinaryZstd(t *testing.T) {
	data := make([]byte, 65536)
	for i := range data {
		data[i] = byte(i * 11)
	}
	roundTripStdin(t, compress.FormatZstd, data)
}

func TestDecompressViaTool(t *testing.T) {
	// Encode externally (via NewWriter), decode via the gunzip alias.
	payload := []byte("the quick brown fox\n")
	var buf bytes.Buffer
	w, _ := compress.NewWriter(compress.FormatGzip, &buf, 0)
	w.Write(payload)
	w.Close()

	tool := Tool{Name: "gunzip", Format: compress.FormatGzip, DefaultDecompress: true}
	exit, out := runCmpressor(t, tool, buf.Bytes(), "-c")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.Equal(out, payload) {
		t.Errorf("decoded=%q want %q", out, payload)
	}
}

func TestZcatAlwaysStdout(t *testing.T) {
	payload := []byte("zcat\n")
	var buf bytes.Buffer
	w, _ := compress.NewWriter(compress.FormatGzip, &buf, 0)
	w.Write(payload)
	w.Close()

	// zcat: AlwaysStdout=true means -c is implied.
	tool := Tool{Name: "zcat", Format: compress.FormatGzip, DefaultDecompress: true, AlwaysStdout: true}
	exit, out := runCmpressor(t, tool, buf.Bytes())
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if !bytes.Equal(out, payload) {
		t.Errorf("decoded=%q", out)
	}
}

func TestFileMode(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "data.txt")
	os.WriteFile(src, []byte("file mode\n"), 0o644)

	tool := Tool{Name: "gzip", Format: compress.FormatGzip}
	exit := Main(tool, []string{"-k", src})
	if exit != 0 {
		t.Fatalf("compress exit=%d", exit)
	}
	out := src + ".gz"
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected %s: %v", out, err)
	}
	// -k keeps the original.
	if _, err := os.Stat(src); err != nil {
		t.Errorf("-k should keep source: %v", err)
	}

	// Now decompress without -k: source removed, .gz removed, leaving the original.
	os.Remove(src) // remove the original so decompress writes back to data.txt
	dt := Tool{Name: "gunzip", Format: compress.FormatGzip, DefaultDecompress: true}
	exit = Main(dt, []string{out})
	if exit != 0 {
		t.Fatalf("decompress exit=%d", exit)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("expected reconstructed %s: %v", src, err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("expected %s to be removed", out)
	}
}

func TestRefuseOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "data.txt")
	os.WriteFile(src, []byte("x\n"), 0o644)
	os.WriteFile(src+".gz", []byte("preexisting"), 0o644)
	tool := Tool{Name: "gzip", Format: compress.FormatGzip}
	exit := Main(tool, []string{src})
	if exit == 0 {
		t.Errorf("expected non-zero exit when output exists without -f")
	}
}

func TestForceOverwrite(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "data.txt")
	os.WriteFile(src, []byte("x\n"), 0o644)
	os.WriteFile(src+".gz", []byte("preexisting"), 0o644)
	tool := Tool{Name: "gzip", Format: compress.FormatGzip}
	exit := Main(tool, []string{"-f", src})
	if exit != 0 {
		t.Errorf("exit=%d with -f", exit)
	}
}

func TestStripExtension(t *testing.T) {
	cases := map[string]string{
		"a.gz":         "a",
		"foo/bar.gz":   "foo/bar",
		"archive.tgz":  "archive.tar",
		"archive.tbz":  "archive.tar",
		"archive.txz":  "archive.tar",
		"archive.tzst": "archive.tar",
	}
	for in, want := range cases {
		var t1 Tool
		switch {
		case wantIs(in, ".gz", ".tgz"):
			t1 = Tool{Format: compress.FormatGzip}
		case wantIs(in, ".tbz", ".tbz2", ".bz2"):
			t1 = Tool{Format: compress.FormatBzip2}
		case wantIs(in, ".xz", ".txz"):
			t1 = Tool{Format: compress.FormatXZ}
		case wantIs(in, ".zst", ".tzst"):
			t1 = Tool{Format: compress.FormatZstd}
		}
		got, err := stripExt(t1, in)
		if err != nil {
			t.Errorf("stripExt(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("stripExt(%q) = %q want %q", in, got, want)
		}
	}
}

func wantIs(s string, exts ...string) bool {
	for _, e := range exts {
		if len(s) >= len(e) && s[len(s)-len(e):] == e {
			return true
		}
	}
	return false
}

func TestUnknownFlag(t *testing.T) {
	tool := Tool{Name: "gzip", Format: compress.FormatGzip}
	exit, _ := runCmpressor(t, tool, []byte("x\n"), "--no-such-flag")
	if exit == 0 {
		t.Errorf("expected non-zero exit on unknown flag")
	}
}
