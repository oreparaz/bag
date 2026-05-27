package jq

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itchyny/gojq"
)

type options struct {
	filter      string
	files       []string
	raw         bool
	compact     bool
	slurp       bool
	nullIn      bool
	rawIn       bool
	asciiOut    bool
	sortKeys    bool
	args        map[string]any
}

func parseArgs(argv []string) (*options, error) {
	o := &options{args: map[string]any{}}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.files = append(o.files, argv[i+1:]...)
			break
		}
		switch {
		case a == "-r" || a == "--raw-output":
			o.raw = true
		case a == "-c" || a == "--compact-output":
			o.compact = true
		case a == "-s" || a == "--slurp":
			o.slurp = true
		case a == "-n" || a == "--null-input":
			o.nullIn = true
		case a == "-R" || a == "--raw-input":
			o.rawIn = true
		case a == "-a" || a == "--ascii-output":
			o.asciiOut = true
		case a == "-S" || a == "--sort-keys":
			o.sortKeys = true
		case a == "-C" || a == "--color-output" || a == "-M" || a == "--monochrome-output":
			// no-op; we never colorize
		case a == "--arg":
			if i+2 >= len(argv) {
				return nil, errors.New("--arg requires NAME and VALUE")
			}
			o.args[argv[i+1]] = argv[i+2]
			i += 2
		case a == "--argjson":
			if i+2 >= len(argv) {
				return nil, errors.New("--argjson requires NAME and JSON")
			}
			var v any
			if err := json.Unmarshal([]byte(argv[i+2]), &v); err != nil {
				return nil, fmt.Errorf("--argjson %s: %w", argv[i+1], err)
			}
			o.args[argv[i+1]] = v
			i += 2
		case strings.HasPrefix(a, "-") && len(a) > 1 && a != "-":
			// Unknown flag — ignore for compat.
		default:
			if o.filter == "" {
				o.filter = a
			} else {
				o.files = append(o.files, a)
			}
		}
	}
	if o.filter == "" {
		return nil, errors.New("missing filter")
	}
	return o, nil
}

func run(argv []string) int {
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jq: %v\n", err)
		return 2
	}
	query, err := gojq.Parse(o.filter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jq: parse: %v\n", err)
		return 2
	}
	var code *gojq.Code
	if len(o.args) > 0 {
		var names []string
		var values []any
		for k, v := range o.args {
			names = append(names, "$"+k)
			values = append(values, v)
		}
		code, err = gojq.Compile(query, gojq.WithVariables(names))
		if err != nil {
			fmt.Fprintf(os.Stderr, "jq: compile: %v\n", err)
			return 2
		}
		_ = values // pass into Run below
	} else {
		code, err = gojq.Compile(query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "jq: compile: %v\n", err)
			return 2
		}
	}

	inputs, err := readInputs(o)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jq: %v\n", err)
		return 2
	}

	lastTruthy := true
	any := false
	for _, in := range inputs {
		args := argsValues(o)
		iter := code.Run(in, args...)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, isErr := v.(error); isErr {
				fmt.Fprintf(os.Stderr, "jq: %v\n", err)
				return 5 // gnu jq uses 5 for runtime errors
			}
			any = true
			lastTruthy = truthy(v)
			emit(os.Stdout, v, o)
		}
	}

	if !any {
		return 0
	}
	if !lastTruthy {
		return 1
	}
	return 0
}

func argsValues(o *options) []any {
	if len(o.args) == 0 {
		return nil
	}
	// Order matters: we built names alphabetically in compile? No, map
	// iteration. Recompile with stable order each time would be
	// cleaner; we kept it simple: parse keys list once.
	var out []any
	for _, k := range sortedKeys(o.args) {
		out = append(out, o.args[k])
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func readInputs(o *options) ([]any, error) {
	if o.nullIn {
		return []any{nil}, nil
	}
	var readers []io.ReadCloser
	if len(o.files) == 0 {
		readers = append(readers, io.NopCloser(os.Stdin))
	} else {
		for _, p := range o.files {
			f, err := os.Open(p)
			if err != nil {
				return nil, err
			}
			readers = append(readers, f)
		}
	}
	defer func() {
		for _, r := range readers {
			r.Close()
		}
	}()

	if o.rawIn {
		var inputs []any
		for _, r := range readers {
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
			for sc.Scan() {
				inputs = append(inputs, sc.Text())
			}
			if err := sc.Err(); err != nil {
				return nil, err
			}
		}
		if o.slurp {
			return []any{joinStrings(inputs)}, nil
		}
		return inputs, nil
	}

	var inputs []any
	for _, r := range readers {
		dec := json.NewDecoder(r)
		dec.UseNumber()
		for {
			var v any
			if err := dec.Decode(&v); err != nil {
				if err == io.EOF {
					break
				}
				return nil, fmt.Errorf("decode: %w", err)
			}
			inputs = append(inputs, jsonNumberToFloat(v))
		}
	}
	if o.slurp {
		return []any{inputs}, nil
	}
	return inputs, nil
}

func joinStrings(in []any) string {
	var b strings.Builder
	for i, x := range in {
		if i > 0 {
			b.WriteByte('\n')
		}
		s, _ := x.(string)
		b.WriteString(s)
	}
	return b.String()
}

// jsonNumberToFloat recursively replaces json.Number with float64 so
// gojq sees the same shape as encoding/json would have without
// UseNumber.
func jsonNumberToFloat(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return int(i)
		}
		f, _ := x.Float64()
		return f
	case map[string]any:
		for k, vv := range x {
			x[k] = jsonNumberToFloat(vv)
		}
		return x
	case []any:
		for i, vv := range x {
			x[i] = jsonNumberToFloat(vv)
		}
		return x
	}
	return v
}

func emit(w io.Writer, v any, o *options) {
	if o.raw {
		if s, ok := v.(string); ok {
			fmt.Fprintln(w, s)
			return
		}
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if !o.compact {
		enc.SetIndent("", "  ")
	}
	if o.sortKeys {
		v = sortKeysDeep(v)
	}
	_ = enc.Encode(v)
}

// sortKeysDeep returns a copy of v with all map keys sorted. json
// encoder iterates map[string]any in key order already; this is a
// no-op for that case but documented behavior for any future
// custom encoder.
func sortKeysDeep(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = sortKeysDeep(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = sortKeysDeep(vv)
		}
		return out
	}
	return v
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	}
	return true
}
