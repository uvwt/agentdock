#!/usr/bin/env bash
# goal-demo-harness.sh — operator harness for AgentDock Goal Mode demos
#
# Default: create + run micro fixture. Optionally --medium for 3-batch demo.
# Does NOT hard-reset Chrome by default (use --hard-reset-chrome only if you
# intentionally want a full Chromium kill/restart outside this script's scope).
#
# Usage:
#   ./scripts/goal-demo-harness.sh
#   ./scripts/goal-demo-harness.sh --timeout-min 30 --auto-approve --run-index 2
#   ./scripts/goal-demo-harness.sh --medium --auto-approve --timeout-min 60
#   ./scripts/goal-demo-harness.sh --create-only --run-index 1
#
# Env:
#   AGENTDOCK_BASE_URL   default http://127.0.0.1:8765
#   AGENTDOCK_AUTH_TOKEN optional bearer token when auth is required
#   MICRO_RUN_INDEX / MEDIUM_RUN_INDEX  alternative to --run-index
#
# Exit: 0 on completed, non-zero on blocked/error/timeout/setup failure.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASE_URL="${AGENTDOCK_BASE_URL:-http://127.0.0.1:8765}"
TIMEOUT_MIN=30
AUTO_APPROVE=0
RUN_INDEX=""
MEDIUM=0
CREATE_ONLY=0
HARD_RESET_CHROME=0
POLL_SEC=15

usage() {
  cat <<'EOF'
goal-demo-harness.sh — AgentDock Goal Mode demo operator harness

Options:
  --timeout-min N     poll timeout in minutes (default 30)
  --auto-approve      open worker + enable auto_approve_tools via API
  --run-index N       isolated workspace index (agentdock-*-goal-N)
  --medium            use demo-create-goal-medium instead of micro
  --create-only       create goal and print goal_id; do not orchestrate
  --hard-reset-chrome document-only flag: this harness NEVER kills Chrome;
                      print a reminder that hard-reset is operator-owned
  --poll-sec N        status poll interval seconds (default 15)
  -h, --help          show this help

Examples:
  ./scripts/goal-demo-harness.sh --create-only
  ./scripts/goal-demo-harness.sh --auto-approve --timeout-min 30 --run-index 1
  ./scripts/goal-demo-harness.sh --medium --auto-approve --timeout-min 90

Notes:
  - Health check: GET /internal/runtime/chatgpt/worker
  - Does NOT hard-reset Chrome by default. If Chrome is wedged, reset it
    yourself (or pass --hard-reset-chrome only as a reminder — the script
    still will not kill browser processes).
  - Requires a running agentdock on AGENTDOCK_BASE_URL (default :8765).
EOF
}

log() { printf '[harness] %s\n' "$*"; }
err() { printf '[harness] ERROR: %s\n' "$*" >&2; }

api() {
  # api METHOD PATH [JSON_BODY]
  local method="$1" path="$2" body="${3-}"
  local url="${BASE_URL}${path}"
  local -a args=(-sS -X "$method" "$url" -H 'Content-Type: application/json')
  if [[ -n "${AGENTDOCK_AUTH_TOKEN:-}" ]]; then
    args+=(-H "Authorization: Bearer ${AGENTDOCK_AUTH_TOKEN}")
  fi
  if [[ -n "$body" ]]; then
    args+=(-d "$body")
  fi
  curl "${args[@]}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --timeout-min)
      TIMEOUT_MIN="${2:?}"
      shift 2
      ;;
    --auto-approve)
      AUTO_APPROVE=1
      shift
      ;;
    --run-index)
      RUN_INDEX="${2:?}"
      shift 2
      ;;
    --medium)
      MEDIUM=1
      shift
      ;;
    --create-only)
      CREATE_ONLY=1
      shift
      ;;
    --hard-reset-chrome)
      HARD_RESET_CHROME=1
      shift
      ;;
    --poll-sec)
      POLL_SEC="${2:?}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      err "unknown arg: $1"
      usage >&2
      exit 2
      ;;
  esac
done

if [[ "$HARD_RESET_CHROME" -eq 1 ]]; then
  log "NOTE: --hard-reset-chrome is documented only; this harness will NOT kill Chrome."
  log "If you need a hard reset, stop agentdock's browser profile yourself, then re-run."
fi

# --- health ---
log "health check: GET ${BASE_URL}/internal/runtime/chatgpt/worker"
if ! health_json="$(api GET /internal/runtime/chatgpt/worker)"; then
  err "agentdock not reachable at ${BASE_URL}"
  exit 1
fi
if [[ -z "$health_json" ]]; then
  err "empty health response from worker endpoint"
  exit 1
fi
log "worker ok: $(printf '%s' "$health_json" | head -c 200)"

