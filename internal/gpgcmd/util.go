package gpgcmd

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"io"
	"os"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// doEnarmor wraps arbitrary bytes in a "PGP MESSAGE" armor envelope,
// without any cryptographic processing. Useful for hand-crafting test
// fixtures or wrapping a binary packet stream.
func doEnarmor(o *options) error {
	in, err := openInput(o)
	if err != nil {
		return err
	}
	defer in.Close()
	out, _, err := openOutput(o, ".asc")
	if err != nil {
		return err
	}
	defer out.Close()

	blockType := "PGP MESSAGE"
	enc, err := armor.Encode(out, blockType, nil)
	if err != nil {
		return err
	}
	if _, err := io.Copy(enc, in); err != nil {
		enc.Close()
		return err
	}
	return enc.Close()
}

// doDearmor strips the ASCII-armor envelope from input, writing the raw
// binary body to output. gpg-compat: --dearmor with no -o defaults to
// stripping the .asc suffix from the input filename.
func doDearmor(o *options) error {
	in, err := openInput(o)
	if err != nil {
		return err
	}
	defer in.Close()
	body, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	stripped, err := dearmorIfNeeded(body)
	if err != nil {
		return err
	}
	out, _, err := openDearmorOutput(o)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.Write(stripped)
	return err
}

func openDearmorOutput(o *options) (io.WriteCloser, string, error) {
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
	if strings.HasSuffix(o.input, ".asc") {
		out := strings.TrimSuffix(o.input, ".asc")
		f, err := os.Create(out)
		return f, out, err
	}
	return nopWriteCloser{os.Stdout}, "-", nil
}

// doPrintMD computes one or more file hashes the same way `gpg
// --print-md ALGO FILE...` does: one digest per file, printed as
// "FILE: HH HH HH HH ..." groups of two hex digits, four to a chunk.
// With no files, reads stdin.
func doPrintMD(o *options) error {
	algo := strings.ToUpper(o.digest)
	if algo == "" {
		return fmt.Errorf("--print-md needs an algorithm (e.g. SHA256)")
	}
	newHash, err := hashCtor(algo)
	if err != nil {
		return err
	}
	files := o.importFiles
	if len(files) == 0 {
		files = []string{"-"}
	}
	for _, p := range files {
		var r io.ReadCloser
		var label string
		if p == "-" {
			r = io.NopCloser(os.Stdin)
		} else {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			r = f
			label = p + ": "
		}
		h := newHash()
		if _, err := io.Copy(h, r); err != nil {
			r.Close()
			return err
		}
		r.Close()
		sum := h.Sum(nil)
		fmt.Fprint(os.Stdout, label)
		printHexGroups(os.Stdout, sum)
		fmt.Fprintln(os.Stdout)
	}
	return nil
}

// printHexGroups writes b as uppercase hex with a space between each
// byte and an extra space every four bytes — matches `gpg --print-md`
// formatting so the output diffs cleanly.
func printHexGroups(w io.Writer, b []byte) {
	for i, x := range b {
		if i > 0 {
			if i%2 == 0 {
				fmt.Fprint(w, " ")
			}
			if i%16 == 0 {
				fmt.Fprint(w, "  ")
			}
		}
		fmt.Fprintf(w, "%02X", x)
	}
}

func hashCtor(name string) (func() hash.Hash, error) {
	switch name {
	case "MD5":
		return md5.New, nil
	case "SHA1", "SHA-1":
		return sha1.New, nil
	case "SHA224", "SHA-224":
		return sha256.New224, nil
	case "SHA256", "SHA-256":
		return sha256.New, nil
	case "SHA384", "SHA-384":
		return sha512.New384, nil
	case "SHA512", "SHA-512":
		return sha512.New, nil
	}
	return nil, fmt.Errorf("unknown digest %q (try MD5, SHA1, SHA256, SHA384, SHA512)", name)
}
