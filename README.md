# bag

A bag of memory-safe command-line tools, written in Go.

`bag` is a single multicall binary (busybox-style) that ships drop-in
replacements for common Unix tools. The replacements aim to behave
identically to the originals for the most-used ~80% of features.

Why?
- **Memory safety.** No `unsafe`, no cgo. Just the Go standard library.
- **Single static binary.** `CGO_ENABLED=0`. No shared-library surprises.
- **Boring.** It's supposed to be transparent. You shouldn't notice.

## Tools shipped today

| Tool     | Status | Notes                                                      |
|----------|--------|------------------------------------------------------------|
| `curl`   | usable | HTTP/HTTPS only                                            |
| `wget`   | usable | HTTP/HTTPS only; recursive `-r` is shallow                 |
| `cat`    | usable | -n, -b, -s, -E, -T, -A, -v; binary-faithful by default     |
| `head`   | usable | -n / -c with `-N` (all but last) and unit suffixes         |
| `tail`   | usable | -n / -c with `+N`; -f follow mode is *not* implemented     |
| `base64` | usable | -d, -w, -i; GNU-compatible alphabet                        |
| `tee`    | usable | -a, -i; standard pipeline use                              |

See [FUTURE.md](FUTURE.md) for the wish list.

## Usage

There are three equivalent ways to invoke a tool:

```sh
# 1. Subcommand
bag curl https://example.com

# 2. Symlink (transparent drop-in)
ln -s bag curl
./curl https://example.com

# 3. argv[0] dispatch — rename the binary
cp bag /usr/local/bin/curl
curl https://example.com
```

## Build

```sh
CGO_ENABLED=0 go build -o bag ./
```

## Test

```sh
# Unit tests
go test ./...

# Conformance tests against real curl/wget on this host
go test ./test/conformance/...

# Conformance tests across 4 Linux distros (requires Docker)
make docker-test
```

## Scope and non-goals

The 80% of curl/wget that everyone actually uses is in scope. The exotic
long tail is not. See [FUTURE.md](FUTURE.md) for ideas; opening an issue
to bump priority is welcome.

Explicitly out of scope:

- Non-HTTP protocols (FTP, SCP, SFTP, gopher, SMTP, IMAP, POP3, …)
- NTLM/SPNEGO/GSSAPI authentication
- Client certificates (planned, not yet)
- HTTP/3
- IDN

## License

MIT. See [LICENSE](LICENSE).
