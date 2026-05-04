# Future features

A running list of features and tools we may want to add later. Items are
roughly grouped, not prioritized. Open an issue to bump something up.

## More tools to add to the bag

- `cat`, `head`, `tail`, `tee`
- `grep` (PCRE-free; RE2)
- `sed` (subset)
- `xxd` / `hexdump`
- `base64`
- `tr`
- `cut`, `sort`, `uniq`, `wc`
- `find`
- `nc` / `netcat` (HTTP/TCP only)
- `dig` (basic A/AAAA/MX/TXT)
- `ssh-keygen` subset (Ed25519 only)
- `openssl` subset (`s_client`, `x509 -text`, `dgst`)
- `tar` (read-only first?), `gzip`, `xz`, `zstd`
- `jq` subset
- `ping` (raw socket — needs cap_net_raw)
- `traceroute` (UDP only)
- `whois`
- `time` (the timing one, not the clock)
- `env`, `which`, `printenv`

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

## Cross-cutting

- Shell completions (bash/zsh/fish)
- `bag --list` to enumerate available tools
- `bag --install /usr/local/bin` to symlink everything
- A `bag doctor` that diffs behavior against the real tool on the host
- Reproducible builds (`-trimpath`, SOURCE_DATE_EPOCH)
- SBOM + signed releases (cosign)
- Fuzz harnesses for argv parsing and URL parsing
- `--json` output mode for the tools that don't have one
