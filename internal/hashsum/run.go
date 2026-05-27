package hashsum

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"
)

type algo struct {
	name    string // CLI name as printed in BSD-tag headers
	newHash func() hash.Hash
	hexLen  int // expected hex-digest length for -c parsing
}

func algoFor(cliName string) (algo, error) {
	switch cliName {
	case "sha256sum", "bag-sha256sum":
		return algo{"SHA256", sha256.New, 64}, nil
	case "sha512sum", "bag-sha512sum":
		return algo{"SHA512", sha512.New, 128}, nil
	case "sha1sum", "bag-sha1sum":
		return algo{"SHA1", sha1.New, 40}, nil
	case "md5sum", "bag-md5sum":
		return algo{"MD5", md5.New, 32}, nil
	}
	return algo{}, fmt.Errorf("unknown hash binary %q", cliName)
}

type options struct {
	binary        bool
	text          bool
	tag           bool
	check         bool
	quiet         bool
	status        bool
	strict        bool
	ignoreMissing bool
	zero          bool
	files         []string
}

func parseArgs(argv []string) (*options, error) {
	o := &options{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			o.files = append(o.files, argv[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "--") {
			switch a[2:] {
			case "binary":
				o.binary = true
			case "text":
				o.text = true
			case "tag":
				o.tag = true
			case "check":
				o.check = true
			case "quiet":
				o.quiet = true
			case "status":
				o.status = true
			case "strict":
				o.strict = true
			case "ignore-missing":
				o.ignoreMissing = true
			case "zero":
				o.zero = true
			default:
				// Silently ignore unknown long flags.
			}
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'b':
					o.binary = true
				case 't':
					o.text = true
				case 'c':
					o.check = true
				case 'z':
					o.zero = true
				default:
					// Silently ignore unknown short flags.
				}
			}
			continue
		}
		o.files = append(o.files, a)
	}
	if len(o.files) == 0 {
		o.files = []string{"-"}
	}
	return o, nil
}

func run(cliName string, argv []string) int {
	a, err := algoFor(cliName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cliName, err)
		return 1
	}
	o, err := parseArgs(argv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", cliName, err)
		return 1
	}
	if o.check {
		return doCheck(a, o, cliName)
	}
	return doHash(a, o, cliName)
}

// doHash streams each input through the hash and prints one line per
// file. Default output format: "HEX  FILE" (two spaces, text mode);
// "HEX *FILE" (binary mode, asterisk between); or "ALGO (FILE) = HEX"
// with --tag.
func doHash(a algo, o *options, cliName string) int {
	term := byte('\n')
	if o.zero {
		term = 0
	}
	exit := 0
	for _, p := range o.files {
		hex, err := hashOne(a, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %s: %v\n", cliName, p, err)
			exit = 1
			continue
		}
		switch {
		case o.tag:
			fmt.Fprintf(os.Stdout, "%s (%s) = %s%c", a.name, p, hex, term)
		case o.binary:
			fmt.Fprintf(os.Stdout, "%s *%s%c", hex, p, term)
		default:
			fmt.Fprintf(os.Stdout, "%s  %s%c", hex, p, term)
		}
	}
	return exit
}

func hashOne(a algo, path string) (string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer f.Close()
		r = f
	}
	h := a.newHash()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// doCheck reads each input as a checksum file, parses one record per
// line (or per NUL with -z), recomputes the file's hash, compares.
func doCheck(a algo, o *options, cliName string) int {
	var (
		ok, bad, missing int
	)
	for _, p := range o.files {
		var r io.Reader
		if p == "-" {
			r = os.Stdin
		} else {
			f, err := os.Open(p)
			if err != nil {
				if !o.status {
					fmt.Fprintf(os.Stderr, "%s: %s: %v\n", cliName, p, err)
				}
				return 1
			}
			defer f.Close()
			r = f
		}
		if err := checkStream(a, r, o, cliName, &ok, &bad, &missing); err != nil {
			if !o.status {
				fmt.Fprintf(os.Stderr, "%s: %v\n", cliName, err)
			}
			return 1
		}
	}
	if !o.status && bad > 0 {
		fmt.Fprintf(os.Stderr, "%s: WARNING: %d computed checksum did NOT match\n",
			cliName, bad)
	}
	if !o.status && missing > 0 && !o.ignoreMissing {
		fmt.Fprintf(os.Stderr, "%s: WARNING: %d listed file could not be read\n",
			cliName, missing)
	}
	if bad > 0 || missing > 0 {
		return 1
	}
	if ok == 0 {
		// gnu: if no lines verified, treat as error.
		if !o.status {
			fmt.Fprintf(os.Stderr, "%s: no properly formatted checksum lines found\n", cliName)
		}
		return 1
	}
	return 0
}

