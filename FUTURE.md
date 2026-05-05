# Future features

A running list of features and tools we may want to add later. Items are
roughly grouped, not prioritized. Open an issue to bump something up.

## Known security limitations

Bag's archive and codec tools have been audited and hardened (see
`internal/safefs`), but a few residual risks are accepted for the 80%
target:

- **Curl / wget output to a pre-existing symlink**: real curl and wget
  follow symlinks at their `-o` / default destination. We match that
  for drop-in compatibility. If you pass an untrusted destination
  directory, set the cwd to a freshly-created tempdir.
- **TOCTOU between Lstat and OpenFile**: a sufficiently fast attacker
  could swap a directory for a symlink between the path-walk check and
  the leaf open. The `O_NOFOLLOW` on the leaf prevents the worst
  outcome, but opening through a fully-controlled symlink chain is not
  airtight without `openat`-style ascent (which Go doesn't expose
  portably). Tracking as a follow-up.
- **Decompression bombs**: gzip / bzip2 / xz / zstd readers stream, so
  memory is bounded. There is no `--max-output` flag, so disk space is
  the user's responsibility (matches GNU defaults).
- **Multipart curl bodies in memory**: `-F` builds the multipart body
  in a `bytes.Buffer` rather than streaming. Acceptable up to a few
  hundred MB; larger uploads should use real curl until we add a
  streaming multipart writer.
- **Sort loads everything in memory**: external sorting for >RAM input
  is not implemented. Use `LC_ALL=C sort -S 50%` with real sort for
  huge inputs.

## More tools to add to the bag

- `tr`
- `nc` / `netcat` (HTTP/TCP only)
- `dig` (basic A/AAAA/MX/TXT)
- `ssh-keygen` subset (Ed25519 only)
- `openssl` subset (`s_client`, `x509 -text`, `dgst`)
- `jq` subset
- `ping` (raw socket — needs cap_net_raw)
- `traceroute` (UDP only)
- `whois`
- `time` (the timing one, not the clock)
- `env`, `which`, `printenv`
- `awk` subset

## sed features intentionally deferred

- `a` / `i` / `c` (append, insert, change)
- Hold space commands: `h H g G x`
- `y` transliteration
- `b` / `t` branches with labels
- `{...}` grouping
- `r` / `w` / `R` / `W` file I/O
- POSIX BRE pickiness (we're RE2-flavored throughout)

## find features intentionally deferred

- `-perm` / `-user` / `-group` (numeric and named, with mode masks)
- `-regex` / `-iregex`
- `-printf` with format strings
- `-fprintf` / `-fprint` / `-fprint0`
- `-execdir` / `-okdir` / `-ok` (interactive prompt)
- `-cnewer` / `-anewer` / `-newerXY`
- `-fstype` / `-xdev`

## tar features intentionally deferred

- `-T LIST` files-from
- `--owner` / `--group` overrides
- Sparse file support
- ACLs / xattrs / SELinux contexts
- PAX-extended headers when *creating* (we accept them on read)

## zip features intentionally deferred

- ZipCrypto / AES encryption
- `-r0` store-only with `-r`
- `-X` strip extra fields
- Self-extracting archives

## curl features intentionally deferred

- HTTP/2 ALPN tuning, HTTP/3 / QUIC
- NTLM, SPNEGO/Negotiate, AWS SigV4 auth
- Client certificate auth (`--cert`, `--key`)
- mTLS in general, including PKCS#12 bundles
- `--resolve` host pinning
- `--unix-socket`, `--abstract-unix-socket`
- `--config` config files (`.curlrc`)
- `--write-out` full variable list (only the common ones today)
- DICT, FILE, FTP(S), GOPHER, IMAP(S), LDAP(S), MQTT, POP3(S), RTMP,
  RTSP, SCP, SFTP, SMB(S), SMTP(S), TELNET, TFTP
- DoH (`--doh-url`)
- `--alt-svc`, `--hsts`
- `--ssl-revoke-best-effort`, OCSP stapling controls
- `--interface`, `--local-port`
- Trace/dump (`--trace`, `--trace-ascii`, `--trace-time`)
- IDN/punycode hostnames
- Metalink, parallel transfers (`-Z`)

## wget features intentionally deferred

- FTP(S), retrieving via FTP login
- `--mirror`, `--page-requisites`, `--convert-links` (deep recursive
  scraping — current `-r` is shallow)
- `--load-cookies` / `--save-cookies` Netscape format full support
- `wget2` features (multithreaded, HSTS, OCSP, metalink)
- `--password-file`, `.netrc`, `.wgetrc`
- `--limit-rate`
- Authentication beyond Basic
- `--warc-*` archiving

## tail features intentionally deferred

- `-f` / `-F` follow modes (and `--retry`, `--max-unchanged-stats`,
  `--pid`)
- `--sleep-interval`, `--max-unchanged-stats` for follow mode

## cat features intentionally deferred

- POSIX `cat -u` already accepted as a no-op, since we line-flush at exit.
- `-A` rendering of multibyte / locale-specific bytes — we render bytes,
  not runes (matches GNU cat).

## Cross-cutting

- Shell completions (bash/zsh/fish)
- `bag --list` to enumerate available tools
- `bag --install /usr/local/bin` to symlink everything
- A `bag doctor` that diffs behavior against the real tool on the host
- Reproducible builds (`-trimpath`, SOURCE_DATE_EPOCH)
- SBOM + signed releases (cosign)
- Fuzz harnesses for argv parsing and URL parsing
- `--json` output mode for the tools that don't have one