# --- create goal ---
cd "$ROOT"
create_args=()
export MICRO_RUN_INDEX="${RUN_INDEX:-${MICRO_RUN_INDEX:-}}"
export MEDIUM_RUN_INDEX="${RUN_INDEX:-${MEDIUM_RUN_INDEX:-}}"
if [[ -n "${RUN_INDEX}" ]]; then
  create_args+=(-n "$RUN_INDEX")
fi

if [[ "$MEDIUM" -eq 1 ]]; then
  demo_cmd=(go run ./cmd/demo-create-goal-medium "${create_args[@]+"${create_args[@]}"}")
  fixture="medium"
else
  demo_cmd=(go run ./cmd/demo-create-goal-micro "${create_args[@]+"${create_args[@]}"}")
  fixture="micro"
fi

log "creating ${fixture} goal: ${demo_cmd[*]}"
create_out="$("${demo_cmd[@]}")"
printf '%s\n' "$create_out"

goal_id="$(printf '%s\n' "$create_out" | sed -n 's/^goal_id=//p' | head -n1)"
if [[ -z "$goal_id" ]]; then
  # fallback: first token that looks like goal_*
  goal_id="$(printf '%s\n' "$create_out" | grep -Eo 'goal_[0-9a-fA-F]+' | head -n1 || true)"
fi
if [[ -z "$goal_id" ]]; then
  err "could not parse goal_id from create output"
  exit 1
fi
log "parsed goal_id=${goal_id}"

if [[ "$CREATE_ONLY" -eq 1 ]]; then
  log "create-only: done"
  printf 'goal_id=%s\n' "$goal_id"
  exit 0
fi

# --- optional open + auto_approve ---
if [[ "$AUTO_APPROVE" -eq 1 ]]; then
  log "opening ChatGPT worker"
  api POST /internal/runtime/chatgpt/worker '{"action":"open"}' >/dev/null || true
  log "enabling auto_approve_tools (+ auto_wake)"
  api POST /internal/runtime/chatgpt/worker \
    '{"auto_approve_tools":true,"auto_wake":true}' >/dev/null || {
    err "failed to set auto_approve_tools"
    exit 1
  }
fi

# --- orchestrate ---
log "orchestrate_start ${goal_id}"
start_json="$(api POST "/internal/runtime/goals/${goal_id}/orchestrate_start" '{}')"
printf '%s\n' "$start_json" | head -c 500
echo

deadline=$(( $(date +%s) + TIMEOUT_MIN * 60 ))
phase=""
last_dump=""

json_field() {
  # tiny portable extractor: first "key":"value" or "key":value
  local key="$1" blob="$2"
  printf '%s' "$blob" | python3 -c '
import json,sys
key=sys.argv[1]
raw=sys.stdin.read()
try:
    data=json.loads(raw)
except Exception:
    print("")
    raise SystemExit(0)
def walk(o, k):
    if isinstance(o, dict):
        if k in o:
            return o[k]
        for v in o.values():
            r=walk(v,k)
            if r is not None:
                return r
    elif isinstance(o, list):
        for v in o:
            r=walk(v,k)
            if r is not None:
                return r
    return None
v=walk(data, key)
if v is None:
    print("")
elif isinstance(v, (dict, list)):
    print(json.dumps(v, ensure_ascii=False))
else:
    print(v)
' "$key" 2>/dev/null || true
}

while true; do
  now=$(date +%s)
  if (( now >= deadline )); then
    err "timeout after ${TIMEOUT_MIN}m (last phase=${phase:-unknown})"
    if [[ -n "$last_dump" ]]; then
      printf '%s\n' "$last_dump" | head -c 2000
      echo
    fi
    exit 124
  fi

  status_json="$(api POST "/internal/runtime/goals/${goal_id}/orchestrate_status" '{}')" || {
    err "orchestrate_status request failed"
    sleep "$POLL_SEC"
    continue
  }
  last_dump="$status_json"
  phase="$(json_field phase "$status_json")"
  # also check nested goal status if present
  goal_status="$(json_field status "$status_json")"
  last_msg="$(json_field LastMessage "$status_json")"
  if [[ -z "$last_msg" ]]; then
    last_msg="$(json_field last_message "$status_json")"
  fi
  log "poll phase=${phase:-?} goal_status=${goal_status:-?} msg=${last_msg:-}"

  case "${phase}" in
    completed)
      log "SUCCESS: completed goal_id=${goal_id}"
      exit 0
      ;;
    blocked|error|stopped)
      err "terminal phase=${phase} goal_id=${goal_id}"
      printf '%s\n' "$status_json" | head -c 2000
      echo
      exit 1
      ;;
  esac

  # Fallback: goal itself may already be completed while orch phase lags.
  case "${goal_status}" in
    completed)
      log "SUCCESS: goal status completed goal_id=${goal_id}"
      exit 0
      ;;
    blocked|failed|cancelled)
      err "goal terminal status=${goal_status} goal_id=${goal_id}"
      printf '%s\n' "$status_json" | head -c 2000
      echo
      exit 1
      ;;
  esac

  sleep "$POLL_SEC"
done
