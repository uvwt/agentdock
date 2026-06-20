#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: backup-agentdock.sh state|memory|all [commit message]

state  : sync AgentDock publishable workflow state to agentdock-state-backup and push.
memory : commit and push MemoryDock memory repository changes.
all    : run state then memory.

Paths can be overridden with:
  AGENTDOCK_RUNTIME_DIR
  AGENTDOCK_STATE_BACKUP_DIR
  MEMORYDOCK_MEMORY_DIR
USAGE
}

MODE="${1:-}"
MESSAGE="${2:-}"
RUNTIME_DIR="${AGENTDOCK_RUNTIME_DIR:-/Users/xx/agentdock-runtime/AgentDock}"
STATE_REPO="${AGENTDOCK_STATE_BACKUP_DIR:-/Volumes/KIOXIA/Docker/agentdock-state-backup}"
MEMORY_REPO="${MEMORYDOCK_MEMORY_DIR:-/Volumes/KIOXIA/Docker/memorydock/memory}"

if [[ -z "$MODE" || "$MODE" == "-h" || "$MODE" == "--help" ]]; then
  usage
  exit 0
fi

run_git_backup() {
  local repo="$1"
  local msg="$2"
  git -C "$repo" diff --check
  if git -C "$repo" diff --cached --quiet && git -C "$repo" diff --quiet --exit-code; then
    echo "clean: $repo"
    git -C "$repo" status --short --branch
    git -C "$repo" rev-parse --short HEAD
    return 0
  fi

  # 只做保守扫描，避免把明显凭据提交进备份仓库。
  if git -C "$repo" diff --cached -- . ':!*.lock' | grep -E 'BEGIN (RSA|OPENSSH|PRIVATE) KEY|password[[:space:]]*[:=][[:space:]]*[^ <]|secret[[:space:]]*[:=][[:space:]]*[^ <]|token[[:space:]]*[:=][[:space:]]*[^ <]' >/dev/null; then
    echo "sensitive-looking staged diff found in $repo" >&2
    exit 2
  fi
  git -C "$repo" commit -m "$msg"
  git -C "$repo" push origin main
  git -C "$repo" status --short --branch
  git -C "$repo" rev-parse --short HEAD
  git -C "$repo" ls-remote origin refs/heads/main | awk '{print substr($1,1,7)}'
}

backup_state() {
  [[ -d "$RUNTIME_DIR/workflows" ]] || { echo "missing workflows: $RUNTIME_DIR/workflows" >&2; exit 2; }
  [[ -d "$STATE_REPO/.git" ]] || { echo "missing state repo: $STATE_REPO" >&2; exit 2; }
  rsync -a --delete "$RUNTIME_DIR/workflows/" "$STATE_REPO/workflows/"
  git -C "$STATE_REPO" add workflows
  run_git_backup "$STATE_REPO" "${MESSAGE:-backup(state): 同步 AgentDock workflow 状态}"
}

backup_memory() {
  [[ -d "$MEMORY_REPO/.git" ]] || { echo "missing memory repo: $MEMORY_REPO" >&2; exit 2; }
  git -C "$MEMORY_REPO" add -A
  run_git_backup "$MEMORY_REPO" "${MESSAGE:-备份 MemoryDock 记忆}"
}

case "$MODE" in
  state) backup_state ;;
  memory) backup_memory ;;
  all) backup_state; backup_memory ;;
  *) usage; exit 2 ;;
esac
