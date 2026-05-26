package gpgcmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// doShowKeys reads one or more keyring files and prints them in the
// same format as --list-keys, but without writing anything to the
// home directory. The bag equivalent of `gpg --show-keys file.asc`.
//
// When --with-colons is set, the machine-readable colon form is used.
func doShowKeys(o *options) error {
	files := o.importFiles
	if len(files) == 0 {
		files = []string{"-"}
	}
	out, _, err := openOutput(o, "")
	if err != nil {
		return err
	}
	defer out.Close()
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
		for _, e := range es {
			if o.withColons {
				printEntityColons(out, e, false)
			} else {
				printEntity(out, e, false)
			}
		}
	}
	return nil
}

// printEntityColons emits the colon-separated record format documented
// in DETAILS in the gpg source tree. We only fill the fields scripts
// actually rely on (record type, length, algo, key id, dates, usage,
// fingerprint, uid). Empty fields are kept as the literal empty
// position so column indexing stays stable.
//
// Record types we emit:
//
//	pub : primary public key
//	sec : primary secret key
//	uid : user id
//	fpr : fingerprint
//	sub : public subkey
//	ssb : secret subkey
func printEntityColons(w io.Writer, e *openpgp.Entity, secret bool) {
	primaryRec := "pub"
	if secret {
		primaryRec = "sec"
	}
	pk := e.PrimaryKey
	bits, _ := pk.BitLength()
	algoNum := pubAlgoNumber(pk.PubKeyAlgo)
	created := pk.CreationTime.Unix()
	expires := selfSigExpiry(e)
	usage := keyUsageTag(e)
	keyid := fmt.Sprintf("%016X", pk.KeyId)
	// Fields 1..15:
	// 1 record type
	// 2 validity (always "u" — bag does not track web of trust)
	// 3 key length in bits
	// 4 public-key algorithm number
	// 5 key id (16 hex chars)
	// 6 creation date (epoch seconds)
	// 7 expiration date (epoch seconds, or empty)
	// 8 cert/serial (unused for keys)
	// 9 ownertrust (always empty here)
	// 10 user id (only on uid lines)
	// 11 sig class (only on sig lines)
	// 12 key capabilities
	// 13 issuer fpr (only on signature/cert lines)
	// 14 flag (e.g. 0=normal)
	// 15 token serial
	fmt.Fprintf(w, "%s:u:%d:%d:%s:%d:%s:::::%s::::\n",
		primaryRec, bits, algoNum, keyid, created, stringOrEmpty(expires), usage)
	fmt.Fprintf(w, "fpr:::::::::%X:\n", pk.Fingerprint)
	for _, id := range e.Identities {
		// uid line: 10th field carries the UID; gpg escapes some
		// characters but plain ASCII UIDs survive a raw write. We use
		// the self-signature's creation time as the UID timestamp,
		// because packet.UserId itself doesn't carry one.
		var uidTime int64
		if id.SelfSignature != nil {
			uidTime = id.SelfSignature.CreationTime.Unix()
		}
		fmt.Fprintf(w, "uid:u::::%d::::%s:\n",
			uidTime, colonEscape(id.UserId.Id))
	}
	for _, sub := range e.Subkeys {
		subRec := "sub"
		if secret {
			subRec = "ssb"
		}
		sb, _ := sub.PublicKey.BitLength()
		fmt.Fprintf(w, "%s:u:%d:%d:%016X:%d:%s:::::%s::::\n",
			subRec, sb,
			pubAlgoNumber(sub.PublicKey.PubKeyAlgo),
			sub.PublicKey.KeyId,
			sub.PublicKey.CreationTime.Unix(),
			stringOrEmpty(subkeyExpiry(sub)),
			subkeyUsageTag(sub),
		)
		fmt.Fprintf(w, "fpr:::::::::%X:\n", sub.PublicKey.Fingerprint)
	}
}

// pubAlgoNumber maps the library's PublicKeyAlgorithm to RFC 4880's
// algorithm number — what gpg --with-colons puts in field 4.
func pubAlgoNumber(a packet.PublicKeyAlgorithm) int {
	switch a {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSAEncryptOnly, packet.PubKeyAlgoRSASignOnly:
		return 1
	case packet.PubKeyAlgoElGamal:
		return 16
	case packet.PubKeyAlgoDSA:
		return 17
	case packet.PubKeyAlgoECDH:
		return 18
	case packet.PubKeyAlgoECDSA:
		return 19
	case packet.PubKeyAlgoEdDSA:
		return 22
	}
	return 0
}

// selfSigExpiry returns the expiration timestamp for the primary key,
// derived from the primary identity's self-signature, or 0 when the
// key never expires.
func selfSigExpiry(e *openpgp.Entity) int64 {
	for _, id := range e.Identities {
		s := id.SelfSignature
		if s == nil {
			continue
		}
		if s.KeyLifetimeSecs == nil || *s.KeyLifetimeSecs == 0 {
			return 0
		}
		return e.PrimaryKey.CreationTime.Unix() + int64(*s.KeyLifetimeSecs)
	}
	return 0
}

func subkeyExpiry(s openpgp.Subkey) int64 {
	if s.Sig == nil || s.Sig.KeyLifetimeSecs == nil || *s.Sig.KeyLifetimeSecs == 0 {
		return 0
	}
	return s.PublicKey.CreationTime.Unix() + int64(*s.Sig.KeyLifetimeSecs)
}

func stringOrEmpty(t int64) string {
	if t == 0 {
		return ""
	}
	return fmt.Sprintf("%d", t)
}

// colonEscape replaces literal colons with their gpg escape form so a
// UID with a colon inside doesn't corrupt the record. gpg also escapes
// control chars; bag keeps it minimal.
func colonEscape(s string) string {
	return strings.ReplaceAll(s, ":", `\x3a`)
}
