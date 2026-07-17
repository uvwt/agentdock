#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
EXECUTION_HOME="$HOME"
EXECUTION_PATH="$PATH"

usage() {
  cat <<'USAGE'
Usage: backup-agentdock.sh state|recall|all [commit message]

state  : sync AgentDock publishable workflow state to agentdock-state-backup and push.
recall : commit/push NexusDock Recall data. Uses NexusDock Recall API + temporary clone when the external worktree is not accessible.
all    : run state then recall.

Paths can be overridden with:
  AGENTDOCK_SERVICE_ENV
  AGENTDOCK_STATE_BACKUP_DIR
  RECALLDOCK_RECALL_DIR
  AGENTDOCK_BACKUP_TMP_DIR
  AGENTDOCK_STATE_BACKUP_REMOTE
  AGENTDOCK_RECALL_BACKUP_REMOTE
USAGE
}

MODE="${1:-}"
MESSAGE="${2:-}"
SERVICE_ENV="${AGENTDOCK_SERVICE_ENV:-$HOME/Library/Application Support/AgentDock/agentdock.env}"
STATE_REPO="${AGENTDOCK_STATE_BACKUP_DIR:-/Volumes/KIOXIA/Docker/agentdock-state-backup}"
RECALL_REPO="${RECALLDOCK_RECALL_DIR:-/Volumes/KIOXIA/Docker/nexusdock/recall}"
TMP_ROOT="${AGENTDOCK_BACKUP_TMP_DIR:-$HOME/.agentdock/tmp/backup-recovery}"
STATE_REMOTE="${AGENTDOCK_STATE_BACKUP_REMOTE:-https://github.com/uvwt/agentdock-state-backup.git}"
RECALL_REMOTE="${AGENTDOCK_RECALL_BACKUP_REMOTE:-https://github.com/uvwt/agentdock-recall.git}"
if [[ -f "$SERVICE_ENV" && ! -L "$SERVICE_ENV" ]]; then
  set -a
  # shellcheck disable=SC1090
  source "$SERVICE_ENV"
  set +a
  # 服务配置只提供 AgentDock 参数，不得改变备份进程的用户目录和命令搜索路径。
  export HOME="$EXECUTION_HOME"
  export PATH="$EXECUTION_PATH"
fi

if [[ -z "${AGENTDOCK_MCP_ENDPOINT:-}" ]]; then
  service_host="${AGENTDOCK_HOST:-127.0.0.1}"
  service_port="${AGENTDOCK_PORT:-8765}"
  [[ "$service_port" =~ ^[0-9]+$ ]] && (( service_port >= 1 && service_port <= 65535 )) || {
    echo "invalid AGENTDOCK_PORT in $SERVICE_ENV: $service_port" >&2
    exit 2
  }
  case "$service_host" in
    0.0.0.0|::) service_host="127.0.0.1" ;;
  esac
  if [[ "$service_host" == *:* && "$service_host" != \[*\] ]]; then
    service_host="[$service_host]"
  fi
  AGENTDOCK_MCP_ENDPOINT="http://$service_host:$service_port/mcp"
fi

if [[ -z "$MODE" || "$MODE" == "-h" || "$MODE" == "--help" ]]; then
  usage
  exit 0
fi

