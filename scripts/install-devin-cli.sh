#!/bin/sh
# Install the official CLI at image-build time, never from an inference request.
# Pins copied from https://static.devin.ai/cli/3000.10.31/manifest.json.
set -eu
arch=${1:?usage: install-devin-cli.sh amd64|arm64 destination}
dest=${2:?destination is required}
version=3000.10.31
case "$arch" in
  amd64) target=x86_64-unknown-linux; checksum=43218d80ee49576f4f84a1ffd4a4cec755c545b5ca98c5b26efce5340824f331 ;;
  arm64) target=aarch64-unknown-linux; checksum=6da96b9c8c2337892c0dad0a12c7aaa7e568bfef7572bbf54e7a0ec88916ba33 ;;
  *) echo 'unsupported Devin CLI architecture' >&2; exit 1 ;;
esac
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT HUP INT TERM
curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
  --connect-timeout 20 --max-time 300 --retry 2 \
  "https://static.devin.ai/cli/$version/devin-$version-$target.tar.gz" -o "$work/bundle.tar.gz"
printf '%s  %s\n' "$checksum" "$work/bundle.tar.gz" | sha256sum -c -
tar xzf "$work/bundle.tar.gz" -C "$work" bin/devin
test -f "$work/bin/devin" && test ! -L "$work/bin/devin"
mkdir -p "$dest"
cp "$work/bin/devin" "$dest/devin"
chmod 755 "$dest/devin"
