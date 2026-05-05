package uniq

import (
	"io"
	"os"
	"testing"
)

func runUniq(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestDefault(t *testing.T) {
	_, out := runUniq(t, []byte("a\na\nb\nc\nc\nc\n"))
	if out != "a\nb\nc\n" {
		t.Errorf("got %q", out)
	}
}

func TestCount(t *testing.T) {
	_, out := runUniq(t, []byte("a\na\nb\n"), "-c")
	if out != "      2 a\n      1 b\n" {
		t.Errorf("got %q", out)
	}
}

func TestDuplicatesOnly(t *testing.T) {
	_, out := runUniq(t, []byte("a\na\nb\nc\nc\n"), "-d")
	if out != "a\nc\n" {
		t.Errorf("got %q", out)
	}
}

func TestUniqueOnly(t *testing.T) {
	_, out := runUniq(t, []byte("a\na\nb\nc\nc\n"), "-u")
	if out != "b\n" {
		t.Errorf("got %q", out)
	}
}

func TestIgnoreCase(t *testing.T) {
	_, out := runUniq(t, []byte("Foo\nFOO\nfOO\nbar\n"), "-i")
	if out != "Foo\nbar\n" {
		t.Errorf("got %q", out)
	}
}

func TestSkipFields(t *testing.T) {
	in := "1 alpha\n2 alpha\n3 beta\n"
	_, out := runUniq(t, []byte(in), "-f", "1")
	if out != "1 alpha\n3 beta\n" {
		t.Errorf("got %q", out)
	}
}

func TestSkipChars(t *testing.T) {
	in := "abc\nabd\nabe\n"
	_, out := runUniq(t, []byte(in), "-s", "2")
	// After skipping 2 chars: "c","d","e" — all distinct.
	if out != in {
		t.Errorf("got %q", out)
	}
}

func TestCheckChars(t *testing.T) {
	in := "alpha\nalpine\nbeta\n"
	_, out := runUniq(t, []byte(in), "-w", "2")
	// First 2 chars: "al","al","be" — first two collapse.
	if out != "alpha\nbeta\n" {
		t.Errorf("got %q", out)
	}
}

func TestNoFinalNewline(t *testing.T) {
	_, out := runUniq(t, []byte("a\na"))
	if out != "a\n" {
		t.Errorf("got %q", out)
	}
}