func checkStream(a algo, r io.Reader, o *options, cliName string, ok, bad, missing *int) error {
	br := bufio.NewReader(r)
	for {
		var line string
		if o.zero {
			s, err := readUntil(br, 0)
			if err != nil && len(s) == 0 {
				if err == io.EOF {
					return nil
				}
				return err
			}
			line = s
		} else {
			s, err := br.ReadString('\n')
			if len(s) == 0 && err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			line = strings.TrimRight(s, "\r\n")
		}
		if line == "" {
			continue
		}
		path, want, format, perr := parseCheckLine(line, a)
		if perr != nil {
			if o.strict {
				return perr
			}
			if !o.status {
				fmt.Fprintf(os.Stderr, "%s: improperly formatted line: %s\n", cliName, line)
			}
			continue
		}
		got, err := hashOne(a, path)
		if err != nil {
			if o.ignoreMissing {
				continue
			}
			*missing++
			if !o.status {
				fmt.Fprintf(os.Stdout, "%s: FAILED open or read\n", path)
			}
			continue
		}
		if strings.EqualFold(got, want) {
			*ok++
			if !o.quiet && !o.status {
				if format == "tag" {
					fmt.Fprintf(os.Stdout, "%s: OK\n", path)
				} else {
					fmt.Fprintf(os.Stdout, "%s: OK\n", path)
				}
			}
		} else {
			*bad++
			if !o.status {
				fmt.Fprintf(os.Stdout, "%s: FAILED\n", path)
			}
		}
	}
}

// parseCheckLine accepts both default ("HEX  FILE" / "HEX *FILE") and
// BSD-tag ("ALGO (FILE) = HEX") forms. Returns path, hex-digest,
// format-name, error.
func parseCheckLine(line string, a algo) (string, string, string, error) {
	// BSD tag form.
	if strings.HasPrefix(line, a.name+" (") {
		// "ALGO (FILE) = HEX"
		open := strings.Index(line, "(")
		close := strings.LastIndex(line, ") = ")
		if open < 0 || close < 0 || close < open {
			return "", "", "", errors.New("bad BSD-tag line")
		}
		path := line[open+1 : close]
		hex := line[close+4:]
		if len(hex) != a.hexLen {
			return "", "", "", fmt.Errorf("expected %d-char digest, got %d", a.hexLen, len(hex))
		}
		return path, hex, "tag", nil
	}
	// Default: HEX{2-space-or-asterisk}FILE
	if len(line) < a.hexLen+3 {
		return "", "", "", errors.New("line too short")
	}
	hex := line[:a.hexLen]
	for i, c := range hex {
		if !isHex(byte(c)) {
			return "", "", "", fmt.Errorf("non-hex at position %d", i)
		}
	}
	sep := line[a.hexLen : a.hexLen+2]
	if sep != "  " && sep != " *" {
		return "", "", "", errors.New("bad separator between digest and filename")
	}
	path := line[a.hexLen+2:]
	return path, hex, "default", nil
}

func isHex(b byte) bool {
	return ('0' <= b && b <= '9') || ('a' <= b && b <= 'f') || ('A' <= b && b <= 'F')
}

func readUntil(br *bufio.Reader, sep byte) (string, error) {
	var b strings.Builder
	for {
		c, err := br.ReadByte()
		if err != nil {
			return b.String(), err
		}
		if c == sep {
			return b.String(), nil
		}
		b.WriteByte(c)
	}
}
