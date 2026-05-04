package xxd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runXXD(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestDefaultDump(t *testing.T) {
	_, out := runXXD(t, []byte("hello world\n"))
	want := "00000000: 6865 6c6c 6f20 776f 726c 640a            hello world.\n"
	if out != want {
		t.Errorf("got %q want %q", out, want)
	}
}

func TestPlain(t *testing.T) {
	_, out := runXXD(t, []byte("hello"), "-p")
	if out != "68656c6c6f\n" {
		t.Errorf("got %q", out)
	}
}

func TestUpper(t *testing.T) {
	_, out := runXXD(t, []byte{0xab, 0xcd}, "-u", "-p")
	if out != "ABCD\n" {
		t.Errorf("got %q", out)
	}
}

func TestRevert(t *testing.T) {
	in := "00000000: 6865 6c6c 6f20 776f 726c 640a            hello world.\n"
	_, out := runXXD(t, []byte(in), "-r")
	if out != "hello world\n" {
		t.Errorf("got %q", out)
	}
}

func TestRevertPlain(t *testing.T) {
	_, out := runXXD(t, []byte("68656c6c6f\n"), "-r", "-p")
	if out != "hello" {
		t.Errorf("got %q", out)
	}
}

func TestRoundTrip(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	p := writeTmp(t, "blob", data)
	_, dumped := runXXD(t, nil, p)
	_, reverted := runXXD(t, []byte(dumped), "-r")
	if !bytes.Equal([]byte(reverted), data) {
		t.Errorf("roundtrip failed")
	}
}

func TestRoundTripPlain(t *testing.T) {
	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	p := writeTmp(t, "blob", data)
	_, dumped := runXXD(t, nil, "-p", p)
	_, reverted := runXXD(t, []byte(dumped), "-r", "-p")
	if !bytes.Equal([]byte(reverted), data) {
		t.Errorf("plain roundtrip failed")
	}
}

func TestSkip(t *testing.T) {
	p := writeTmp(t, "blob", []byte("0123456789"))
	_, out := runXXD(t, nil, "-s", "5", "-p", p)
	if !strings.HasPrefix(out, "3536373839") {
		t.Errorf("got %q", out)
	}
}

func TestLimit(t *testing.T) {
	_, out := runXXD(t, []byte("abcdefghij"), "-l", "3", "-p")
	if out != "616263\n" {
		t.Errorf("got %q", out)
	}
}

func TestCols(t *testing.T) {
	_, out := runXXD(t, []byte("0123456789"), "-c", "5")
	// Two rows of 5 bytes each.
	if !strings.Contains(out, "00000000: 3031 3233 34") {
		t.Errorf("first row missing in %q", out)
	}
	if !strings.Contains(out, "00000005: 3536 3738 39") {
		t.Errorf("second row missing in %q", out)
	}
}

func TestGroup(t *testing.T) {
	_, out := runXXD(t, []byte("01234567"), "-g", "4")
	// 4-byte groups: "30313233 34353637"
	if !strings.Contains(out, "30313233 34353637") {
		t.Errorf("group=4 not honored: %q", out)
	}
}

func TestNonexistent(t *testing.T) {
	exit, _ := runXXD(t, nil, "/no/such/file")
	if exit == 0 {
		t.Errorf("exit=0; expected non-zero")
	}
}
