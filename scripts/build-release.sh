#!/usr/bin/env bash
set -euo pipefail

version="${1:?Usage: bash scripts/build-release.sh v0.1.0}"
if [[ ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo 'Expected a release version such as v0.1.0 (no prerelease suffix).' >&2
  exit 1
fi

go_command="${GO:-go}"
output_dir="dist/$version"
mkdir -p "$output_dir"
build_dir="$(mktemp -d)"
trap 'rm -rf "$build_dir"' EXIT
archives=()

for arch in amd64 arm64; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" "$go_command" build \
    -trimpath -ldflags "-s -w -X main.version=$version" \
    -o "$build_dir/mini-coder" ./cmd/server
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" "$go_command" build \
    -trimpath -ldflags "-s -w" \
    -o "$build_dir/mini-coder-migrate" ./cmd/migrate
  archive="mini-coder_${version}_linux_${arch}.tar.gz"
  tar -czf "$output_dir/$archive" -C "$build_dir" mini-coder mini-coder-migrate
  archives+=("$archive")
done

(
  cd "$output_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${archives[@]}" > checksums.txt
  else
    shasum -a 256 "${archives[@]}" > checksums.txt
  fi
)
printf 'Release files: %s\n' "$output_dir"
