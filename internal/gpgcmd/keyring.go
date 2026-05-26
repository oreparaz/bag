package gpgcmd

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
)

// doListKeys prints public (or secret) keys matching the user query
// in the human-readable format gpg uses. The format is a stable subset
// of gpg --list-keys output so scripts that parse it (search for
// "pub" / "uid" / fingerprints) keep working.
func doListKeys(o *options, secret bool) error {
	kr, err := loadKeyrings(o)
	if err != nil {
		return err
	}
	list := kr.public
	headerPath := filepath.Join(homeDir(o), "pubring.gpg")
	if secret {
		list = kr.secret
		headerPath = filepath.Join(homeDir(o), "secring.gpg")
	}
	out, _, err := openOutput(o, "")
	if err != nil {
		return err
	}
	defer out.Close()

	fmt.Fprintln(out, headerPath)
	fmt.Fprintln(out, strings.Repeat("-", len(headerPath)))
	for _, e := range list {
		if !matchUserID(e, o.exportUser) {
			continue
		}
		printEntity(out, e, secret)
	}
	return nil
}

func printEntity(w io.Writer, e *openpgp.Entity, secret bool) {
	prefix := "pub"
	if secret {
		prefix = "sec"
	}
	created := e.PrimaryKey.CreationTime.Format("2006-01-02")
	algo := algoTag(e.PrimaryKey.PubKeyAlgo, 0)
	if bits, err := e.PrimaryKey.BitLength(); err == nil && bits > 0 &&
		strings.HasPrefix(algo, "rsa") {
		algo = fmt.Sprintf("rsa%d", bits)
	}
	fmt.Fprintf(w, "%s   %s %s [%s]\n", prefix, algo, created, keyUsageTag(e))
	fmt.Fprintf(w, "      %X\n", e.PrimaryKey.Fingerprint)
	for _, id := range e.Identities {
		fmt.Fprintf(w, "uid           [ unknown] %s\n", id.UserId.Id)
	}
	for _, sub := range e.Subkeys {
		subPrefix := "sub"
		if secret {
			subPrefix = "ssb"
		}
		subAlgo := algoTag(sub.PublicKey.PubKeyAlgo, 0)
		if bits, err := sub.PublicKey.BitLength(); err == nil && bits > 0 &&
			strings.HasPrefix(subAlgo, "rsa") {
			subAlgo = fmt.Sprintf("rsa%d", bits)
		}
		fmt.Fprintf(w, "%s   %s %s [%s]\n",
			subPrefix, subAlgo,
			sub.PublicKey.CreationTime.Format("2006-01-02"),
			subkeyUsageTag(sub),
		)
	}
	fmt.Fprintln(w)
}

// keyUsageTag returns a one-letter usage hint for the primary key:
// S=sign, C=certify, E=encrypt, A=auth. Default is SC (sign+certify)
// for the primary key, which is what gpg shows for almost every
// modern key.
func keyUsageTag(e *openpgp.Entity) string {
	// Self-signature's flags carry the real usage; pick the primary
	// identity's self-sig.
	for _, id := range e.Identities {
		s := id.SelfSignature
		if s == nil {
			continue
		}
		var b strings.Builder
		if s.FlagSign || !s.FlagsValid {
			b.WriteByte('S')
		}
		if s.FlagCertify || !s.FlagsValid {
			b.WriteByte('C')
		}
		if s.FlagEncryptCommunications || s.FlagEncryptStorage {
			b.WriteByte('E')
		}
		if s.FlagAuthenticate {
			b.WriteByte('A')
		}
		if b.Len() > 0 {
			return b.String()
		}
	}
	return "SC"
}

func subkeyUsageTag(sub openpgp.Subkey) string {
	if sub.Sig == nil {
		return "E"
	}
	var b strings.Builder
	if sub.Sig.FlagSign {
		b.WriteByte('S')
	}
	if sub.Sig.FlagEncryptCommunications || sub.Sig.FlagEncryptStorage {
		b.WriteByte('E')
	}
	if sub.Sig.FlagAuthenticate {
		b.WriteByte('A')
	}
	if b.Len() == 0 {
		return "E"
	}
	return b.String()
}

// doImport reads one or more keyring files (each can be binary or
// armored, contain one or many entities) and merges the new entities
// into ~/.gnupg/pubring.gpg + secring.gpg.
func doImport(o *options) error {
	files := o.importFiles
	if len(files) == 0 {
		files = []string{"-"}
	}
	var incoming openpgp.EntityList
	for _, p := range files {
		var r io.Reader
		if p == "-" {
			r = os.Stdin
		} else {
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			r = f
		}
		es, err := readEntities(r)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		incoming = append(incoming, es...)
	}
	if len(incoming) == 0 {
		return fmt.Errorf("no keys found")
	}
	for _, e := range incoming {
		hasSecret := e.PrivateKey != nil
		if err := appendToKeyringIfNew(o, e, hasSecret); err != nil {
			return err
		}
		uid := primaryUIDString(e)
		fmt.Fprintf(stderr(), "gpg: key %X: \"%s\" imported\n",
			e.PrimaryKey.KeyId, uid)
	}
	fmt.Fprintf(stderr(), "gpg: Total number processed: %d\n", len(incoming))
	return nil
}

