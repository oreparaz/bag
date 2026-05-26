package gpgcmd

import (
	"fmt"
	"io"
)

func printVersion(w io.Writer) {
	fmt.Fprintln(w, "gpg (bag) 2.4-compat")
	fmt.Fprintln(w, "Compatible with OpenPGP messages from GnuPG.")
}

func printHelp(w io.Writer) {
	fmt.Fprint(w, `Usage: gpg [OPTION]... [FILE]...
Bag's drop-in subset of GnuPG.

Commands:
  -e, --encrypt              encrypt data (needs -r RECIPIENT)
  -c, --symmetric            encrypt with a passphrase only
  -d, --decrypt              decrypt data (symmetric or public-key)
  -s, --sign                 make an inline signature
  -b, --detach-sign          make a detached signature
      --clearsign            make a cleartext signature
      --verify [SIG] [FILE]  verify a signature
      --gen-key              interactively generate a key pair
      --quick-gen-key USER-ID [ALGO]
      -k, --list-keys        list public keys
      -K, --list-secret-keys list secret keys
      --import [FILE]...     import keys
      --export [USER]        export public keys
      --export-secret-keys   export secret keys
  -h, --help                 this help
      --version              version

Options:
  -a, --armor                ASCII-armor output
  -o, --output FILE          write output to FILE
  -r, --recipient USER       encrypt for USER
  -u, --local-user USER      sign as USER
      --passphrase STRING    use STRING as passphrase
      --passphrase-fd N      read passphrase from fd N
      --batch / --yes        non-interactive mode
      --digest-algo NAME     SHA256 / SHA512 / ...
      --cipher-algo NAME     AES / AES256 / 3DES / CAST5
      --compress-algo NAME   ZIP / ZLIB / NONE
      --homedir DIR          override GnuPG home (default ~/.gnupg)
      --textmode             canonicalize CRLFs before signing
`)
}
