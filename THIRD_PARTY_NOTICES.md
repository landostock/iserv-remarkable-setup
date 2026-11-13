# Third-party notices

This project uses third-party software to interact with provider APIs.

The project-specific code is distributed under AGPL-3.0; see LICENSE. Third-party components retain their own license terms and notices. Release downloads include the complete corresponding-source archive for the reMarkable integration alongside the application packages.

## Fly CLI

The setup companion downloads the exact Fly CLI version pinned in `cmd/setup/tools.go` from the official `superfly/flyctl` GitHub release. Each supported OS/architecture archive is accepted only when its SHA-256 digest matches the digest pinned in the application source for that release.

## Node.js and Wrangler

Node.js is downloaded from the official Node.js distribution server and verified against the official `SHASUMS256.txt`. Wrangler is installed into the setup's private Worker runtime at the exact version pinned in `cmd/setup/payload/worker/package.json`, using the committed integrity-bearing npm lockfile and `npm ci`.

## remarquee

The Fly sync image builds `go-go-golems/remarquee` at commit:

`08ab8e048a30cac72f9d7560b0c54aaee87c9f4e`

remarquee is distributed under the MIT License. Its license text and copyright notice must be retained with redistributions.

## rmapi fork

remarquee and the bundled folder helper use `FNStudios-NI/rmapi` at commit:

`f295d5466978954edabaae4c730d50d03740cfac`

The rmapi fork is licensed under GNU AGPL version 3. Release bundles therefore include `Corresponding-Source-remarquee-rmapi.tar.gz`, containing the exact rmapi source, the exact remarquee source used by the Fly image, the local helper source, the provider-endpoint source change, the bounded replacement for rmapi's obsolete JWT parser, upstream license texts and build notes. Release builds also raise remarquee's indirect `google.golang.org/grpc` dependency to at least 1.83.1 to include the fix for GO-2026-6061.

## Provider interfaces and trademarks

The reMarkable cloud interface used here is unofficial/reverse-engineered and can change independently of this project. This project is not affiliated with or endorsed by reMarkable or IServ. Product and service names remain the property of their respective owners.
