package vi

import "testing"

func spansContain(spans []Span, kind SpanKind, text, line string) bool {
	for _, s := range spans {
		if s.Kind == kind && line[s.Start:s.End] == text {
			return true
		}
	}
	return false
}

func TestGoSyntaxKeywords(t *testing.T) {
	line := `	if x := 1; x != nil {`
	spans := goSyntax{}.Tokenize(line)
	for _, kw := range []string{"if"} {
		if !spansContain(spans, SpanKeyword, kw, line) {
			t.Errorf("expected keyword %q in spans %+v", kw, spans)
		}
	}
}

func TestGoSyntaxStringAndComment(t *testing.T) {
	line := `	greet := "hi" // friendly`
	spans := goSyntax{}.Tokenize(line)
	if !spansContain(spans, SpanString, `"hi"`, line) {
		t.Errorf("string span missing: %+v", spans)
	}
	if !spansContain(spans, SpanComment, "// friendly", line) {
		t.Errorf("comment span missing: %+v", spans)
	}
}

func TestGoSyntaxNumber(t *testing.T) {
	line := `	x := 0xdead + 42`
	spans := goSyntax{}.Tokenize(line)
	if !spansContain(spans, SpanNumber, "0xdead", line) {
		t.Errorf("hex number missing: %+v", spans)
	}
	if !spansContain(spans, SpanNumber, "42", line) {
		t.Errorf("decimal number missing: %+v", spans)
	}
}

func TestPythonSyntax(t *testing.T) {
	line := `def hello(x): return "world" # docstring`
	spans := pythonSyntax{}.Tokenize(line)
	if !spansContain(spans, SpanKeyword, "def", line) {
		t.Errorf("def missing: %+v", spans)
	}
	if !spansContain(spans, SpanKeyword, "return", line) {
		t.Errorf("return missing: %+v", spans)
	}
	if !spansContain(spans, SpanString, `"world"`, line) {
		t.Errorf("string missing: %+v", spans)
	}
	if !spansContain(spans, SpanComment, "# docstring", line) {
		t.Errorf("comment missing: %+v", spans)
	}
}

func TestShellSyntax(t *testing.T) {
	line := `if [ -f "$f" ]; then echo $f; fi # done`
	spans := shellSyntax{}.Tokenize(line)
	if !spansContain(spans, SpanKeyword, "if", line) {
		t.Errorf("if missing: %+v", spans)
	}
	if !spansContain(spans, SpanComment, "# done", line) {
		t.Errorf("comment missing: %+v", spans)
	}
}

func TestPickSyntaxByExtension(t *testing.T) {
	cases := map[string]string{
		"foo.go":  "go",
		"foo.py":  "py",
		"foo.js":  "js",
		"foo.cpp": "c",
		"foo.sh":  "sh",
		"foo.txt": "generic",
	}
	for path, want := range cases {
		got := pickSyntax(path)
		switch got.(type) {
		case goSyntax:
			if want != "go" {
				t.Errorf("%s: got go", path)
			}
		case pythonSyntax:
			if want != "py" {
				t.Errorf("%s: got py", path)
			}
		case cFamilySyntax:
			// Both .js and .cpp map to cFamilySyntax with different
			// keyword tables, that's fine for this test.
			if want != "js" && want != "c" {
				t.Errorf("%s: got cFamilySyntax", path)
			}
		case shellSyntax:
			if want != "sh" {
				t.Errorf("%s: got sh", path)
			}
		case genericSyntax:
			if want != "generic" {
				t.Errorf("%s: got generic", path)
			}
		}
	}
}

func TestStringEscapes(t *testing.T) {
	line := `s := "esc \"inner\" rest"`
	spans := goSyntax{}.Tokenize(line)
	want := `"esc \"inner\" rest"`
	if !spansContain(spans, SpanString, want, line) {
		t.Errorf("string not detected with embedded escapes: %+v", spans)
	}
}

func TestNoFalsePositiveKeywordPrefix(t *testing.T) {
	// "ifoo" must not be highlighted just because "if" is a keyword.
	line := `ifoo := 1`
	spans := goSyntax{}.Tokenize(line)
	for _, s := range spans {
		if s.Kind == SpanKeyword && line[s.Start:s.End] == "if" && s.End < len(line) && isIdentPart(line[s.End]) {
			t.Errorf("identifier prefix %q wrongly highlighted: %+v", line[s.Start:s.End], spans)
		}
	}
}
