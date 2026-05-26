package gpgcmd

import (
	"fmt"
	"io"

	"github.com/ProtonMail/go-crypto/openpgp"
)

// doEncryptPublic implements `gpg --encrypt -r RECIPIENT`. It can also
// sign the message when -s is combined (`gpg -se -r ...`).
func doEncryptPublic(o *options) error {
	if len(o.recipients) == 0 {
		return fmt.Errorf("--encrypt requires at least one --recipient")
	}
	kr, err := loadKeyrings(o)
	if err != nil {
		return err
	}

	var recipients []*openpgp.Entity
	for _, r := range o.recipients {
		match := findEntity(kr.public, r)
		if match == nil {
			return fmt.Errorf("recipient %q: no public key found", r)
		}
		recipients = append(recipients, match)
	}

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

	armorCloser, err := armorOrPlain(out, "PGP MESSAGE", o)
	if err != nil {
		return err
	}

	hints := &openpgp.FileHints{
		IsBinary: !o.textMode,
		FileName: o.input,
	}
	if o.input == "-" {
		hints.FileName = ""
	}

	// If --sign was combined with --encrypt, pick a signer from the
	// secret keyring (matching --local-user when given) and unlock
	// its private material if it's passphrase-protected. The library
	// embeds a one-pass signature packet inside the encrypted stream
	// so the recipient sees both layers.
	var signer *openpgp.Entity
	if o.alsoSign {
		if len(kr.secret) == 0 {
			return fmt.Errorf("--sign with --encrypt needs a secret key")
		}
		if o.localUser != "" {
			signer = findEntity(kr.secret, o.localUser)
			if signer == nil {
				return fmt.Errorf("--local-user %q: no matching secret key", o.localUser)
			}
		} else {
			signer = kr.secret[0]
		}
		if signer.PrivateKey != nil && signer.PrivateKey.Encrypted {
			pp, err := readPassphrase(o, "Enter passphrase for signing key: ")
			if err != nil {
				return err
			}
			if err := signer.PrivateKey.Decrypt(pp); err != nil {
				return fmt.Errorf("bad passphrase: %w", err)
			}
		}
	}

	cfg := configFromOptions(o)
	w, err := openpgp.Encrypt(armorCloser, recipients, signer, hints, cfg)
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

// findEntity searches the keyring for the first Entity whose UID or
// fingerprint matches query. Returns nil if nothing matches.
func findEntity(list openpgp.EntityList, query string) *openpgp.Entity {
	for _, e := range list {
		if matchUserID(e, query) {
			return e
		}
	}
	return nil
}
