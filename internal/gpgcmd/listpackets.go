package gpgcmd

import (
	"fmt"
	"io"
	"os"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// doListPackets dumps a one-line summary for each OpenPGP packet in
// the input stream. This is the bag analogue of `gpg --list-packets`,
// useful when debugging mangled keyrings, broken armor, or unexpected
// signature framing. The format is intentionally short — it covers
// just packet type, tag number, and one or two distinguishing fields
// (key id, fingerprint, signature hash) — but it's enough to spot the
// shape of a message without reaching for `pgpdump`.
func doListPackets(o *options) error {
	in, err := openInput(o)
	if err != nil {
		return err
	}
	defer in.Close()

	body, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	binary, err := dearmorIfNeeded(body)
	if err != nil {
		return err
	}

	r := packet.NewReader(byteReader(binary))
	for {
		p, err := r.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			fmt.Fprintf(os.Stdout, "# parse error: %v\n", err)
			return nil
		}
		describePacket(os.Stdout, p)
	}
}

func describePacket(w io.Writer, p packet.Packet) {
	switch x := p.(type) {
	case *packet.PublicKey:
		fmt.Fprintf(w, ":public key packet: algo %d, keyid %X\n",
			x.PubKeyAlgo, x.KeyId)
	case *packet.PrivateKey:
		fmt.Fprintf(w, ":secret key packet: algo %d, keyid %X\n",
			x.PubKeyAlgo, x.KeyId)
	case *packet.UserId:
		fmt.Fprintf(w, ":user ID packet: %q\n", x.Id)
	case *packet.Signature:
		fmt.Fprintf(w, ":signature packet: type=%d hash=%d pubkey=%d\n",
			x.SigType, x.Hash, x.PubKeyAlgo)
	case *packet.OnePassSignature:
		fmt.Fprintf(w, ":one-pass signature packet: type=%d hash=%d\n",
			x.SigType, x.Hash)
	case *packet.LiteralData:
		fmt.Fprintf(w, ":literal data packet: name=%q\n", x.FileName)
		io.Copy(io.Discard, x.Body)
	case *packet.Compressed:
		fmt.Fprintf(w, ":compressed packet\n")
		io.Copy(io.Discard, x.Body)
	case *packet.EncryptedKey:
		fmt.Fprintf(w, ":pubkey encrypted session key: keyid=%X algo=%d\n",
			x.KeyId, x.Algo)
	case *packet.SymmetricKeyEncrypted:
		fmt.Fprintf(w, ":symkey-encrypted session key: cipher=%d\n", x.CipherFunc)
	case *packet.SymmetricallyEncrypted:
		fmt.Fprintf(w, ":encrypted data packet\n")
	case *packet.OpaquePacket:
		fmt.Fprintf(w, ":opaque packet: tag=%d len=%d\n", x.Tag, len(x.Contents))
	default:
		fmt.Fprintf(w, ":unknown packet %T\n", p)
	}
}

// byteReader wraps a byte slice as an io.Reader; the library's
// packet.NewReader needs one, and bytes.NewReader allocates so this
// avoids a small import.
func byteReader(b []byte) io.Reader {
	return &sliceReader{data: b}
}

type sliceReader struct {
	data []byte
	off  int
}

func (r *sliceReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
