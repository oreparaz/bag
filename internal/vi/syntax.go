package vi

import (
	"path/filepath"
	"strings"
)

// SpanKind classifies a slice of a line for highlighting. The terminal
// driver maps each kind to an ANSI sequence.
type SpanKind int

const (
	SpanPlain SpanKind = iota
	SpanComment
	SpanString
	SpanKeyword
	SpanNumber
)

// Span is a half-open byte range [Start, End) on a line, with a kind.
type Span struct {
	Start int
	End   int
	Kind  SpanKind
}

// Syntax tokenises one line at a time. Implementations are stateless
// across lines for this minimal version — multi-line constructs such as
// /* ... */ block comments are not perfectly highlighted across line
// boundaries; we mark only the in-line portion.
type Syntax interface {
	Tokenize(line string) []Span
}

// pickSyntax returns a Syntax appropriate for path's extension.
func pickSyntax(path string) Syntax {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return goSyntax{}
	case ".py":
		return pythonSyntax{}
	case ".js", ".ts", ".jsx", ".tsx":
		return cFamilySyntax{
			keywords: jsKeywords,
			lineComment: "//",
		}
	case ".c", ".h", ".cc", ".cpp", ".hpp":
		return cFamilySyntax{
			keywords: cKeywords,
			lineComment: "//",
		}
	case ".sh", ".bash":
		return shellSyntax{}
	}
	return genericSyntax{}
}

// --- generic ----------------------------------------------------------

type genericSyntax struct{}

func (genericSyntax) Tokenize(line string) []Span {
	return tokenizeStringsAndNumbers(line, "")
}

// tokenizeStringsAndNumbers handles ", ', and decimal numbers. lineComment
// is the optional comment prefix (e.g. "//" or "#").
func tokenizeStringsAndNumbers(line string, lineComment string) []Span {
	var spans []Span
	i := 0
	for i < len(line) {
		// Line comment.
		if lineComment != "" && strings.HasPrefix(line[i:], lineComment) {
			spans = append(spans, Span{Start: i, End: len(line), Kind: SpanComment})
			return spans
		}
		c := line[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			end := scanString(line, i, c)
			spans = append(spans, Span{Start: i, End: end, Kind: SpanString})
			i = end
			continue
		case c >= '0' && c <= '9':
			end := i + 1
			for end < len(line) && (isDigit(line[end]) || line[end] == '.' || line[end] == 'x' || line[end] == 'X' || isHex(line[end])) {
				end++
			}
			spans = append(spans, Span{Start: i, End: end, Kind: SpanNumber})
			i = end
			continue
		}
		i++
	}
	return spans
}

// scanString returns the first index AFTER the closing quote, or len(line)
// if unterminated. Handles backslash escapes.
func scanString(line string, start int, quote byte) int {
	i := start + 1
	for i < len(line) {
		if line[i] == '\\' && i+1 < len(line) {
			i += 2
			continue
		}
		if line[i] == quote {
			return i + 1
		}
		i++
	}
	return len(line)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isHex(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// --- Go ---------------------------------------------------------------

type goSyntax struct{}

var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true,
	"continue": true, "default": true, "defer": true, "else": true,
	"fallthrough": true, "for": true, "func": true, "go": true,
	"goto": true, "if": true, "import": true, "interface": true,
	"map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true,
	"var": true,
	// constants commonly highlighted as keywords:
	"true": true, "false": true, "nil": true, "iota": true,
}

func (goSyntax) Tokenize(line string) []Span {
	return tokenizeWithKeywords(line, "//", goKeywords)
}

// --- Python -----------------------------------------------------------

type pythonSyntax struct{}

var pythonKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true,
	"assert": true, "async": true, "await": true, "break": true,
	"class": true, "continue": true, "def": true, "del": true,
	"elif": true, "else": true, "except": true, "finally": true,
	"for": true, "from": true, "global": true, "if": true,
	"import": true, "in": true, "is": true, "lambda": true,
	"nonlocal": true, "not": true, "or": true, "pass": true,
	"raise": true, "return": true, "try": true, "while": true,
	"with": true, "yield": true,
}

