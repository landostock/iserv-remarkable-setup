# Security

IServ → reMarkable is a pre-1.0 project that provisions resources in accounts owned by the person running the setup companion. Security-sensitive behavior is therefore treated conservatively and failures should stop rather than silently weaken protection.

## Security model

- The setup companion listens on the loopback interface only, validates the exact local Host/Origin context for unsafe requests, rejects non-loopback peers, and uses a random per-session setup token.
- IServ passwords and reMarkable authorization tokens are not written to `installation.json`.
- Cloud credentials are sent to the user's own Fly.io/Cloudflare resources as provider-managed secrets.
- The Fly dashboard-control credential is app-scoped and created with a one-year lifetime. Maintenance warns when fewer than six months remain, and a deliberate cloud update rotates the credential and updates its stored non-secret expiry metadata.
- The dashboard uses HTTPS, a long administrator password, signed short-lived sessions, same-origin checks, and rate limiting.
- Automatic synchronization remains disabled after provisioning until a no-upload verification has succeeded and the user deliberately enables it.
- Downloaded managed tools and installer updates use HTTPS, reject insecure redirect downgrades, enforce size limits, and are verified with SHA-256 before use.
- Tool archives reject path traversal and do not materialize archive links.
- Recovery returns deployment identifiers only; it does not return IServ or reMarkable credentials.
- Cloud teardown is an explicit maintenance action and is separate from ordinary local application removal.
- reMarkable metadata scans run with a private temporary HOME/cache/config tree. Cleanup is checked and setup fails closed if that tree cannot be removed.
- The pinned rmapi token reader is replaced at build time by a 64 KiB-bounded standard-library parser; the obsolete reachable JWT parser and dependency are excluded from release builds.
- Fly image builder/runtime bases are pinned by official multi-architecture content digest as well as human-readable version tag.
- Native release binaries and the Fly builder use Go 1.26.11 or newer within the 1.26 line. RC23 raises the floor for the November 2026 standard-library advisories.
- Permanent CI scans the setup and helper with pinned `govulncheck`; the RC source gate additionally builds and scans the exact patched rmapi/remarquee path used by the Fly image.

The reMarkable cloud API used by this project is unofficial. Provider behavior can change without notice, so a successful security review cannot guarantee future compatibility with third-party services.
Dependency scans describe the vulnerability database available when they run; newly disclosed issues still require a new review and release.

## Reporting a vulnerability

Do not post passwords, tokens, private URLs, document names, school information, diagnostic files, or exploit details in a public issue.

If GitHub private vulnerability reporting is enabled for this repository, use that channel. Otherwise, open a minimal issue stating that you need a private security contact, without including the sensitive details. A private reporting route should be configured before the repository is made public.

## Diagnostics

The built-in diagnostic export performs best-effort redaction of common credentials, URLs, email addresses, and the local home-directory path. Redaction is deliberately described as best-effort rather than a guarantee. Review a diagnostic file before sharing it.

## Release integrity

Release-candidate packages may be unsigned while the project is pre-1.0 and under controlled testing. Public v1+ Windows and macOS releases must not be published as trusted releases unless publisher signing is configured; macOS additionally requires notarization. Signing credentials must remain in protected release secrets and must never be committed to the repository or pasted into issue/chat logs.
