package gpgcmd

import (
	"fmt"
	"strings"
)

// action is which top-level operation the user asked for. Exactly one
// is set per invocation; the dispatch in run() picks based on the
// argv flags.
type action int

const (
	actionNone action = iota
	actionEncryptSymmetric
	actionEncryptPublic
	actionDecrypt
	actionSign
	actionDetachSign
	actionClearsign
	actionVerify
	actionGenKey
	actionQuickGenKey
	actionListKeys
	actionListSecretKeys
	actionImport
	actionExport
	actionExportSecret
	actionDeleteKeys
	actionDeleteSecretKeys
	actionEnarmor
	actionDearmor
	actionPrintMD
	actionListPackets
	actionShowKeys
	actionHelp
	actionVersion
)

type options struct {
	act action
	// alsoSign means --sign was passed alongside --encrypt; the
	// encrypted message gets a signature wrapped inside it.
	alsoSign bool

	// I/O.
	input  string // positional input file; "" means stdin.
	output string // -o / --output; "" means stdout.

	// Encryption / signing options.
	recipients []string // -r / --recipient (one or more)
	localUser  string   // --local-user / -u (signer identity)
	armor      bool     // -a / --armor
	textMode   bool     // -t / --textmode
	symmetric  bool     // -c when no -r given
	digest     string   // --digest-algo (sha256 etc)
	cipher     string   // --cipher-algo (aes256 etc)
	compress   string   // --compress-algo (zip/zlib/bzip2)

	// Verification: when verifying a detached signature, signatureFile
	// is the .sig and input is the data file.
	signatureFile string

	// Key-gen knobs.
	keyType       string // rsa, dsa, ed25519, default
	keyBits       int
	keyName       string // --quick-gen-key user-id positional
	keyExpiry     string // 0 = never, or duration like "1y"
	keyUsage      string // default sign,encrypt

	// Import/export targets.
	importFiles []string
	exportUser  string

	// Passphrase + batch.
	passphrase    string // --passphrase
	passphraseFD  int    // --passphrase-fd N (-1 if unset)
	batch         bool   // --batch
	yes           bool   // --yes
	noTTY         bool   // --no-tty

	// Misc.
	homeDir    string // --homedir
	verbose    int    // -v repeated
	quiet      bool   // -q
	withColons bool   // --with-colons (machine-readable list)
}

