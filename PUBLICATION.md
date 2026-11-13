# Public source and release provenance

This repository starts with a clean source snapshot for RC27. Earlier private development history, account data, installation state, local logs and diagnostic reports are intentionally not imported.

Publication review checks the allowlisted source tree, the new public Git history and the release assets. The public history uses a project contributor identity with a non-personal noreply address. Test cases use synthetic accounts and deployment identifiers.

The scanner used for the initial review is Gitleaks 8.30.1, downloaded from its upstream GitHub release and verified against the upstream SHA-256 manifest. Its source findings were reviewed as synthetic test credentials and pinned upstream commit hashes, not operational secrets. A separate check covers known personal identifiers and local paths. These checks cannot prove the absence of every possible disclosure.

RC27 application behavior and embedded Cloudflare/Fly payload are unchanged from the tested RC27 implementation. Public preparation adds project licensing, general documentation, updated repository links, synthetic test identifiers, private-file exclusions and license inclusion in packaging. The private self-hosted runner configuration is omitted. Original Go module identifiers are retained; a private repository checkout is not required to build this snapshot.

Release assets are built locally from this public snapshot, not from personal application-support folders. GitHub Actions are initially disabled; no hosted build minutes are required to download or use the published files. Signing and native platform test limits are documented in the README and RC_VERIFICATION.md.
