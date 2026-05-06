package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostKeyVerifier returns an ssh.HostKeyCallback that:
//
//   - if --insecure (-o StrictHostKeyChecking=no) is set, accepts every key
//   - otherwise checks against ~/.ssh/known_hosts (or the override path)
//   - on first connect, prompts the user to accept the fingerprint
//   - on key change, refuses the connection
func hostKeyVerifier(o *options) (ssh.HostKeyCallback, error) {
	if o.insecure {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	path := o.knownHostsPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".ssh", "known_hosts")
	}

	// Ensure the file exists; knownhosts.New errors out if not.
	if err := ensureFileExists(path); err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}

	hk, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if err := hk(hostname, remote, key); err == nil {
			return nil
		} else {
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) {
				if len(keyErr.Want) > 0 {
					// Mismatch — refuse.
					return fmt.Errorf("REMOTE HOST IDENTIFICATION HAS CHANGED for %s: known fingerprint mismatch", hostname)
				}
				// No known entry — TOFU prompt.
				if !promptAcceptKey(hostname, key) {
					return errors.New("host key not accepted")
				}
				return appendKnownHost(path, hostname, key)
			}
			return err
		}
	}, nil
}

// ensureFileExists creates an empty known_hosts file if it doesn't
// already exist, with parent dirs.
func ensureFileExists(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	return f.Close()
}

// promptAcceptKey shows the fingerprint and reads yes/no.
func promptAcceptKey(host string, key ssh.PublicKey) bool {
	fingerprint := ssh.FingerprintSHA256(key)
	fmt.Fprintf(os.Stderr, "The authenticity of host '%s' can't be established.\n", host)
	fmt.Fprintf(os.Stderr, "%s key fingerprint is %s.\n", key.Type(), fingerprint)
	fmt.Fprint(os.Stderr, "Are you sure you want to continue connecting (yes/no)? ")
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "yes" || line == "y"
}

// appendKnownHost appends one host-key entry in OpenSSH known_hosts
// format. We use `knownhosts.Line` which produces a non-hashed entry —
// hashed entries are deferred (FUTURE.md).
func appendKnownHost(path, hostname string, key ssh.PublicKey) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	line := knownhosts.Line([]string{hostname}, key)
	if _, err := f.WriteString(line + "\n"); err != nil {
		return err
	}
	return nil
}
