package gpgcmd

import (
	"bufio"
	"crypto"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// doGenKey implements --gen-key (interactive prompts) and
// --quick-gen-key (one-shot from argv). The new entity is appended to
// the pubring.gpg / secring.gpg files in the home dir, matching what
// system gpg writes to its legacy keybox.
func doGenKey(o *options) error {
	gk, err := collectKeyGenInputs(o)
	if err != nil {
		return err
	}

	cfg := &packet.Config{
		Algorithm:  gk.algo,
		RSABits:    gk.rsaBits,
		Time:       func() time.Time { return time.Now() },
		DefaultHash: crypto.SHA256,
	}
	switch gk.algo {
	case packet.PubKeyAlgoEdDSA:
		cfg.Curve = packet.Curve25519
	case packet.PubKeyAlgoECDSA:
		cfg.Curve = packet.CurveNistP256
	}

	uid := gk.userID
	entity, err := openpgp.NewEntity(uid.name, uid.comment, uid.email, cfg)
	if err != nil {
		return fmt.Errorf("key generation failed: %w", err)
	}

	// Expiry: zero means no expiration.
	if gk.expiry > 0 {
		for _, id := range entity.Identities {
			id.SelfSignature.KeyLifetimeSecs = &gk.expiry
		}
		for _, sub := range entity.Subkeys {
			sub.Sig.KeyLifetimeSecs = &gk.expiry
		}
	}

	// Encrypt secret material with the passphrase, if one was given.
	if len(gk.passphrase) > 0 {
		if err := lockEntity(entity, gk.passphrase); err != nil {
			return err
		}
	}

	if err := appendToKeyring(o, entity); err != nil {
		return err
	}

	fp := entity.PrimaryKey.Fingerprint
	fmt.Fprintf(os.Stderr, "gpg: key %X marked as ultimately trusted\n", entity.PrimaryKey.KeyId)
	fmt.Fprintf(os.Stderr, "gpg: public and secret key created and signed.\n")
	fmt.Fprintf(os.Stderr, "pub   %s %s [SC]\n", algoTag(entity.PrimaryKey.PubKeyAlgo, gk.rsaBits),
		time.Now().Format("2006-01-02"))
	fmt.Fprintf(os.Stderr, "      %X\n", fp)
	fmt.Fprintf(os.Stderr, "uid          %s\n", uid.String())
	if len(entity.Subkeys) > 0 {
		sub := entity.Subkeys[0]
		fmt.Fprintf(os.Stderr, "sub   %s %s [E]\n",
			algoTag(sub.PublicKey.PubKeyAlgo, 0),
			time.Now().Format("2006-01-02"))
	}
	return nil
}

type keyGenInputs struct {
	algo       packet.PublicKeyAlgorithm
	rsaBits    int
	userID     userID
	expiry     uint32 // seconds; 0 = never
	passphrase []byte
}

type userID struct {
	name    string
	email   string
	comment string
}

func (u userID) String() string {
	out := u.name
	if u.comment != "" {
		out += " (" + u.comment + ")"
	}
	if u.email != "" {
		out += " <" + u.email + ">"
	}
	return out
}

func collectKeyGenInputs(o *options) (keyGenInputs, error) {
	var gk keyGenInputs

	// Resolve algo string → packet.PublicKeyAlgorithm.
	algo := strings.ToLower(o.keyType)
	if algo == "" {
		algo = "default"
	}
	bits := o.keyBits
	switch algo {
	case "default", "rsa", "rsa4096":
		gk.algo = packet.PubKeyAlgoRSA
		if bits == 0 {
			bits = 4096
		}
		if algo == "rsa" && o.keyBits == 0 {
			bits = 3072
		}
		gk.rsaBits = bits
	case "rsa2048":
		gk.algo = packet.PubKeyAlgoRSA
		gk.rsaBits = 2048
	case "rsa3072":
		gk.algo = packet.PubKeyAlgoRSA
		gk.rsaBits = 3072
	case "ed25519", "eddsa":
		gk.algo = packet.PubKeyAlgoEdDSA
	case "ecdsa", "nistp256":
		gk.algo = packet.PubKeyAlgoECDSA
	case "dsa", "elgamal", "elg":
		// The underlying library can READ DSA / ElGamal keys (needed for
		// decrypt/verify of old material) but does not implement
		// generation. Surface the limitation up front instead of letting
		// NewEntity fail with "unsupported public key algorithm".
		return gk, fmt.Errorf("--quick-gen-key %q: bag generates RSA/EdDSA/ECDSA only; DSA/ElGamal can still be imported and used", o.keyType)
	case "future-default":
		gk.algo = packet.PubKeyAlgoEdDSA
	default:
		return gk, fmt.Errorf("unsupported key type %q", o.keyType)
	}

	// User ID.
	var rawUID string
	if o.keyName != "" {
		rawUID = o.keyName
	} else if o.act == actionGenKey {
		// Interactive prompts. Skip in --batch.
		if o.batch {
			return gk, errors.New("--batch with --gen-key needs --quick-gen-key or batch input")
		}
		r := bufio.NewReader(os.Stdin)
		name, err := promptLine(r, "Real name: ")
		if err != nil {
			return gk, err
		}
		email, err := promptLine(r, "Email address: ")
		if err != nil {
			return gk, err
		}
		comment, err := promptLine(r, "Comment: ")
		if err != nil {
			return gk, err
		}
		gk.userID = userID{name: name, email: email, comment: comment}
	}
	if rawUID != "" {
		gk.userID = parseUID(rawUID)
	}
	if gk.userID.name == "" && gk.userID.email == "" {
		return gk, errors.New("user-id required (Name Surname <email@example.com>)")
	}

	// Expiry.
	if o.keyExpiry != "" {
		secs, err := parseDuration(o.keyExpiry)
		if err != nil {
			return gk, err
		}
		gk.expiry = secs
	}

	// Passphrase. If not in batch and none supplied, prompt.
	if o.passphrase != "" || o.passphraseFD >= 0 {
		pp, err := readPassphrase(o, "Enter passphrase for new key: ")
		if err != nil {
			return gk, err
		}
		gk.passphrase = pp
	} else if !o.batch {
		// Real gpg prompts. Skip in batch.
		pp, err := readPassphrase(o, "Enter passphrase (empty for unprotected): ")
		if err == nil && len(pp) > 0 {
			gk.passphrase = pp
		}
	}
	return gk, nil
}

// parseUID parses gpg's combined "Name Surname (comment) <email@host>"
// form. All three fields are optional but at least one must be set.
func parseUID(s string) userID {
	var u userID
	s = strings.TrimSpace(s)
	// Email in <>
	if i := strings.LastIndexByte(s, '<'); i >= 0 {
		if j := strings.LastIndexByte(s, '>'); j > i {
			u.email = s[i+1 : j]
			s = strings.TrimSpace(s[:i])
		}
	}
	// Comment in ()
	if i := strings.LastIndexByte(s, '('); i >= 0 {
		if j := strings.LastIndexByte(s, ')'); j > i {
			u.comment = s[i+1 : j]
			s = strings.TrimSpace(s[:i])
		}
	}
	u.name = s
	return u
}

func promptLine(r *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

// parseDuration accepts gpg's expiry shorthand: "0" (never), "<N>"
// (days), "<N>w" / "<N>m" / "<N>y", or an ISO date YYYY-MM-DD.
func parseDuration(s string) (uint32, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	if len(s) == 10 && s[4] == '-' && s[7] == '-' {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return 0, err
		}
		d := time.Until(t)
		if d <= 0 {
			return 0, fmt.Errorf("expiry %q is in the past", s)
		}
		return uint32(d.Seconds()), nil
	}
	unit := s[len(s)-1]
	num := s
	mult := 24 * 3600
	switch unit {
	case 'd':
		num = s[:len(s)-1]
	case 'w':
		num = s[:len(s)-1]
		mult = 7 * 24 * 3600
	case 'm':
		num = s[:len(s)-1]
		mult = 30 * 24 * 3600
	case 'y':
		num = s[:len(s)-1]
		mult = 365 * 24 * 3600
	}
	var n int64
	if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
		return 0, fmt.Errorf("invalid expiry %q", s)
	}
	if n < 0 {
		return 0, fmt.Errorf("expiry must be positive")
	}
	return uint32(n * int64(mult)), nil
}

