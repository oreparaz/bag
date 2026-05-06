// Package ssh implements a minimal ssh client for bag.
//
// What it does:
//
//   - public-key auth from default identity files (~/.ssh/id_ed25519,
//     id_ecdsa, id_rsa); falls back to interactive password
//   - host-key verification against ~/.ssh/known_hosts; on first
//     connection the user is asked to accept the fingerprint
//   - run a remote command (with stdin / stdout / stderr piped through)
//     or open an interactive shell (with PTY + raw mode locally)
//
// What it doesn't do (intentionally — see FUTURE.md):
//
//   - agent auth, ProxyJump, port forwarding, X11, sftp, scp
//   - complex ~/.ssh/config parsing
//   - hashed known_hosts entries (we accept them but don't write them)
//
// The implementation builds on golang.org/x/crypto/ssh.
package ssh

// Main is the bag-dispatch entry point.
func Main(args []string) int { return run(args) }
