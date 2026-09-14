#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$ROOT"

macos=false
case "${1:-}" in
  --macos) macos=true ;;
  "") ;;
  *)
    printf 'usage: %s [--macos]\n' "$0" >&2
    exit 2
    ;;
esac

syntax() {
  checker=$1
  script=$2
  command -v "$checker" >/dev/null 2>&1 || return 0
  "$checker" -n "$script"
}

if [ "$macos" = true ]; then
  [ "$(uname -s)" = Darwin ] || {
    printf 'macOS script checks require Darwin\n' >&2
    exit 1
  }
  while IFS= read -r rel; do
    [ -n "$rel" ] || continue
    [ -f "$rel" ] || continue
    case "$(sed -n '1p' "$rel")" in
      *zsh*) syntax zsh "$rel" ;;
    esac
  done <<EOF
$(git ls-files '*.sh')
EOF
  ./scripts/test/test-install-macos.sh
  ./scripts/test/test-macos-app.sh
  exit 0
fi

run_python() {
  for candidate in python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 &&
      PYTHONDONTWRITEBYTECODE=1 "$candidate" -c 'import sys' >/dev/null 2>&1; then
      PYTHONDONTWRITEBYTECODE=1 "$candidate" "$@"
      return $?
    fi
  done
  printf 'python3 or python is required\n' >&2
  return 1
}

run_python ./scripts/test/test_shell_variable_boundaries.py

has_shellcheck=false
if command -v shellcheck >/dev/null 2>&1; then
  has_shellcheck=true
fi

while IFS= read -r rel; do
  [ -n "$rel" ] || continue
  [ -f "$rel" ] || continue
  case "$(sed -n '1p' "$rel")" in
    *zsh*) continue ;;
    *bash*) syntax bash "$rel" ;;
    *)
      syntax sh "$rel"
      syntax dash "$rel"
      ;;
  esac
  if [ "$has_shellcheck" = true ]; then
    shellcheck "$rel"
  fi
done <<EOF
$(git ls-files '*.sh')
EOF

./scripts/test/test-install-entry.sh
