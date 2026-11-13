#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 OUTPUT.tar.gz" >&2
  exit 2
fi

OUTPUT="$1"
case "$OUTPUT" in
  /*) ;;
  *) OUTPUT="$(pwd -P)/$OUTPUT" ;;
esac
mkdir -p "$(dirname "$OUTPUT")"
OUTPUT="$(cd "$(dirname "$OUTPUT")" && pwd -P)/$(basename "$OUTPUT")"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
BUNDLE="$WORK/Corresponding-Source-remarquee-rmapi"
mkdir -p "$BUNDLE"

REMARQUEE_COMMIT="08ab8e048a30cac72f9d7560b0c54aaee87c9f4e"
RMAPI_COMMIT="f295d5466978954edabaae4c730d50d03740cfac"

git clone --filter=blob:none https://github.com/go-go-golems/remarquee.git "$BUNDLE/remarquee"
git -C "$BUNDLE/remarquee" checkout "$REMARQUEE_COMMIT"
git clone --filter=blob:none https://github.com/FNStudios-NI/rmapi.git "$BUNDLE/rmapi"
git -C "$BUNDLE/rmapi" checkout "$RMAPI_COMMIT"

# Include the exact provider endpoint and bounded token parser used by the
# production image instead of requiring recipients to reconstruct hidden patches.
python3 - "$BUNDLE/rmapi/auth/auth.go" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
s = p.read_text(encoding="utf-8")
old = "https://my.remarkable.com/token/json/2/"
new = "https://webapp-prod.cloud.remarkable.engineering/token/json/2/"
if old not in s and new not in s:
    raise SystemExit("rmapi auth endpoint anchor not found")
p.write_text(s.replace(old, new), encoding="utf-8")
PY
cp "$ROOT/cmd/setup/payload/rmapi_safe_api.go.txt" "$BUNDLE/rmapi/api/api.go"
cp "$ROOT/cmd/setup/payload/rmapi_safe_api_test.go.txt" "$BUNDLE/rmapi/api/api_security_test.go"
(
  cd "$BUNDLE/rmapi"
  go mod edit -droprequire=github.com/golang-jwt/jwt
)
(
  cd "$BUNDLE/remarquee"
  go mod edit -replace=github.com/juruen/rmapi=../rmapi
  go get google.golang.org/grpc@v1.83.1 golang.org/x/net@v0.60.0
  go mod tidy
)

rm -rf "$BUNDLE/remarquee/.git" "$BUNDLE/rmapi/.git"
mkdir -p "$BUNDLE/helper-rmfolder"
cp "$ROOT/helper/rmfolder/main.go" "$ROOT/helper/rmfolder/go.mod" "$ROOT/helper/rmfolder/go.sum" "$BUNDLE/helper-rmfolder/"
cp "$ROOT/cmd/setup/payload/Dockerfile" "$BUNDLE/Dockerfile"
cp "$ROOT/cmd/setup/payload/rmapi_safe_api.go.txt" "$BUNDLE/rmapi_safe_api.go.txt"
cp "$ROOT/cmd/setup/payload/rmapi_safe_api_test.go.txt" "$BUNDLE/rmapi_safe_api_test.go.txt"
cp "$ROOT/THIRD_PARTY_NOTICES.md" "$BUNDLE/THIRD_PARTY_NOTICES.md"

cat > "$BUNDLE/BUILDING.md" <<EOF
# Corresponding source build notes

This archive accompanies an IServ → reMarkable release and contains the exact
source revisions used for the bundled reMarkable integration.

- remarquee: ${REMARQUEE_COMMIT}
- Go security floor: 1.26.11
- gRPC security floor: v1.83.1
- x/net security floor: v0.60.0
- rmapi fork: ${RMAPI_COMMIT}
- local folder helper: helper-rmfolder/
  The helper includes go.mod and go.sum so its module graph is auditable.

The production Fly image is described by Dockerfile. The included remarquee
module already replaces github.com/juruen/rmapi with the included rmapi tree.
The included rmapi tree already contains the provider-endpoint source change
and bounded standard-library token parser installed by that Dockerfile.

To build the standalone folder helper from this archive:

    cd helper-rmfolder
    go mod edit -replace=github.com/juruen/rmapi=../rmapi
    go mod tidy
    go build .

Network access may be needed to obtain other Go modules referenced by upstream
source. Their versions and cryptographic module checksums are recorded by the
upstream go.mod/go.sum files. No private project credentials are required.
EOF

# Preserve upstream license texts and source while avoiding non-source VCS data.
if tar --version 2>/dev/null | grep -qi 'gnu tar'; then
  tar --sort=name --mtime='UTC 2026-08-21' --owner=0 --group=0 --numeric-owner \
    -C "$WORK" -czf "$OUTPUT" Corresponding-Source-remarquee-rmapi
else
  tar -C "$WORK" -czf "$OUTPUT" Corresponding-Source-remarquee-rmapi
fi

test -s "$OUTPUT"
