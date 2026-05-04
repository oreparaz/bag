package tee

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runTee(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestTeeStdoutAndFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	exit, out := runTee(t, []byte("payload\n"), p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "payload\n" {
		t.Errorf("stdout=%q", out)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "payload\n" {
		t.Errorf("file=%q", body)
	}
}

func TestTeeMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	exit, _ := runTee(t, []byte("multi\n"), a, b)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	for _, p := range []string{a, b} {
		body, _ := os.ReadFile(p)
		if string(body) != "multi\n" {
			t.Errorf("%s=%q", p, body)
		}
	}
}

func TestTeeAppend(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out.txt")
	os.WriteFile(p, []byte("first\n"), 0o644)
	exit, _ := runTee(t, []byte("second\n"), "-a", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "first\nsecond\n" {
		t.Errorf("file=%q", body)
	}
}

func TestTeeNoFilesPassthrough(t *testing.T) {
	exit, out := runTee(t, []byte("just stdout\n"))
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "just stdout\n" {
		t.Errorf("out=%q", out)
	}
}

func TestTeeUnwritableFileContinues(t *testing.T) {
	// /readonly/path/foo will fail to open; tee should still pass stdout
	// through and exit non-zero.
	exit, out := runTee(t, []byte("ok\n"), "/proc/1/no-perms-file")
	if exit == 0 {
		t.Errorf("exit=0 expected non-zero")
	}
	if out != "ok\n" {
		t.Errorf("stdout=%q", out)
	}
}