func parseArgs(argv []string) (*options, error) {
	o := &options{passphraseFD: -1}
	var positional []string

	pickArg := func(i *int, name string) (string, error) {
		if *i+1 >= len(argv) {
			return "", fmt.Errorf("option %s requires an argument", name)
		}
		*i++
		return argv[*i], nil
	}

	for i := 0; i < len(argv); i++ {
		a := argv[i]
		// Long forms: --name or --name=value
		if strings.HasPrefix(a, "--") {
			name := a[2:]
			val := ""
			hasEq := false
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				val = name[eq+1:]
				name = name[:eq]
				hasEq = true
			}
			next := func() (string, error) {
				if hasEq {
					return val, nil
				}
				return pickArg(&i, "--"+name)
			}

			switch name {
			case "encrypt":
				if o.act == actionNone {
					o.act = actionEncryptPublic
				}
			case "symmetric":
				if o.act == actionNone {
					o.act = actionEncryptSymmetric
				} else if o.act == actionEncryptPublic {
					// gpg --symmetric --encrypt -r U: both. Treat as
					// public-key with a passphrase wrapper isn't
					// supported; fall back to public-key only.
				}
				o.symmetric = true
			case "decrypt":
				o.act = actionDecrypt
			case "sign":
				if o.act == actionNone {
					o.act = actionSign
				} else if o.act == actionEncryptPublic || o.act == actionEncryptSymmetric {
					o.alsoSign = true
				}
			case "detach-sign":
				o.act = actionDetachSign
			case "clearsign":
				o.act = actionClearsign
			case "verify":
				o.act = actionVerify
			case "gen-key", "generate-key", "full-generate-key":
				o.act = actionGenKey
			case "quick-gen-key", "quick-generate-key":
				o.act = actionQuickGenKey
			case "list-keys", "list-public-keys":
				o.act = actionListKeys
			case "list-secret-keys", "list-private-keys":
				o.act = actionListSecretKeys
			case "import":
				o.act = actionImport
			case "export":
				o.act = actionExport
			case "export-secret-keys", "export-secret-subkeys":
				o.act = actionExportSecret
			case "delete-keys", "delete-key":
				o.act = actionDeleteKeys
			case "delete-secret-keys", "delete-secret-key":
				o.act = actionDeleteSecretKeys
			case "delete-secret-and-public-keys", "delete-secret-and-public-key":
				o.act = actionDeleteSecretKeys // we'll also clear pub
			case "enarmor":
				o.act = actionEnarmor
			case "dearmor":
				o.act = actionDearmor
			case "print-md", "print-mds":
				o.act = actionPrintMD
			case "list-packets":
				o.act = actionListPackets
			case "show-keys", "show-key":
				o.act = actionShowKeys
			case "with-colons":
				o.withColons = true
			case "help":
				o.act = actionHelp
			case "version":
				o.act = actionVersion

			case "recipient":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.recipients = append(o.recipients, v)
			case "local-user":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.localUser = v
			case "output":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.output = v
			case "armor":
				o.armor = true
			case "no-armor", "no-armour":
				o.armor = false
			case "textmode":
				o.textMode = true
			case "passphrase":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.passphrase = v
			case "passphrase-fd":
				v, err := next()
				if err != nil {
					return nil, err
				}
				fmt.Sscanf(v, "%d", &o.passphraseFD)
			case "passphrase-file":
				v, err := next()
				if err != nil {
					return nil, err
				}
				// Defer reading until run() — file might be a pipe.
				o.passphrase = "@file:" + v
			case "batch":
				o.batch = true
			case "yes":
				o.yes = true
			case "no-tty":
				o.noTTY = true
			case "homedir":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.homeDir = v
			case "digest-algo":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.digest = v
			case "cipher-algo":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.cipher = v
			case "compress-algo", "compression-algo":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.compress = v
			case "expire-date":
				v, err := next()
				if err != nil {
					return nil, err
				}
				o.keyExpiry = v
			case "verbose":
				o.verbose++
			case "quiet":
				o.quiet = true
			case "":
				// `--` end-of-options.
				positional = append(positional, argv[i+1:]...)
				i = len(argv)
			default:
				// gpg accepts many ignored options for compatibility.
				// Swallow unknown long flags rather than erroring;
				// power users can pipe through real gpg if they need
				// the long tail.
				if hasEq {
					// already consumed val
				}
			}
			continue
		}
		// Short forms: clustering allowed (-ca = -c -a). Each short
		// flag may take its own argument.
		if strings.HasPrefix(a, "-") && a != "-" && len(a) > 1 {
			for j := 1; j < len(a); j++ {
				switch a[j] {
				case 'c':
					if o.act == actionNone {
						o.act = actionEncryptSymmetric
					}
					o.symmetric = true
				case 'e':
					if o.act == actionNone {
						o.act = actionEncryptPublic
					}
				case 'd':
					o.act = actionDecrypt
				case 's':
					if o.act == actionNone {
						o.act = actionSign
					} else if o.act == actionEncryptPublic || o.act == actionEncryptSymmetric {
						o.alsoSign = true
					}
				case 'b':
					o.act = actionDetachSign
				case 'v':
					o.verbose++
				case 'q':
					o.quiet = true
				case 'a':
					o.armor = true
				case 't':
					o.textMode = true
				case 'k':
					// -k is GPG's "--list-keys" shortcut.
					o.act = actionListKeys
				case 'K':
					o.act = actionListSecretKeys
				case 'r':
					// -r REC (or rest of cluster as arg)
					rest := a[j+1:]
					if rest != "" {
						o.recipients = append(o.recipients, rest)
						j = len(a)
					} else {
						v, err := pickArg(&i, "-r")
						if err != nil {
							return nil, err
						}
						o.recipients = append(o.recipients, v)
					}
					j = len(a)
				case 'u':
					rest := a[j+1:]
					if rest != "" {
						o.localUser = rest
					} else {
						v, err := pickArg(&i, "-u")
						if err != nil {
							return nil, err
						}
						o.localUser = v
					}
					j = len(a)
				case 'o':
					rest := a[j+1:]
					if rest != "" {
						o.output = rest
					} else {
						v, err := pickArg(&i, "-o")
						if err != nil {
							return nil, err
						}
						o.output = v
					}
					j = len(a)
				case 'h':
					o.act = actionHelp
				default:
					// silently ignore unknown short flag
				}
			}
			continue
		}
		positional = append(positional, a)
	}

	// Dispatch positional args based on action.
	switch o.act {
	case actionImport, actionShowKeys:
		o.importFiles = positional
	case actionExport, actionExportSecret, actionListKeys, actionListSecretKeys,
		actionDeleteKeys, actionDeleteSecretKeys:
		if len(positional) > 0 {
			o.exportUser = positional[0]
		}
	case actionVerify:
		// `gpg --verify SIG [DATA]`. With one arg the sig is also the
		// data (inline / clearsign); with two it's detached.
		if len(positional) >= 1 {
			o.signatureFile = positional[0]
		}
		if len(positional) >= 2 {
			o.input = positional[1]
		}
	case actionPrintMD:
		// --print-md ALGO [FILE...]
		if len(positional) >= 1 {
			o.digest = positional[0]
		}
		if len(positional) >= 2 {
			o.importFiles = positional[1:]
		}
	case actionQuickGenKey:
		// `--quick-gen-key USER-ID [ALGO [USAGE [EXPIRE]]]`
		if len(positional) >= 1 {
			o.keyName = positional[0]
		}
		if len(positional) >= 2 {
			o.keyType = positional[1]
		}
		if len(positional) >= 3 {
			o.keyUsage = positional[2]
		}
		if len(positional) >= 4 {
			o.keyExpiry = positional[3]
		}
	default:
		if len(positional) > 0 {
			o.input = positional[0]
		}
	}

	return o, nil
}
