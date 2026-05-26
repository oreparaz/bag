package gpgcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"golang.org/x/term"
)

// exit codes mirror gpg's most-common ones; the full set is huge but
// these three are what shells actually branch on.
const (
	exitOK    = 0
	exitErr   = 2
	exitUsage = 2
)

func run(args []string) int {
	o, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gpg: %v\n", err)
		return exitUsage
	}

	switch o.act {
	case actionHelp:
		printHelp(os.Stdout)
		return exitOK
	case actionVersion:
		printVersion(os.Stdout)
		return exitOK
	case actionNone:
		// gpg with no action and a file is treated as --decrypt (the
		// "drag-and-drop" default).
		if o.input != "" {
			o.act = actionDecrypt
		} else {
			printHelp(os.Stdout)
			return exitOK
		}
	}

	if err := runAction(o); err != nil {
		fmt.Fprintf(os.Stderr, "gpg: %v\n", err)
		return exitErr
	}
	return exitOK
}

func runAction(o *options) error {
	switch o.act {
	case actionEncryptSymmetric:
		return doEncryptSymmetric(o)
	case actionEncryptPublic:
		return doEncryptPublic(o)
	case actionDecrypt:
		return doDecrypt(o)
	case actionSign:
		return doSign(o, signInline)
	case actionDetachSign:
		return doSign(o, signDetached)
	case actionClearsign:
		return doSign(o, signClear)
	case actionVerify:
		return doVerify(o)
	case actionGenKey, actionQuickGenKey:
		return doGenKey(o)
	case actionListKeys:
		return doListKeys(o, false)
	case actionListSecretKeys:
		return doListKeys(o, true)
	case actionImport:
		return doImport(o)
	case actionExport:
		return doExport(o, false)
	case actionExportSecret:
		return doExport(o, true)
	case actionDeleteKeys:
		return doDeleteKeys(o, false)
	case actionDeleteSecretKeys:
		return doDeleteKeys(o, true)
	}
	return fmt.Errorf("unknown action")
}

// homeDir resolves the GPG home directory: --homedir, then $GNUPGHOME,
// then ~/.gnupg. Matches gpg's own search order.
func homeDir(o *options) string {
	if o.homeDir != "" {
		return o.homeDir
	}
	if v := os.Getenv("GNUPGHOME"); v != "" {
		return v
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".gnupg")
}

// readPassphrase resolves the passphrase from the various sources
// gpg supports, falling back to /dev/tty when nothing else is set.
func readPassphrase(o *options, prompt string) ([]byte, error) {
	if strings.HasPrefix(o.passphrase, "@file:") {
		path := strings.TrimPrefix(o.passphrase, "@file:")
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return []byte(strings.TrimRight(string(b), "\r\n")), nil
	}
	if o.passphrase != "" {
		return []byte(o.passphrase), nil
	}
	if o.passphraseFD >= 0 {
		var r io.Reader
		// fd 0 specifically routes through os.Stdin so the process-wide
		// stdin redirection (used by tests, and by shells piping into
		// gpg) is honored. For other fds we open a fresh File on the
		// raw descriptor — gpg's documented behaviour.
		if o.passphraseFD == 0 {
			r = os.Stdin
		} else {
			f := os.NewFile(uintptr(o.passphraseFD), fmt.Sprintf("fd %d", o.passphraseFD))
			if f == nil {
				return nil, fmt.Errorf("passphrase-fd %d: invalid fd", o.passphraseFD)
			}
			defer f.Close()
			r = f
		}
		// Read just one line (passphrase ends at the first newline,
		// per gpg). io.ReadAll on stdin would block on the test pipe
		// even after the passphrase is written, because the writing
		// goroutine in tests may keep the pipe open.
		b, err := readOneLine(r)
		if err != nil {
			return nil, err
		}
		return b, nil
	}
	if o.batch {
		return nil, errors.New("no passphrase available in --batch mode")
	}
	// Interactive: read from /dev/tty so a pipe on stdin doesn't get
	// swallowed.
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("cannot open /dev/tty: %w", err)
	}
	defer tty.Close()
	fmt.Fprint(tty, prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	fmt.Fprintln(tty)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// openInput resolves the input source: positional arg or stdin.
func openInput(o *options) (io.ReadCloser, error) {
	if o.input == "" || o.input == "-" {
		return io.NopCloser(os.Stdin), nil
	}
	return os.Open(o.input)
}

// openOutput resolves the output sink. Default for binary modes when
// no -o is given: write to <input>.gpg (or <input>.asc with --armor).
// Default to stdout when stdin is the input.
func openOutput(o *options, defaultSuffix string) (io.WriteCloser, string, error) {
	if o.output != "" {
		if o.output == "-" {
			return nopWriteCloser{os.Stdout}, "-", nil
		}
		f, err := os.Create(o.output)
		return f, o.output, err
	}
	if o.input == "" || o.input == "-" {
		// stdin input → stdout output by default.
		return nopWriteCloser{os.Stdout}, "-", nil
	}
	out := o.input + defaultSuffix
	f, err := os.Create(out)
	return f, out, err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// readOneLine reads bytes from r until the first '\n' or EOF and
// returns the line without the trailing newline. gpg's passphrase-fd
// reads one passphrase up to the first newline; reading the rest of
// the stream would block on a test pipe.
func readOneLine(r io.Reader) ([]byte, error) {
	var buf []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return buf, nil
			}
			buf = append(buf, one[0])
		}
		if err != nil {
			if err == io.EOF {
				return buf, nil
			}
			return buf, err
		}
	}
}

