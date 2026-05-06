// Package scp implements the bag drop-in replacement for scp.
//
// We speak the classic SCP protocol over an exec'd remote `scp -t/-f`,
// matching what every standard sshd ships. SFTP-based scp (newer
// openssh default) is intentionally deferred — see FUTURE.md.
//
// Wire protocol (per the de-facto convention; there is no RFC):
//
//	C<mode> <size> <name>\n   begin a regular file
//	D<mode> <size> <name>\n   begin a directory (size always 0)
//	E\n                       end the current directory
//	T<mtime> <0> <atime> <0>\n  set times for the next entry (with -p)
//	\x00                      ack between every record / file
//	\x01<msg>\n / \x02<msg>\n  warning / fatal error from remote
//
// 80% surface:
//
//	scp [-r] [-p] [-q] [-P PORT] [-i IDENT] SRC ... DST
//
// SRC and DST may be local paths or [USER@]HOST:PATH. Mixed direction
// (some sources local, some remote) and remote-to-remote copies are
// not supported in v1.
package scp

func Main(args []string) int { return run(args) }
