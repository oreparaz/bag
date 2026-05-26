package gpgcmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// doSign produces an OpenPGP signature in one of three flavors:
//
//	signInline   — message + signature in one packet stream
//	signDetached — signature only, separate file
//	signClear    — cleartext-signed message (human-readable)
//
// The signer's secret key is selected by --local-user / -u, falling
// back to the first secret key in the keyring.
func doSign(o *options, kind signKind) error {
	kr, err := loadKeyrings(o)
	if err != nil {
		return err
	}
	if len(kr.secret) == 0 {
		return errors.New("no secret keys available for signing")
	}

	var signer *openpgp.Entity
	if o.localUser != "" {
		signer = findEntity(kr.secret, o.localUser)
		if signer == nil {
			return fmt.Errorf("--local-user %q: no matching secret key", o.localUser)
		}
	} else {
		signer = kr.secret[0]
	}

	// Unlock the signer's secret key if it's passphrase-protected.
	if signer.PrivateKey != nil && signer.PrivateKey.Encrypted {
		pp, err := readPassphrase(o, "Enter passphrase for signing key: ")
		if err != nil {
			return err
		}
		if err := signer.PrivateKey.Decrypt(pp); err != nil {
			return fmt.Errorf("bad passphrase for signing key: %w", err)
		}
	}

	in, err := openInput(o)
	if err != nil {
		return err
	}
	defer in.Close()
	out, _, err := openOutput(o, signOutputSuffix(o, kind))
	if err != nil {
		return err
	}
	defer out.Close()

	cfg := configFromOptions(o)
	switch kind {
	case signInline:
		return signInlineMessage(out, in, signer, o, cfg)
	case signDetached:
		return signDetachedMessage(out, in, signer, o, cfg)
	case signClear:
		return signClearMessage(out, in, signer, cfg)
	}
	return fmt.Errorf("unknown sign kind")
}

// signOutputSuffix mirrors gpg's defaults for sign output filenames.
// --detach-sign uses .sig (.asc with -a), inline uses .gpg / .asc,
// --clearsign uses .asc.
func signOutputSuffix(o *options, kind signKind) string {
	switch kind {
	case signDetached:
		if o.armor {
			return ".asc"
		}
		return ".sig"
	case signClear:
		return ".asc"
	}
	if o.armor {
		return ".asc"
	}
	return ".gpg"
}

func signInlineMessage(out io.WriteCloser, in io.Reader, signer *openpgp.Entity, o *options, cfg *packet.Config) error {
	armorCloser, err := armorOrPlain(out, "PGP MESSAGE", o)
	if err != nil {
		return err
	}
	hints := &openpgp.FileHints{IsBinary: !o.textMode, FileName: o.input}
	if o.input == "-" {
		hints.FileName = ""
	}
	w, err := openpgp.Sign(armorCloser, signer, hints, cfg)
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

func signDetachedMessage(out io.WriteCloser, in io.Reader, signer *openpgp.Entity, o *options, cfg *packet.Config) error {
	if o.armor {
		enc, err := armor.Encode(out, "PGP SIGNATURE", nil)
		if err != nil {
			return err
		}
		if o.textMode {
			if err := openpgp.ArmoredDetachSignText(enc, signer, in, cfg); err == nil {
				return enc.Close()
			}
			// fallthrough on error
		}
		if err := openpgp.DetachSign(enc, signer, in, cfg); err != nil {
			enc.Close()
			return err
		}
		return enc.Close()
	}
	if o.textMode {
		return openpgp.DetachSignText(out, signer, in, cfg)
	}
	return openpgp.DetachSign(out, signer, in, cfg)
}

func signClearMessage(out io.WriteCloser, in io.Reader, signer *openpgp.Entity, cfg *packet.Config) error {
	body, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	w, err := clearsign.Encode(out, signer.PrivateKey, cfg)
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// doVerify dispatches on the shape of its arguments: one file (inline
// or clearsign), two files (sig + data), or stdin (clearsign by
// content sniffing).
func doVerify(o *options) error {
	kr, err := loadKeyrings(o)
	if err != nil {
		return err
	}

	// Case A: detached signature + separate data file.
	if o.signatureFile != "" && o.input != "" {
		return verifyDetached(o.signatureFile, o.input, kr.public)
	}

	// Case B: single file (or stdin) — inline or clearsign.
	var path string
	if o.signatureFile != "" {
		path = o.signatureFile
	} else if o.input != "" {
		path = o.input
	}
	var data []byte
	if path == "" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return err
	}

	if looksLikeClearsigned(data) {
		return verifyClearsigned(data, kr.public)
	}
	return verifyInline(data, kr.public)
}

func looksLikeClearsigned(data []byte) bool {
	trimmed := strings.TrimLeft(string(data), " \t\r\n")
	return strings.HasPrefix(trimmed, "-----BEGIN PGP SIGNED MESSAGE-----")
}

func verifyDetached(sigPath, dataPath string, keyring openpgp.EntityList) error {
	sig, err := os.Open(sigPath)
	if err != nil {
		return err
	}
	defer sig.Close()
	data, err := os.Open(dataPath)
	if err != nil {
		return err
	}
	defer data.Close()

	sigBytes, err := io.ReadAll(sig)
	if err != nil {
		return err
	}
	// Strip armor if present.
	body, err := dearmorIfNeeded(sigBytes)
	if err != nil {
		return err
	}
	signer, err := openpgp.CheckDetachedSignature(keyring, data, strings.NewReader(string(body)), nil)
	if err != nil {
		return fmt.Errorf("BAD signature: %w", err)
	}
	uid := primaryUIDString(signer)
	fmt.Fprintf(stderr(), "gpg: Good signature from \"%s\" [%X]\n", uid, signer.PrimaryKey.KeyId)
	return nil
}

func verifyInline(data []byte, keyring openpgp.EntityList) error {
	body, err := dearmorIfNeeded(data)
	if err != nil {
		return err
	}
	md, err := openpgp.ReadMessage(strings.NewReader(string(body)), keyring, nil, nil)
	if err != nil {
		return fmt.Errorf("read signed message: %w", err)
	}
	if _, err := io.Copy(io.Discard, md.UnverifiedBody); err != nil {
		return err
	}
	if md.SignatureError != nil {
		return fmt.Errorf("BAD signature: %w", md.SignatureError)
	}
	if md.SignedBy == nil {
		return errors.New("message has no signature")
	}
	uid := primaryUIDString(md.SignedBy.Entity)
	fmt.Fprintf(stderr(), "gpg: Good signature from \"%s\" [%X]\n",
		uid, md.SignedBy.PublicKey.KeyId)
	return nil
}

func verifyClearsigned(data []byte, keyring openpgp.EntityList) error {
	block, _ := clearsign.Decode(data)
	if block == nil {
		return errors.New("not a clearsigned message")
	}
	signer, err := openpgp.CheckDetachedSignature(keyring,
		strings.NewReader(string(block.Bytes)),
		block.ArmoredSignature.Body, nil)
	if err != nil {
		return fmt.Errorf("BAD clearsigned signature: %w", err)
	}
	uid := primaryUIDString(signer)
	fmt.Fprintf(stderr(), "gpg: Good signature from \"%s\" [%X]\n", uid, signer.PrimaryKey.KeyId)
	return nil
}
