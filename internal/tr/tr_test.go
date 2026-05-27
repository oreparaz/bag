package tr

import (
	"io"
	"os"
	"strings"
	"testing"
)

func runTR(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	rIn, wIn, _ := os.Pipe()
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldIn, oldOut, oldErr := os.Stdin, os.Stdout, os.Stderr
	os.Stdin = rIn
	os.Stdout = wOut
	os.Stderr = wErr
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = oldIn, oldOut, oldErr
		rIn.Close()
		rOut.Close()
		rErr.Close()
	}()
	go func() {
		wIn.WriteString(stdin)
		wIn.Close()
	}()
	exit := Main(args)
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	er, _ := io.ReadAll(rErr)
	return exit, string(out), string(er)
}

func TestTranslateRange(t *testing.T) {
	exit, out, _ := runTR(t, "Hello World\n", "a-z", "A-Z")
	if exit != 0 {
		t.Fatalf("exit=%d", exit)
	}
	if out != "HELLO WORLD\n" {
		t.Errorf("got %q", out)
	}
}

func TestTranslateExplicit(t *testing.T) {
	_, out, _ := runTR(t, "abcdef", "abc", "xyz")
	if out != "xyzdef" {
		t.Errorf("got %q", out)
	}
}

func TestDelete(t *testing.T) {
	_, out, _ := runTR(t, "hello, world!", "-d", "aeiou")
	if out != "hll, wrld!" {
		t.Errorf("got %q", out)
	}
}

func TestSqueezeRepeats(t *testing.T) {
	_, out, _ := runTR(t, "aaabbbccc", "-s", "abc")
	if out != "abc" {
		t.Errorf("got %q", out)
	}
}

func TestSqueezeWithTranslation(t *testing.T) {
	// Translate a→b, then squeeze runs of b.
	_, out, _ := runTR(t, "aaabbb", "-s", "a", "b")
	if out != "b" {
		t.Errorf("got %q", out)
	}
}

func TestComplementDelete(t *testing.T) {
	_, out, _ := runTR(t, "abc123def", "-dc", "0-9")
	if out != "123" {
		t.Errorf("got %q", out)
	}
}

func TestCharClassAlpha(t *testing.T) {
	_, out, _ := runTR(t, "abc 123 def", "-d", "[:alpha:]")
	if out != " 123 " {
		t.Errorf("got %q", out)
	}
}

func TestCharClassDigit(t *testing.T) {
	_, out, _ := runTR(t, "abc123def", "-d", "[:digit:]")
	if out != "abcdef" {
		t.Errorf("got %q", out)
	}
}

func TestUpperLowerClass(t *testing.T) {
	_, out, _ := runTR(t, "Hello World", "[:lower:]", "[:upper:]")
	if out != "HELLO WORLD" {
		t.Errorf("got %q", out)
	}
}

func TestEscapeSequences(t *testing.T) {
	_, out, _ := runTR(t, "a\tb\tc", "\\t", " ")
	if out != "a b c" {
		t.Errorf("got %q", out)
	}
}

func TestOctalEscape(t *testing.T) {
	_, out, _ := runTR(t, "abc", "a", "\\101") // \101 = 'A'
	if out != "Abc" {
		t.Errorf("got %q", out)
	}
}

func TestNewlineRemoval(t *testing.T) {
	_, out, _ := runTR(t, "a\nb\nc\n", "-d", "\\n")
	if out != "abc" {
		t.Errorf("got %q", out)
	}
}

func TestMissingSet2(t *testing.T) {
	exit, _, er := runTR(t, "abc", "abc")
	if exit == 0 {
		t.Errorf("expected error when SET2 omitted without -d/-s")
	}
	if !strings.Contains(er, "SET2") {
		t.Errorf("expected diagnostic, got %s", er)
	}
}

func TestBinarySafe(t *testing.T) {
	_, out, _ := runTR(t, string([]byte{0x00, 0xff, 0x00, 0xff}), "\\000", "X")
	if out != "X\xffX\xff" {
		t.Errorf("got %x", out)
	}
}
