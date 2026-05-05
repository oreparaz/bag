package cut

import (
	"io"
	"os"
	"testing"
)

func runCut(t *testing.T, stdin []byte, args ...string) (int, string) {
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

func TestFields(t *testing.T) {
	_, out := runCut(t, []byte("a:b:c\nd:e:f\n"), "-d:", "-f2")
	if out != "b\ne\n" {
		t.Errorf("got %q", out)
	}
}

func TestFieldsRange(t *testing.T) {
	_, out := runCut(t, []byte("a:b:c:d\n"), "-d:", "-f", "2-3")
	if out != "b:c\n" {
		t.Errorf("got %q", out)
	}
}

func TestFieldsOpenEnd(t *testing.T) {
	_, out := runCut(t, []byte("a:b:c:d\n"), "-d:", "-f", "2-")
	if out != "b:c:d\n" {
		t.Errorf("got %q", out)
	}
}

func TestCharacters(t *testing.T) {
	_, out := runCut(t, []byte("abcdef\n"), "-c", "2-4")
	if out != "bcd\n" {
		t.Errorf("got %q", out)
	}
}

func TestComplement(t *testing.T) {
	_, out := runCut(t, []byte("abcdef\n"), "-c", "2-4", "--complement")
	if out != "aef\n" {
		t.Errorf("got %q", out)
	}
}

func TestSkipNoDelim(t *testing.T) {
	_, out := runCut(t, []byte("a:b\nno_delim\n"), "-d:", "-f1", "-s")
	if out != "a\n" {
		t.Errorf("got %q", out)
	}
}

func TestOutputDelimiter(t *testing.T) {
	_, out := runCut(t, []byte("a:b:c\n"), "-d:", "-f", "1,3", "--output-delimiter", ",")
	if out != "a,c\n" {
		t.Errorf("got %q", out)
	}
}

func TestParseList(t *testing.T) {
	cases := []struct {
		in   string
		want []rng
	}{
		{"1", []rng{{1, 1}}},
		{"1-3", []rng{{1, 3}}},
		{"1,3-5,7-", []rng{{1, 1}, {3, 5}, {7, -1}}},
		{"-3", []rng{{1, 3}}},
	}
	for _, tc := range cases {
		got, err := parseList(tc.in)
		if err != nil {
			t.Errorf("parseList(%q): %v", tc.in, err)
			continue
		}
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %v want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q[%d]: got %v want %v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestNoMode(t *testing.T) {
	exit, _ := runCut(t, []byte("x\n"))
	if exit == 0 {
		t.Errorf("expected non-zero without mode")
	}
}
