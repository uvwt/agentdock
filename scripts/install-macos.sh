#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
SRC_DIR="${SCRIPT_DIR:h}"
RUNTIME_DIR="${SRC_DIR:h}/agentdock-runtime"
ALLOW_ENV="${AGENTDOCK_INSTALL_ALLOW_ENV_PATHS:-false}"

if [[ "$ALLOW_ENV" == "true" ]]; then
  if [[ -n "${AGENTDOCK_SRC:-}" && -d "$AGENTDOCK_SRC/.git" ]]; then
    SRC_DIR="$AGENTDOCK_SRC"
  fi
  if [[ -n "${AGENTDOCK_RUNTIME_DIR:-}" && -f "$AGENTDOCK_RUNTIME_DIR/start-agentdock.sh" ]]; then
    RUNTIME_DIR="$AGENTDOCK_RUNTIME_DIR"
  fi
fi

TARGET="$SRC_DIR/agentdock"
BACKUP_DIR="$RUNTIME_DIR/backups/agentdock"
if [[ "$ALLOW_ENV" == "true" && -n "${AGENTDOCK_TARGET:-}" ]]; then
  TARGET="$AGENTDOCK_TARGET"
fi
if [[ "$ALLOW_ENV" == "true" && -n "${AGENTDOCK_BACKUP_DIR:-}" ]]; then
  case "$AGENTDOCK_BACKUP_DIR" in
    "$RUNTIME_DIR"/*) BACKUP_DIR="$AGENTDOCK_BACKUP_DIR" ;;
  esac
fi

STAMP="$(date +%Y%m%d%H%M%S)"
TMP_BIN="$TARGET.new.$STAMP"

cd "$SRC_DIR"

printf '==> source: %s\n' "$SRC_DIR"
printf '==> target: %s\n' "$TARGET"
printf '==> backup_dir: %s\n' "$BACKUP_DIR"

printf '==> running gofmt check\n'
test -z "$(gofmt -l ./cmd ./internal)"

printf '==> running tests\n'
go test ./...

printf '==> running go vet\n'
go vet ./...

printf '==> building %s\n' "$TMP_BIN"
go build -trimpath -o "$TMP_BIN" ./cmd/agentdock

if command -v codesign >/dev/null 2>&1; then
  printf '==> ad-hoc codesigning\n'
  codesign --force --sign - "$TMP_BIN" >/dev/null
fi

mkdir -p "$BACKUP_DIR"
if [[ -f "$TARGET" ]]; then
  cp -p "$TARGET" "$BACKUP_DIR/agentdock.$STAMP"
fi

chmod +x "$TMP_BIN"
mv "$TMP_BIN" "$TARGET"

printf 'installed: %s\n' "$TARGET"
printf 'backup_dir: %s\n' "$BACKUP_DIR"
printf 'restart with: make restart-macos\n'