// lockEntity passphrase-encrypts the secret key material in entity
// (primary + all subkeys), so writing the secret keyring doesn't leak
// the unencrypted private bits to disk.
func lockEntity(entity *openpgp.Entity, pp []byte) error {
	if entity.PrivateKey == nil {
		return errors.New("no private key to lock")
	}
	if err := entity.PrivateKey.Encrypt(pp); err != nil {
		return err
	}
	for _, sub := range entity.Subkeys {
		if sub.PrivateKey != nil {
			if err := sub.PrivateKey.Encrypt(pp); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendToKeyring writes entity.Serialize() (public material) to
// pubring.gpg and, if the entity carries private key material,
// entity.SerializePrivateWithoutSigning() to secring.gpg. Public-only
// imports skip the secring write.
func appendToKeyring(o *options, entity *openpgp.Entity) error {
	home := homeDir(o)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	if err := appendEntity(filepath.Join(home, "pubring.gpg"), entity, false); err != nil {
		return err
	}
	if entity.PrivateKey != nil {
		if err := appendEntity(filepath.Join(home, "secring.gpg"), entity, true); err != nil {
			return err
		}
	}
	return nil
}

func appendEntity(path string, entity *openpgp.Entity, secret bool) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if secret {
		return entity.SerializePrivateWithoutSigning(f, nil)
	}
	return entity.Serialize(f)
}

// algoTag returns the short tag (e.g. "rsa3072", "ed25519") for a
// public-key algorithm, used in the human-readable post-genkey
// summary.
func algoTag(a packet.PublicKeyAlgorithm, rsaBits int) string {
	switch a {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSAEncryptOnly, packet.PubKeyAlgoRSASignOnly:
		if rsaBits > 0 {
			return fmt.Sprintf("rsa%d", rsaBits)
		}
		return "rsa"
	case packet.PubKeyAlgoDSA:
		return "dsa"
	case packet.PubKeyAlgoElGamal:
		return "elg"
	case packet.PubKeyAlgoECDSA:
		return "ecdsa"
	case packet.PubKeyAlgoEdDSA:
		return "ed25519"
	case packet.PubKeyAlgoECDH:
		return "ecdh"
	}
	return fmt.Sprintf("algo%d", a)
}