func (pythonSyntax) Tokenize(line string) []Span {
	return tokenizeWithKeywords(line, "#", pythonKeywords)
}

// --- C / C++ / JS / TS ------------------------------------------------

type cFamilySyntax struct {
	keywords    map[string]bool
	lineComment string
}

var cKeywords = map[string]bool{
	"auto": true, "break": true, "case": true, "char": true,
	"const": true, "continue": true, "default": true, "do": true,
	"double": true, "else": true, "enum": true, "extern": true,
	"float": true, "for": true, "goto": true, "if": true,
	"int": true, "long": true, "register": true, "return": true,
	"short": true, "signed": true, "sizeof": true, "static": true,
	"struct": true, "switch": true, "typedef": true, "union": true,
	"unsigned": true, "void": true, "volatile": true, "while": true,
	// C++:
	"bool": true, "class": true, "namespace": true, "new": true,
	"nullptr": true, "private": true, "protected": true,
	"public": true, "this": true, "true": true, "false": true,
	"using": true, "virtual": true,
}

var jsKeywords = map[string]bool{
	"break": true, "case": true, "catch": true, "class": true,
	"const": true, "continue": true, "debugger": true, "default": true,
	"delete": true, "do": true, "else": true, "enum": true,
	"export": true, "extends": true, "false": true, "finally": true,
	"for": true, "function": true, "if": true, "import": true,
	"in": true, "instanceof": true, "let": true, "new": true,
	"null": true, "of": true, "return": true, "super": true,
	"switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "undefined": true, "var": true,
	"void": true, "while": true, "with": true, "yield": true,
	"async": true, "await": true,
}

func (s cFamilySyntax) Tokenize(line string) []Span {
	return tokenizeWithKeywords(line, s.lineComment, s.keywords)
}

// --- shell ------------------------------------------------------------

type shellSyntax struct{}

var shKeywords = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "fi": true,
	"case": true, "esac": true, "for": true, "while": true,
	"until": true, "do": true, "done": true, "in": true,
	"return": true, "function": true, "select": true,
	"local": true, "export": true,
}

func (shellSyntax) Tokenize(line string) []Span {
	return tokenizeWithKeywords(line, "#", shKeywords)
}

// --- shared tokenizer -------------------------------------------------

// tokenizeWithKeywords scans line emitting comment / string / number /
// keyword spans. Plain text is left ungapped — the renderer fills in
// "plain" between spans.
func tokenizeWithKeywords(line string, lineComment string, keywords map[string]bool) []Span {
	var spans []Span
	i := 0
	for i < len(line) {
		// Line comment: rest of the line.
		if lineComment != "" && strings.HasPrefix(line[i:], lineComment) {
			spans = append(spans, Span{Start: i, End: len(line), Kind: SpanComment})
			return spans
		}
		c := line[i]

		// String.
		if c == '"' || c == '\'' || c == '`' {
			end := scanString(line, i, c)
			spans = append(spans, Span{Start: i, End: end, Kind: SpanString})
			i = end
			continue
		}

		// Number.
		if c >= '0' && c <= '9' {
			end := i + 1
			for end < len(line) && (isDigit(line[end]) || line[end] == '.' || line[end] == 'x' || line[end] == 'X' || isHex(line[end])) {
				end++
			}
			spans = append(spans, Span{Start: i, End: end, Kind: SpanNumber})
			i = end
			continue
		}

		// Identifier / keyword.
		if isIdentStart(c) {
			end := i + 1
			for end < len(line) && isIdentPart(line[end]) {
				end++
			}
			word := line[i:end]
			if keywords[word] {
				spans = append(spans, Span{Start: i, End: end, Kind: SpanKeyword})
			}
			i = end
			continue
		}

		i++
	}
	return spans
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}
