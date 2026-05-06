package sshconn

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

func hostKeyVerifier(o Options) (ssh.HostKeyCallback, error) {
	if o.Insecure {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	path := o.KnownHostsPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
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
					return fmt.Errorf("REMOTE HOST IDENTIFICATION HAS CHANGED for %s: known fingerprint mismatch", hostname)
				}
				if !promptAcceptKey(hostname, key) {
					return errors.New("host key not accepted")
				}
				return appendKnownHost(path, hostname, key)
			}
			return err
		}
	}, nil
}

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
