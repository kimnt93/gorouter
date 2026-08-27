#!/usr/bin/env bash
set -euo pipefail

mode="${1:-check}"
repo_root=$(git rev-parse --show-toplevel)
generated_dir="$repo_root/internal/docs"

generate_into() {
  local output_dir="$1"
  mkdir -p "$output_dir"
  (
    cd "$repo_root"
    go run github.com/swaggo/swag/cmd/swag@v1.16.6 init \
      -g cmd/gorouter/main.go \
      -o "$output_dir" \
      --parseDependency \
      --parseInternal
    gofmt -w "$output_dir/docs.go"
  )
}

case "$mode" in
  generate)
    generate_into "$generated_dir"
    ;;
  check)
    check_root=$(mktemp -d /tmp/gorouter-swaggo-check.XXXXXX)
    check_dir="$check_root/docs"
    trap 'rm -r -- "$check_root"' EXIT
    generate_into "$check_dir"
    for name in docs.go swagger.json swagger.yaml; do
      diff -u "$generated_dir/$name" "$check_dir/$name"
    done
    ;;
  *)
    echo "usage: $0 [check|generate]" >&2
    exit 2
    ;;
esac
