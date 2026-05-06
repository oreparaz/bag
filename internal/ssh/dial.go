package ssh

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"golang.org/x/crypto/ssh"
)

// connectAndRun is the top-level orchestrator: build the client config,
// dial, authenticate, then either run a command or open a shell.
func connectAndRun(o *options) error {
	authMethods, err := loadAuthMethods(o)
	if err != nil {
		return err
	}
	hostKey, err := hostKeyVerifier(o)
	if err != nil {
		return err
	}

	cfg := &ssh.ClientConfig{
		User:            o.user,
		Auth:            authMethods,
		HostKeyCallback: hostKey,
		Timeout:         15 * time.Second,
		// Negotiate sane modern algorithms; the stdlib default is
		// reasonable but we exclude legacy ssh-rsa-with-SHA1.
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519,
			ssh.KeyAlgoECDSA256,
			ssh.KeyAlgoECDSA384,
			ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512,
			ssh.KeyAlgoRSASHA256,
		},
	}

	addr := net.JoinHostPort(o.host, strconv.Itoa(o.port))
	if o.verbose {
		fmt.Fprintf(stderrW(), "ssh: connecting to %s as %s\n", addr, o.user)
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer client.Close()

	return runSession(client, o)
}