quick_git_ok() {
  local repo="$1"
  [[ -d "$repo/.git" ]] || return 1
  python3 - "$repo" <<'PY'
import subprocess, sys
repo = sys.argv[1]
try:
    subprocess.run(['git', '-C', repo, 'rev-parse', '--is-inside-work-tree'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=8, check=True)
    subprocess.run(['git', '-C', repo, 'status', '--short', '--branch', '-uno'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=8, check=True)
    # macOS launchd 可能允许只读访问外置 worktree，却在 Git 进入目录并准备 index 时
    # 因 TCC 无法解析 cwd。提前执行无副作用的 add --dry-run，失败就改用临时 clone。
    subprocess.run(['git', '-C', repo, 'add', '--dry-run', '--all'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=8, check=True)
except Exception:
    raise SystemExit(1)
PY
}

worktree_is_usable() {
  local repo="$1"
  # macOS LaunchAgent 对外置卷可能出现“绝对路径读写正常，但 Git 进入目录后
  # getcwd 被 TCC 拒绝”的状态。定时任务对此不做冒险，统一使用用户目录临时 clone。
  if [[ "$(uname -s)" == "Darwin" && -n "${XPC_SERVICE_NAME:-}" && "$repo" == /Volumes/* ]]; then
    return 1
  fi
  quick_git_ok "$repo"
}

guard_no_zero_markdown() {
  local repo="$1"
  [[ -d "$repo/.git" ]] || return 0
  local zero_file
  zero_file="$(mktemp)"
  git -C "$repo" ls-files '*.md' | while IFS= read -r rel; do
    if [[ -f "$repo/$rel" && ! -s "$repo/$rel" ]]; then
      printf '%s\n' "$rel"
    fi
  done >"$zero_file"
  if [[ -s "$zero_file" ]]; then
    echo "refusing to commit zero-byte tracked markdown files in $repo:" >&2
    sed -n '1,120p' "$zero_file" >&2
    rm -f "$zero_file"
    exit 3
  fi
  rm -f "$zero_file"
}

run_git_backup() {
  local repo="$1"
  local msg="$2"
  git -C "$repo" diff --check
  guard_no_zero_markdown "$repo"
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
  guard_no_zero_markdown "$repo"
  git -C "$repo" commit -m "$msg"
  git -C "$repo" push origin main
  git -C "$repo" status --short --branch
  git -C "$repo" rev-parse --short HEAD
  git -C "$repo" ls-remote origin refs/heads/main | awk '{print substr($1,1,7)}'
}

with_temp_clone() {
  local remote="$1"
  local name="$2"
  local target="$TMP_ROOT/$name"
  rm -rf "$target"
  mkdir -p "$TMP_ROOT"
  git clone --depth 1 "$remote" "$target" >/dev/null
  printf '%s\n' "$target"
}

export_workflows_from_runtime_api() {
  local repo="$1"
  python3 - "$repo" "$AGENTDOCK_MCP_ENDPOINT" <<'PY'
import json, os, pathlib, shutil, sys, urllib.request

repo = pathlib.Path(sys.argv[1])
endpoint = sys.argv[2]
token = os.environ.get('AGENTDOCK_AUTH_TOKEN', '')

def call_tool(name, args, req_id):
    payload = {'jsonrpc': '2.0', 'id': req_id, 'method': 'tools/call', 'params': {'name': name, 'arguments': args}}
    request = urllib.request.Request(endpoint, data=json.dumps(payload).encode(), headers={'Content-Type': 'application/json'})
    if token:
        request.add_header('Authorization', 'Bearer ' + token)
    with urllib.request.urlopen(request, timeout=60) as response:
        outer = json.loads(response.read())
    if 'error' in outer:
        raise RuntimeError(outer['error'])
    text = outer['result']['content'][0]['text']
    return json.loads(text)

items = []
seen = set()
req_id = 1
for status in ('draft', 'active', 'retired'):
    listed = call_tool('workflow_template_manage', {'action': 'list', 'template_status': status}, req_id)
    req_id += 1
    for item in listed.get('items') or []:
        template_id = str(item.get('id') or '').strip()
        version = str(item.get('version') or '').strip()
        if not template_id or not version:
            continue
        key = (template_id, version)
        if key in seen:
            continue
        seen.add(key)
        items.append((template_id, version, status))

if not items:
    raise RuntimeError('workflow_template_manage returned no workflow templates')

target = repo / 'workflows'
tmp = repo / '.workflows-api-export.tmp'
if tmp.exists():
    shutil.rmtree(tmp)
tmp.mkdir(parents=True)

for template_id, version, listed_status in items:
    detail = call_tool('workflow_template_manage', {'action': 'get', 'template_id': template_id, 'template_version': version}, req_id)
    req_id += 1
    template = detail.get('template') or {}
    status = str(template.get('status') or listed_status)
    location = 'drafts' if status in ('draft', 'validated') else 'published'
    file_name = f'{template_id}@{version}.json'
    content = json.dumps(template, ensure_ascii=False, indent=2) + '\n'
    out = tmp / location / file_name
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(content, encoding='utf-8')

vector_index = call_tool('workflow_template_manage', {'action': 'vector_index'}, req_id)
if vector_index.get('available') and isinstance(vector_index.get('content'), str) and vector_index['content'].strip():
    (tmp / 'vector-index.json').write_text(vector_index['content'] if vector_index['content'].endswith('\n') else vector_index['content'] + '\n', encoding='utf-8')
    print(f'workflow vector index exported: {vector_index.get("vector_index_items", 0)} items')
else:
    print(f'workflow vector index skipped: {vector_index.get("vector_index_status", "unavailable")}')

if target.exists():
    shutil.rmtree(target)
tmp.rename(target)
print(f'workflow source: AgentDock workflow_template_manage API {endpoint}')
print(f'workflow templates exported: {len(items)}')
PY
}

backup_state_worktree() {
  local repo="$1"
  export_workflows_from_runtime_api "$repo"
  git -C "$repo" add workflows
  run_git_backup "$repo" "${MESSAGE:-backup(state): 同步 AgentDock workflow 状态}"
}

backup_state() {
  if worktree_is_usable "$STATE_REPO"; then
    backup_state_worktree "$STATE_REPO"
  else
    echo "state backup worktree unavailable, using temporary clone: $STATE_REPO" >&2
    local repo
    repo="$(with_temp_clone "$STATE_REMOTE" agentdock-state-backup)"
    backup_state_worktree "$repo"
    rm -rf "$repo"
  fi
}

export_recall_to_repo() {
  local repo="$1"
  python3 "$SCRIPT_DIR/recall_backup_export.py" \
    --repo "$repo" \
    --endpoint "$AGENTDOCK_MCP_ENDPOINT"
}

backup_recall_worktree() {
  local repo="$1"
  # 正常可访问真实 NexusDock Recall Git worktree 时，不通过 API 重新导出覆盖仓库；
  # 只提交当前 worktree 里的真实文件，避免 API/挂载异常时把空正文写回 Git。
  guard_no_zero_markdown "$repo"
  git -C "$repo" add -A
  run_git_backup "$repo" "${MESSAGE:-备份 NexusDock Recall 数据}"
}

backup_recall_export_clone() {
  local repo="$1"
  export_recall_to_repo "$repo"
  guard_no_zero_markdown "$repo"
  git -C "$repo" add -A
  run_git_backup "$repo" "${MESSAGE:-备份 NexusDock Recall 数据}"
}

backup_recall() {
  if worktree_is_usable "$RECALL_REPO"; then
    backup_recall_worktree "$RECALL_REPO"
  else
    echo "recall backup worktree unavailable, using NexusDock Recall API + temporary clone: $RECALL_REPO" >&2
    local repo
    repo="$(with_temp_clone "$RECALL_REMOTE" agentdock-recall)"
    backup_recall_export_clone "$repo"
    rm -rf "$repo"
  fi
}

case "$MODE" in
  state) backup_state ;;
  recall) backup_recall ;;
  all) backup_state; backup_recall ;;
  *) usage; exit 2 ;;
esac
