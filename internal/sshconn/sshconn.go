// Package sshconn is the shared connect/auth/known-hosts layer used by
// bag's ssh and scp tools. It centralises the policy (where to find
// identity files, what to do on a host-key mismatch, how to prompt for
// passwords) so the two front-ends can stay focused on their own
// vocabulary (sessions vs. file transfer).
package sshconn

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

// Options carries the bits of configuration both ssh and scp need.
// User, Host, Port are required. Everything else is optional.
type Options struct {
	User string
	Host string
	Port int

	// IdentityFile, when set, overrides the default identity-file
	// search (~/.ssh/id_ed25519, id_ecdsa, id_rsa).
	IdentityFile string

	// KnownHostsPath, when set, overrides ~/.ssh/known_hosts.
	KnownHostsPath string

	// Insecure disables host-key verification entirely.
	// Equivalent to -o StrictHostKeyChecking=no. Off by default.
	Insecure bool

	// Verbose makes Dial log a connection summary to stderr.
	Verbose bool
}

// Dial builds a fresh ssh.Client for opts. It applies bag's preferred
// host-key algorithms (no SHA-1 RSA), wires in our auth + known_hosts
// callbacks, and uses a 15-second handshake timeout.
func Dial(opts Options) (*ssh.Client, error) {
	auth, err := loadAuthMethods(opts)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyVerifier(opts)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            opts.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         15 * time.Second,
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519,
			ssh.KeyAlgoECDSA256,
			ssh.KeyAlgoECDSA384,
			ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512,
			ssh.KeyAlgoRSASHA256,
		},
	}

	port := opts.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(opts.Host, strconv.Itoa(port))
	if opts.Verbose {
		fmt.Fprintf(stderrW(), "ssh: connecting to %s as %s\n", addr, opts.User)
	}
	return ssh.Dial("tcp", addr, cfg)
}
