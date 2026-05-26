package gpgcmd

import (
	"crypto"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// mapHash converts a gpg --digest-algo name to a crypto.Hash. Empty
// string means "library default" (currently SHA-256).
func mapHash(name string) crypto.Hash {
	switch strings.ToUpper(name) {
	case "":
		return 0
	case "MD5":
		return crypto.MD5
	case "SHA1", "SHA-1":
		return crypto.SHA1
	case "RIPEMD160", "RIPEMD-160":
		return crypto.RIPEMD160
	case "SHA224", "SHA-224":
		return crypto.SHA224
	case "SHA256", "SHA-256":
		return crypto.SHA256
	case "SHA384", "SHA-384":
		return crypto.SHA384
	case "SHA512", "SHA-512":
		return crypto.SHA512
	}
	return 0
}

// mapCipher converts a gpg --cipher-algo name to a packet.CipherFunction.
// Returns zero (library default) for empty input.
func mapCipher(name string) packet.CipherFunction {
	switch strings.ToUpper(name) {
	case "":
		return 0
	case "3DES", "TRIPLEDES":
		return packet.Cipher3DES
	case "CAST5":
		return packet.CipherCAST5
	case "AES", "AES128", "AES-128":
		return packet.CipherAES128
	case "AES192", "AES-192":
		return packet.CipherAES192
	case "AES256", "AES-256":
		return packet.CipherAES256
	}
	return 0
}

// mapCompress converts a gpg --compress-algo name to a
// packet.CompressionAlgo.
func mapCompress(name string) packet.CompressionAlgo {
	switch strings.ToLower(name) {
	case "":
		return packet.CompressionZIP
	case "none", "uncompressed":
		return packet.CompressionNone
	case "zip":
		return packet.CompressionZIP
	case "zlib":
		return packet.CompressionZLIB
	}
	return packet.CompressionZIP
}