// openDecryptOutput chooses an output sink for --decrypt. Rules:
//   - explicit -o / --output wins.
//   - stdin input → stdout output.
//   - input ending in .gpg / .asc / .pgp / .sig → strip the suffix.
//   - otherwise stdout (NEVER overwrite the input file).
func openDecryptOutput(o *options) (io.WriteCloser, string, error) {
	if o.output != "" {
		if o.output == "-" {
			return nopWriteCloser{os.Stdout}, "-", nil
		}
		f, err := os.Create(o.output)
		return f, o.output, err
	}
	if o.input == "" || o.input == "-" {
		return nopWriteCloser{os.Stdout}, "-", nil
	}
	for _, suf := range []string{".gpg", ".asc", ".pgp", ".sig"} {
		if strings.HasSuffix(o.input, suf) {
			out := strings.TrimSuffix(o.input, suf)
			f, err := os.Create(out)
			return f, out, err
		}
	}
	return nopWriteCloser{os.Stdout}, "-", nil
}

// armorOrPlain wraps w in an armor encoder if o.armor is set,
// otherwise returns w directly. Caller MUST call the returned close
// before flushing w itself to finish the armor framing.
func armorOrPlain(w io.Writer, blockType string, o *options) (io.WriteCloser, error) {
	if o.armor {
		// "-" stdout: gpg also armors by default for terminals,
		// but bag keeps the explicit-only behaviour for simplicity.
		enc, err := armor.Encode(w, blockType, nil)
		return enc, err
	}
	return nopWriteCloser{w}, nil
}

// outputSuffix is the extension gpg adds to the input filename when
// --output isn't given.
func outputSuffix(o *options, armored bool) string {
	if armored {
		return ".asc"
	}
	return ".gpg"
}

// configFromOptions builds an openpgp config from the user's --digest
// / --cipher / --compress flags. Falls through to library defaults
// when a knob isn't set.
func configFromOptions(o *options) *packet.Config {
	c := &packet.Config{
		DefaultHash:            mapHash(o.digest),
		DefaultCipher:          mapCipher(o.cipher),
		DefaultCompressionAlgo: mapCompress(o.compress),
	}
	return c
}

// openpgp.Config implements its own defaults when fields are zero, so
// our map helpers convert from gpg's name strings to the library's
// algorithm constants, returning zero (= library default) when the
// option is empty.

// withReadKeyring is a helper that loads the public keyring + secret
// keyring from disk, invokes fn with both, then closes any files.
type keyrings struct {
	public openpgp.EntityList
	secret openpgp.EntityList
}

func loadKeyrings(o *options) (*keyrings, error) {
	kr := &keyrings{}
	pub, err := loadKeyring(filepath.Join(homeDir(o), "pubring.gpg"), false)
	if err != nil && !os.IsNotExist(err) {
		// Try .kbx (newer gpg). We can't parse kbx; only flat .gpg
		// works. If only kbx is present, suggest exporting.
		return nil, fmt.Errorf("cannot read public keyring: %w", err)
	}
	kr.public = pub
	sec, err := loadKeyring(filepath.Join(homeDir(o), "secring.gpg"), false)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot read secret keyring: %w", err)
	}
	kr.secret = sec
	return kr, nil
}

// loadKeyring reads either a binary or armored keyring file.
func loadKeyring(path string, mustExist bool) (openpgp.EntityList, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) && !mustExist {
			return nil, err
		}
		return nil, err
	}
	defer f.Close()
	// Try armored first by peeking; ReadArmoredKeyRing reads from a
	// Reader so we use a bufio buffer.
	return readEntities(f)
}

func readEntities(r io.Reader) (openpgp.EntityList, error) {
	// Read everything into memory — keyring files are typically tiny.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// Detect armor by leading "-----BEGIN".
	if len(data) >= 5 && string(data[:5]) == "-----" {
		return openpgp.ReadArmoredKeyRing(strings.NewReader(string(data)))
	}
	return openpgp.ReadKeyRing(strings.NewReader(string(data)))
}