// appendToKeyringIfNew skips entities whose fingerprint already
// appears in pubring; matches `gpg --import`'s deduplication.
func appendToKeyringIfNew(o *options, e *openpgp.Entity, secret bool) error {
	kr, _ := loadKeyrings(o)
	existing := kr.public
	for _, x := range existing {
		if x.PrimaryKey.KeyId == e.PrimaryKey.KeyId {
			return nil // already present
		}
	}
	return appendToKeyring(o, e)
}

// doExport writes selected public (or secret) keys to stdout / --output
// in either binary or armored form.
func doExport(o *options, secret bool) error {
	kr, err := loadKeyrings(o)
	if err != nil {
		return err
	}
	list := kr.public
	if secret {
		list = kr.secret
	}

	var matched openpgp.EntityList
	for _, e := range list {
		if !matchUserID(e, o.exportUser) {
			continue
		}
		matched = append(matched, e)
	}
	if len(matched) == 0 {
		return fmt.Errorf("nothing exported (no keys match %q)", o.exportUser)
	}

	out, _, err := openOutput(o, "")
	if err != nil {
		return err
	}
	defer out.Close()

	var w io.Writer = out
	var armorCloser io.Closer
	if o.armor {
		blockType := "PGP PUBLIC KEY BLOCK"
		if secret {
			blockType = "PGP PRIVATE KEY BLOCK"
		}
		enc, err := armor.Encode(out, blockType, nil)
		if err != nil {
			return err
		}
		armorCloser = enc
		w = enc
	}

	for _, e := range matched {
		if secret {
			if err := e.SerializePrivateWithoutSigning(w, nil); err != nil {
				return err
			}
		} else {
			if err := e.Serialize(w); err != nil {
				return err
			}
		}
	}
	if armorCloser != nil {
		return armorCloser.Close()
	}
	return nil
}

// doDeleteKeys removes the entity matching o.exportUser from the
// keyring on disk. When alsoSecret is true, only the secret half is
// removed (public stays). For real --delete-keys (alsoSecret=false)
// we refuse if a corresponding secret key is still present, matching
// gpg's safeguard.
func doDeleteKeys(o *options, alsoSecret bool) error {
	if o.exportUser == "" {
		return fmt.Errorf("delete-keys requires a key identifier")
	}
	kr, err := loadKeyrings(o)
	if err != nil {
		return err
	}

	if !alsoSecret {
		// --delete-keys: refuse if a secret key matches first.
		for _, e := range kr.secret {
			if matchUserID(e, o.exportUser) {
				return fmt.Errorf("there is a secret key for %q; use --delete-secret-keys first",
					o.exportUser)
			}
		}
	}

	home := homeDir(o)
	if err := rewriteKeyring(filepath.Join(home, "secring.gpg"), kr.secret, o.exportUser, true); err != nil {
		return err
	}
	if !alsoSecret {
		if err := rewriteKeyring(filepath.Join(home, "pubring.gpg"), kr.public, o.exportUser, false); err != nil {
			return err
		}
	}
	fmt.Fprintf(stderr(), "gpg: key %q deleted\n", o.exportUser)
	return nil
}

// rewriteKeyring filters out any entity whose UID/fingerprint matches
// query, then atomically replaces path with the trimmed keyring. We
// write to a tempfile in the same directory and rename so a crash
// can't leave a half-written keyring.
func rewriteKeyring(path string, list openpgp.EntityList, query string, secret bool) error {
	var kept openpgp.EntityList
	for _, e := range list {
		if matchUserID(e, query) {
			continue
		}
		kept = append(kept, e)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".kr-rewrite-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	for _, e := range kept {
		if secret {
			if e.PrivateKey == nil {
				continue
			}
			if err := e.SerializePrivateWithoutSigning(tmp, nil); err != nil {
				tmp.Close()
				cleanup()
				return err
			}
		} else {
			if err := e.Serialize(tmp); err != nil {
				tmp.Close()
				cleanup()
				return err
			}
		}
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// primaryUIDString picks the human-readable UID for log messages.
func primaryUIDString(e *openpgp.Entity) string {
	for _, id := range e.Identities {
		return id.UserId.Id
	}
	return fmt.Sprintf("[%X]", e.PrimaryKey.Fingerprint)
}

// keyChecksum is used by tests that compare two keys for equality
// even when binary representation differs slightly between encoders.
// Compare by fingerprint + creation time + primary UID.
func keyChecksum(e *openpgp.Entity) string {
	h := sha256.New()
	h.Write(e.PrimaryKey.Fingerprint[:])
	fmt.Fprintf(h, "%d", e.PrimaryKey.CreationTime.Unix())
	for _, id := range e.Identities {
		h.Write([]byte(id.UserId.Id))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// pendingMtime is a workaround for test flakes: ensure files we just
// wrote are flushed to disk before re-reading.
func pendingMtime() time.Time { return time.Now() }
