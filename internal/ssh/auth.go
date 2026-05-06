package ssh

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// defaultIdentityFiles lists the private-key paths we try when -i is
// not given. Order matches openssh's preference (Ed25519 → ECDSA → RSA).
func defaultIdentityFiles(home string) []string {
	return []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}
}

// loadAuthMethods builds the []ssh.AuthMethod list:
//
//  1. PublicKeys parsed from explicit -i (or default identities)
//  2. KeyboardInteractive / Password prompt as a fallback
//
// Encrypted keys are decrypted via passphrase prompt.
func loadAuthMethods(o *options) ([]ssh.AuthMethod, error) {
	home, _ := os.UserHomeDir()
	var keyPaths []string
	if o.identityFile != "" {
		keyPaths = []string{o.identityFile}
	} else {
		keyPaths = defaultIdentityFiles(home)
	}

	var signers []ssh.Signer
	for _, p := range keyPaths {
		s, err := loadSigner(p)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && o.verbose {
				fmt.Fprintf(os.Stderr, "ssh: %s: %v\n", p, err)
			}
			continue
		}
		signers = append(signers, s)
	}

	var methods []ssh.AuthMethod
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	// Always offer password as a last resort. openssh-equivalent.
	methods = append(methods, ssh.PasswordCallback(func() (string, error) {
		return promptPassword(fmt.Sprintf("%s@%s's password: ", o.user, o.host))
	}))

	// Keyboard-interactive (prompts for many-step challenges).
	methods = append(methods, ssh.KeyboardInteractive(keyboardInteractive(o)))

	return methods, nil
}

// loadSigner reads a private key from path. If the key is encrypted,
// prompts for a passphrase.
func loadSigner(path string) (ssh.Signer, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err == nil {
		return signer, nil
	}
	var pmErr *ssh.PassphraseMissingError
	if errors.As(err, &pmErr) {
		pass, perr := promptPassword(fmt.Sprintf("Enter passphrase for %s: ", path))
		if perr != nil {
			return nil, perr
		}
		return ssh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
	}
	return nil, err
}

// promptPassword reads a line from /dev/tty with echo off. Falls back
// to stdin echo-off when /dev/tty isn't available.
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr)

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil {
		defer tty.Close()
		bytes, err := term.ReadPassword(int(tty.Fd()))
		if err != nil {
			return "", err
		}
		return string(bytes), nil
	}
	bytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// keyboardInteractive returns a callback that surfaces server prompts
// to the user and echoes their typed answers back to the SSH layer.
// We turn echo off for prompts where the server flagged the answer as
// "secret" via the prompt-echo flag.
func keyboardInteractive(o *options) ssh.KeyboardInteractiveChallenge {
	return func(name, instruction string, questions []string, echos []bool) ([]string, error) {
		if name != "" {
			fmt.Fprintln(os.Stderr, name)
		}
		if instruction != "" {
			fmt.Fprintln(os.Stderr, instruction)
		}
		answers := make([]string, len(questions))
		for i, q := range questions {
			if echos[i] {
				fmt.Fprint(os.Stderr, q)
				var line string
				_, err := fmt.Fscanln(os.Stdin, &line)
				if err != nil && err.Error() != "unexpected newline" {
					return nil, err
				}
				answers[i] = line
				continue
			}
			ans, err := promptPassword(q)
			if err != nil {
				return nil, err
			}
			answers[i] = ans
		}
		return answers, nil
	}
}
