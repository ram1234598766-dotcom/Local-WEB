# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| main    | Yes       |
| 0.x.x   | No        |

## Key Storage

Node identity keys are stored at:
- Linux/macOS: `~/.localweb/identity.json` (mode 0600)
- Windows: `%USERPROFILE%\.localweb\identity.json` (mode 0600)

**The private key is never printed to stdout or logged.** If you see a private
key in logs, report it immediately — this is a security incident.

## Reporting a Vulnerability

Please report security vulnerabilities responsibly:

1. **Do not** open a public GitHub issue for security bugs.
2. Email: `security@localweb.p2p` (or open a private security advisory on GitHub).
3. Include:
   - A description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Suggested fix (if any)

### Critical Issues (Immediate Response)

Contact immediately for issues involving:
- Private key exposure
- Identity forgery / key regeneration bugs
- DTLS/Noise handshake bypasses
- Store encryption bypass
- Remote code execution via transport layer

### Response Timeline

| Severity | Acknowledgment | Patch |
|----------|---------------|-------|
| Critical | 2 hours       | 24 hours |
| High     | 24 hours      | 7 days   |
| Medium   | 48 hours      | 30 days  |
| Low      | 72 hours      | Next release |

## Security Architecture

- **Transport**: QUIC with Noise XX handshake (X25519 + SHA3-256)
- **Identity**: Ed25519 keypair, persistent on disk (0600 permissions)
- **Store encryption**: AES-256-GCM via BadgerDB, key derived from node identity
- **Access control**: Ed25519-signed capability tokens with canonical JSON
- **Spam resistance**: SHA3-based Proof of Work
- **Audit trail**: Append-only SHA3-256 hash chain

## Running with CGO

Race detector (`-race`) and some platform backends require CGO. On Windows,
install MSYS2 (mingw64) and ensure `gcc` is on PATH.

## Disclosure Policy

Upon acceptance of a vulnerability report, we will:
1. Acknowledge receipt within the timeline above
2. Investigate and reproduce
3. Develop a fix in a private branch
4. Coordinate release with the reporter
5. Publish CVE after patch is available
