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

| Tool       | Status | Notes                                                      |
|------------|--------|------------------------------------------------------------|
| `curl`     | usable | HTTP/HTTPS only                                            |
| `wget`     | usable | HTTP/HTTPS only; recursive `-r` is shallow                 |
| `cat`      | usable | -n, -b, -s, -E, -T, -A, -v; binary-faithful by default     |
| `head`     | usable | -n / -c with `-N` (all but last) and unit suffixes         |
| `tail`     | usable | -n / -c with `+N`; -f / -F follow at 200 ms (configurable) |
| `base64`   | usable | -d, -w, -i; GNU-compatible alphabet                        |
| `tee`      | usable | -a, -i; standard pipeline use                              |
| `wc`       | usable | -l, -w, -c, -m, -L; locale-aware -m matches GNU            |
| `xxd`      | usable | hex dump + reverse, -p, -c, -g, -s, -l, -u                 |
| `gzip`     | usable | + `gunzip` and `zcat` aliases                              |
| `bzip2`    | usable | + `bunzip2` and `bzcat` aliases                            |
| `xz`       | usable | + `unxz` and `xzcat` aliases                               |
| `zstd`     | usable | + `unzstd` and `zstdcat` aliases                           |
| `tar`      | usable | -c/-x/-t with -z/-j/-J/--zstd, -C, --exclude, --strip-components |
| `zip`      | usable | -r recurse, -j junk paths, -q                              |
| `unzip`    | usable | -l list, -p stdout, -d DIR, -j, -o, -n                     |
| `grep`     | usable | RE2 backend; -i -v -c -l -L -n -r -E -F -w -A/-B/-C        |
| `sed`      | subset | s/// with g/i/p/Nth, d, p, q, addresses, -i in-place       |
| `cut`      | usable | -f/-c/-b LIST with -d, --output-delimiter, --complement    |
| `sort`     | usable | -n -r -u -f -b -k key -t sep -o; byte locale (C)           |
| `uniq`     | usable | -c -d -u -i -f -s -w                                       |
| `find`     | usable | -name/-iname/-path/-type/-size/-mtime/-prune/-exec/-delete |
| `hexdump`  | usable | -C / -b / -c / -d / -o / -x; -n -s -v; squeeze with `*`    |

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
