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
				// `Want` is empty means knownhosts.New found no entry that
				// matched our (host, port) tuple — but the bare hostname
				// might already exist in the file under a different port
				// or hash. If so, treat as a mismatch instead of prompting
				// the user to TOFU-accept any key for that host.
				bare := bareHost(hostname)
				if hasEntryForHost(path, bare) {
					return fmt.Errorf("REMOTE HOST IDENTIFICATION HAS CHANGED for %s: known entry exists for %s but no key matched (possible MITM)", hostname, bare)
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

// bareHost strips the [host]:port wrapper if present.
func bareHost(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

// hasEntryForHost reports whether the known_hosts file already contains
// a matching entry for the given bare hostname (any syntax/port).
func hasEntryForHost(path, host string) bool {
	if host == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hosts := fields[0]
		// Hashed entry — knownhosts already searched these via the
		// callback, so a hashed line that didn't match doesn't help us
		// here. Skip.
		if strings.HasPrefix(hosts, "|1|") {
			continue
		}
		for _, h := range strings.Split(hosts, ",") {
			// Strip optional [host]:port wrapper.
			if strings.HasPrefix(h, "[") {
				if end := strings.Index(h, "]"); end >= 0 {
					h = h[1:end]
				}
			}
			if strings.EqualFold(h, host) {
				return true
			}
		}
	}
	return false
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
	// Read from /dev/tty rather than os.Stdin: a script-driven invocation
	// (echo data | bag ssh ...) must not have its piped stdin parsed as
	// a "yes" to a host-key TOFU prompt.
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "no controlling terminal; refusing to accept new host key")
		return false
	}
	defer tty.Close()
	r := bufio.NewReader(tty)
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
