#!/usr/bin/env bash
# Cross-compile the baton binary for every supported OS/arch into dist/.
# Asset names match what install.sh downloads: baton_<os>_<arch>.
#   ./scripts/build-release.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$REPO/dist"
mkdir -p "$OUT"

targets=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
)

for t in "${targets[@]}"; do
  read -r os arch <<<"$t"
  out="$OUT/baton_${os}_${arch}"
  echo "  • $out"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -ldflags="-s -w" -o "$out" "$REPO/cmd/baton"
done

echo "Done. Binaries in $OUT/"
