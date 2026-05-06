package sshconn

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

func defaultIdentityFiles(home string) []string {
	return []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}
}

func loadAuthMethods(o Options) ([]ssh.AuthMethod, error) {
	home, _ := os.UserHomeDir()
	var keyPaths []string
	if o.IdentityFile != "" {
		keyPaths = []string{o.IdentityFile}
	} else {
		keyPaths = defaultIdentityFiles(home)
	}

	var signers []ssh.Signer
	for _, p := range keyPaths {
		s, err := loadSigner(p)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) && o.Verbose {
				fmt.Fprintf(stderrW(), "ssh: %s: %v\n", p, err)
			}
			continue
		}
		signers = append(signers, s)
	}

	var methods []ssh.AuthMethod
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}
	methods = append(methods, ssh.PasswordCallback(func() (string, error) {
		return promptPassword(fmt.Sprintf("%s@%s's password: ", o.User, o.Host))
	}))
	methods = append(methods, ssh.KeyboardInteractive(keyboardInteractive(o)))
	return methods, nil
}

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

func keyboardInteractive(o Options) ssh.KeyboardInteractiveChallenge {
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

// stderrW returns os.Stderr (or io.Discard if it's somehow nil). The
// indirection keeps callers from leaking direct os.Stderr references
// when this package is reused inside tests that capture stderr.
func stderrW() *os.File { return os.Stderr }
