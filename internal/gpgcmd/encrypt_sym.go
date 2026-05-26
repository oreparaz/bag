package gpgcmd

import (
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// doEncryptSymmetric implements `gpg -c` / `gpg --symmetric` — encrypt
// using a passphrase only (no key required). The output is an OpenPGP
// symmetrically encrypted message that the system gpg can decrypt
// with the same passphrase.
func doEncryptSymmetric(o *options) error {
	in, err := openInput(o)
	if err != nil {
		return err
	}
	defer in.Close()

	out, _, err := openOutput(o, outputSuffix(o, o.armor))
	if err != nil {
		return err
	}
	defer out.Close()

	pass, err := readPassphrase(o, "Enter passphrase: ")
	if err != nil {
		return err
	}

	armorCloser, err := armorOrPlain(out, "PGP MESSAGE", o)
	if err != nil {
		return err
	}

	hints := &openpgp.FileHints{
		IsBinary: !o.textMode,
		FileName: o.input, // FYI metadata; "" / "-" → no hint
	}
	if o.input == "-" {
		hints.FileName = ""
	}
	cfg := configFromOptions(o)
	w, err := openpgp.SymmetricallyEncrypt(armorCloser, pass, hints, cfg)
	if err != nil {
		armorCloser.Close()
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		w.Close()
		armorCloser.Close()
		return err
	}
	if err := w.Close(); err != nil {
		armorCloser.Close()
		return err
	}
	return armorCloser.Close()
}
