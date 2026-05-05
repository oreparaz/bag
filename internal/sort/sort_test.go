package sort

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func runSort(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestLexicalDefault(t *testing.T) {
	_, out := runSort(t, []byte("banana\napple\ncherry\n"))
	if out != "apple\nbanana\ncherry\n" {
		t.Errorf("got %q", out)
	}
}

func TestNumericSort(t *testing.T) {
	_, out := runSort(t, []byte("10\n2\n1\n11\n"), "-n")
	if out != "1\n2\n10\n11\n" {
		t.Errorf("got %q", out)
	}
}

func TestReverse(t *testing.T) {
	_, out := runSort(t, []byte("a\nb\nc\n"), "-r")
	if out != "c\nb\na\n" {
		t.Errorf("got %q", out)
	}
}

func TestUnique(t *testing.T) {
	_, out := runSort(t, []byte("z\na\nz\na\n"), "-u")
	if out != "a\nz\n" {
		t.Errorf("got %q", out)
	}
}

func TestIgnoreCase(t *testing.T) {
	_, out := runSort(t, []byte("Banana\napple\nCherry\n"), "-f")
	if out != "apple\nBanana\nCherry\n" {
		t.Errorf("got %q", out)
	}
}

func TestKeySimple(t *testing.T) {
	in := "1 zebra\n2 apple\n3 mango\n"
	_, out := runSort(t, []byte(in), "-k", "2")
	if out != "2 apple\n3 mango\n1 zebra\n" {
		t.Errorf("got %q", out)
	}
}

func TestKeyNumeric(t *testing.T) {
	in := "alpha 100\nbeta 9\ngamma 50\n"
	_, out := runSort(t, []byte(in), "-k", "2n")
	if out != "beta 9\ngamma 50\nalpha 100\n" {
		t.Errorf("got %q", out)
	}
}

func TestSeparator(t *testing.T) {
	in := "z:1\na:2\nm:3\n"
	_, out := runSort(t, []byte(in), "-t:", "-k", "1")
	if out != "a:2\nm:3\nz:1\n" {
		t.Errorf("got %q", out)
	}
}

func TestOutput(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "out")
	exit, _ := runSort(t, []byte("c\na\nb\n"), "-o", p)
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	body, _ := os.ReadFile(p)
	if string(body) != "a\nb\nc\n" {
		t.Errorf("got %q", body)
	}
}

func TestCheckSorted(t *testing.T) {
	exit, _ := runSort(t, []byte("a\nb\nc\n"), "-c")
	if exit != 0 {
		t.Errorf("expected 0; got %d", exit)
	}
}

func TestCheckUnsorted(t *testing.T) {
	exit, _ := runSort(t, []byte("c\nb\na\n"), "-c")
	if exit == 0 {
		t.Errorf("expected non-zero on unsorted input")
	}
}

func TestStableOrderPreservedForEqualKeys(t *testing.T) {
	in := "1 zeta\n1 alpha\n1 beta\n"
	_, out := runSort(t, []byte(in), "-k", "1n")
	if out != in {
		t.Errorf("expected stable order kept; got %q", out)
	}
}
