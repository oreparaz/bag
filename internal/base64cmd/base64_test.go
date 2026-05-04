package base64cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runB64(t *testing.T, stdin []byte, args ...string) (int, string) {
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
	exit := Main(args)
	wOut.Close()
	os.Stdout = oldOut
	out, _ := io.ReadAll(rOut)
	return exit, string(out)
}

func writeTmp(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEncodeDefault(t *testing.T) {
	exit, out := runB64(t, []byte("hello"))
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "aGVsbG8=\n" {
		t.Errorf("got %q", out)
	}
}

func TestEncodeWrap0(t *testing.T) {
	// Long input that would normally wrap at 76 cols.
	in := bytes.Repeat([]byte{'A'}, 100)
	exit, out := runB64(t, in, "-w", "0")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	// GNU base64 -w 0: encoded bytes only, no trailing newline.
	// Encoded length: ceil(100/3)*4 = 136.
	if len(out) != 136 {
		t.Errorf("len=%d want 136", len(out))
	}
	if strings.Contains(out, "\n") {
		t.Errorf("wrap=0 should produce no newlines: %q", out)
	}
}

func TestEncodeWrap10(t *testing.T) {
	in := bytes.Repeat([]byte{'A'}, 30) // 40 base64 chars, no padding
	_, out := runB64(t, in, "-w", "10")
	want := "QUFBQUFBQU\nFBQUFBQUFB\nQUFBQUFBQU\nFBQUFBQUFB\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestEncodeFromFile(t *testing.T) {
	p := writeTmp(t, "in.txt", []byte("foobar"))
	exit, out := runB64(t, nil, p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "Zm9vYmFy\n" {
		t.Errorf("got %q", out)
	}
}

func TestDecode(t *testing.T) {
	exit, out := runB64(t, []byte("aGVsbG8=\n"), "-d")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestDecodeMultiLine(t *testing.T) {
	exit, out := runB64(t, []byte("Zm9v\nYmFy\n"), "--decode")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "foobar" {
		t.Errorf("got %q", out)
	}
}

func TestDecodeIgnoreGarbage(t *testing.T) {
	in := []byte("aG**Vs\tbG8\n=\n")
	exit, out := runB64(t, in, "-d", "-i")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestDecodeRejectGarbageWithoutFlag(t *testing.T) {
	exit, _ := runB64(t, []byte("not!base64"), "-d")
	if exit == 0 {
		t.Errorf("exit=0; expected error on bad base64")
	}
}

func TestRoundTripBinary(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	_, encoded := runB64(t, data)
	_, decoded := runB64(t, []byte(encoded), "-d")
	if !bytes.Equal([]byte(decoded), data) {
		t.Errorf("roundtrip mismatch")
	}
}

func TestEmptyInputEncode(t *testing.T) {
	_, out := runB64(t, []byte(""))
	if out != "" {
		t.Errorf("expected empty output got %q", out)
	}
}

func TestNonexistentFile(t *testing.T) {
	exit, _ := runB64(t, nil, "/no/such/file")
	if exit == 0 {
		t.Errorf("exit=0; expected non-zero")
	}
}
