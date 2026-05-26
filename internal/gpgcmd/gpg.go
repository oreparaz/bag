// Package gpgcmd implements bag's `gpg` tool — a subset of GnuPG built
// on golang.org/ProtonMail/go-crypto. The goal is interoperable
// OpenPGP: messages produced by bag-gpg can be decrypted/verified by
// the system gpg, and vice versa.
//
// Implemented:
//
//	gpg -c FILE            symmetric (passphrase) encrypt
//	gpg -d / --decrypt     decrypt (symmetric or public-key)
//	gpg --encrypt -r U     public-key encrypt
//	gpg --sign FILE        inline sign
//	gpg --detach-sign      detached signature
//	gpg --clearsign        cleartext signature
//	gpg --verify [SIG] F   verify
//	gpg --gen-key          interactive key generation
//	gpg --quick-gen-key    one-shot key generation
//	gpg --list-keys        list public keys
//	gpg --list-secret-keys list secret keys
//	gpg --import [F]       import a keyring
//	gpg --export [USER]    export public keys
//	gpg --export-secret-keys
//	-a / --armor           ASCII-armor output (default for stdout)
//	--passphrase X         non-interactive passphrase
//	--batch / --yes        non-interactive mode
//	--output FILE / -o     redirect output
//
// Not implemented: --edit-key, smartcard, web of trust, --refresh-keys,
// dirmngr, --list-packets. Bag's gpg targets the encrypt / decrypt /
// sign / verify path that almost every user actually runs.
package gpgcmd

// Main is the entry point called by the multicall dispatcher.
func Main(args []string) int { return run(args) }
