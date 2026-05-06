package ssh

import (
	"fmt"

	"github.com/oreparaz/bag/internal/sshconn"
)

// connectAndRun is the top-level orchestrator: build the client config,
// dial via sshconn, then either run a command or open a shell.
func connectAndRun(o *options) error {
	client, err := sshconn.Dial(sshconn.Options{
		User:           o.user,
		Host:           o.host,
		Port:           o.port,
		IdentityFile:   o.identityFile,
		KnownHostsPath: o.knownHostsPath,
		Insecure:       o.insecure,
		Verbose:        o.verbose,
	})
	if err != nil {
		return fmt.Errorf("dial %s: %w", o.host, err)
	}
	defer client.Close()
	return runSession(client, o)
}
