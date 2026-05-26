package gpgcmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// doDecrypt implements `gpg --decrypt`. Handles both symmetric (-c
// produced) and public-key encrypted messages — bag detects which by
// trying the loaded keyring first, falling back to asking for a
// passphrase.
func doDecrypt(o *options) error {
	in, err := openInput(o)
	if err != nil {
		return err
	}
	defer in.Close()

	// Slurp the input — OpenPGP messages are read multi-pass when the
	// reader contains both a session key and an integrity hash, and we
	// may need a second go if the first attempt was wrong.
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}

	// Strip ASCII armor if present. Library accepts both, but doing
	// it explicitly here means we can read the message once and
	// detect format issues earlier.
	body, err := dearmorIfNeeded(raw)
	if err != nil {
		return err
	}

	kr, err := loadKeyrings(o)
	if err != nil {
		// Still allow symmetric decrypt — that doesn't need keys.
		kr = &keyrings{}
	}

	// promptOnce remembers a passphrase across the multi-call prompts
	// the openpgp library may issue (it asks once per encryption layer,
	// once per locked secret key, ...). After the user types it the
	// first time we reuse it.
	var cached []byte
	prompt := openpgp.PromptFunction(func(keys []openpgp.Key, symmetric bool) ([]byte, error) {
		if cached != nil {
			return cached, nil
		}
		// For passphrase-only messages the library calls us with
		// symmetric=true. For locked secret keys it calls with
		// symmetric=false and the locked key set.
		msg := "Enter passphrase: "
		if !symmetric && len(keys) > 0 {
			msg = fmt.Sprintf("Enter passphrase for key %X: ", keys[0].PublicKey.KeyId)
		}
		p, err := readPassphrase(o, msg)
		if err != nil {
			return nil, err
		}
		cached = p
		// Decrypt any locked private keys so the next pass uses them.
		for _, k := range keys {
			if k.PrivateKey != nil && k.PrivateKey.Encrypted {
				_ = k.PrivateKey.Decrypt(p)
			}
		}
		return p, nil
	})

	md, err := openpgp.ReadMessage(bytes.NewReader(body), kr.secret, prompt, configFromOptions(o))
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}

	out, _, err := openDecryptOutput(o)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, md.UnverifiedBody); err != nil {
		return err
	}
	// Check signature status if the message was signed.
	if md.IsSigned {
		if md.SignatureError != nil {
			return fmt.Errorf("signature verify failed: %w", md.SignatureError)
		}
		if md.SignedBy != nil {
			fmt.Fprintf(stderr(), "gpg: Good signature from key %X\n", md.SignedBy.PublicKey.KeyId)
		}
	}
	return nil
}

// dearmorIfNeeded peels an ASCII-armored wrapper if present, returning
// the binary body. Plain binary input passes through unchanged.
func dearmorIfNeeded(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(data, " \t\r\n"), []byte("-----BEGIN")) {
		return data, nil
	}
	block, err := armor.Decode(bufio.NewReader(bytes.NewReader(data)))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(block.Body)
}

// stderr is wrapped in a helper so tests can intercept it cleanly.
var stderr = func() io.Writer { return errStream() }

// matchUserID returns true if the given Entity has an identity whose
// "name" matches the user query. The match is case-insensitive
// substring (gpg's default) plus exact hex Key ID / fingerprint.
func matchUserID(e *openpgp.Entity, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	for _, id := range e.Identities {
		if strings.Contains(strings.ToLower(id.UserId.Id), q) ||
			strings.Contains(strings.ToLower(id.UserId.Email), q) ||
			strings.Contains(strings.ToLower(id.UserId.Name), q) {
			return true
		}
	}
	// Fingerprint or KeyID (8 / 16 hex digits, ignoring 0x prefix and spaces).
	clean := strings.TrimPrefix(strings.ReplaceAll(strings.ToLower(query), " ", ""), "0x")
	fp := fmt.Sprintf("%x", e.PrimaryKey.Fingerprint)
	if strings.HasSuffix(fp, clean) {
		return true
	}
	kid := fmt.Sprintf("%016x", e.PrimaryKey.KeyId)
	return strings.HasSuffix(kid, clean)
}

// Errors that compare-equal to a sentinel let callers branch on
// "bad passphrase" vs "no key available" without needing to do error
// string matching.
var (
	errBadPassphrase = errors.New("bad passphrase")
	errNoKey         = errors.New("no usable key found")
)
