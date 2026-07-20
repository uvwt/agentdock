#!/usr/bin/env bash
set -euo pipefail

runtime_image="${AGENTDOCK_TEST_RUNTIME_IMAGE:-agentdock:test-runtime}"
dev_image="${AGENTDOCK_TEST_DEV_IMAGE:-agentdock:test-dev}"
browser_image="${AGENTDOCK_TEST_BROWSER_IMAGE:-agentdock:test-browser}"
build_images="${AGENTDOCK_TEST_BUILD:-true}"
pull_images="${AGENTDOCK_TEST_PULL:-}"
expected_version="${AGENTDOCK_EXPECT_IMAGE_VERSION:-}"
build_commit="${AGENTDOCK_TEST_BUILD_COMMIT:-$(git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)}"
build_date="${AGENTDOCK_TEST_BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
max_runtime_bytes="${AGENTDOCK_TEST_MAX_RUNTIME_BYTES:-700000000}"
max_dev_bytes="${AGENTDOCK_TEST_MAX_DEV_BYTES:-1100000000}"
max_browser_bytes="${AGENTDOCK_TEST_MAX_BROWSER_BYTES:-1600000000}"

runtime_container=""
browser_container=""
test_volume=""
work_dir="$(mktemp -d)"

cleanup() {
  if [[ -n "$runtime_container" ]]; then
    docker rm -f "$runtime_container" >/dev/null 2>&1 || true
  fi
  if [[ -n "$browser_container" ]]; then
    docker rm -f "$browser_container" >/dev/null 2>&1 || true
  fi
  if [[ -n "$test_volume" ]]; then
    docker volume rm -f "$test_volume" >/dev/null 2>&1 || true
  fi
  rm -rf "$work_dir"
}
trap cleanup EXIT

wait_for_healthy() {
  local container="$1"
  local label="$2"
  local health_state=""
  for attempt in $(seq 1 30); do
    health_state="$(docker inspect --format '{{.State.Health.Status}}' "$container")"
    printf '%s health[%s]=%s\n' "$label" "$attempt" "$health_state"
    if [[ "$health_state" == "healthy" ]]; then
      return 0
    fi
    if [[ "$health_state" == "unhealthy" ]]; then
      docker logs "$container"
      return 1
    fi
    sleep 1
  done
  docker logs "$container"
  return 1
}

assert_image_size() {
  local image="$1"
  local maximum="$2"
  local label="$3"
  local size
  size="$(docker image inspect "$image" --format '{{.Size}}')"
  printf '%s image size=%s bytes\n' "$label" "$size"
  if (( size > maximum )); then
    printf '%s image exceeds limit %s bytes\n' "$label" "$maximum" >&2
    return 1
  fi
}

if [[ -z "$pull_images" ]]; then
  if [[ "$build_images" == "true" ]]; then
    pull_images="false"
  else
    pull_images="true"
  fi
fi

if [[ "$build_images" == "true" ]]; then
  docker_build_args=(
    --build-arg "BUILD_COMMIT=$build_commit"
    --build-arg "BUILD_DATE=$build_date"
  )
  docker build "${docker_build_args[@]}" --target runtime -t "$runtime_image" .
  docker build "${docker_build_args[@]}" --target dev -t "$dev_image" .
  docker build "${docker_build_args[@]}" --target browser -t "$browser_image" .
elif [[ "$build_images" != "false" ]]; then
  printf 'AGENTDOCK_TEST_BUILD must be true or false\n' >&2
  exit 1
fi

if [[ "$pull_images" == "true" ]]; then
  docker pull "$runtime_image"
  docker pull "$dev_image"
  docker pull "$browser_image"
elif [[ "$pull_images" != "false" ]]; then
  printf 'AGENTDOCK_TEST_PULL must be true or false\n' >&2
  exit 1
fi

AGENTDOCK_AUTH_TOKEN=compose-config-token \
AGENTDOCK_IMAGE="$runtime_image" \
AGENTDOCK_BROWSER_IMAGE="$browser_image" \
AGENTDOCK_PUBLISH_PORT=18767 \
  docker compose -f docker-compose.yml -f docker-compose.browser.yml config >"$work_dir/compose.yml"
grep -q '/home/agentdock/.agentdock' "$work_dir/compose.yml"
grep -q '/home/agentdock/AgentDock' "$work_dir/compose.yml"
grep -q 'agentdock-healthcheck' "$work_dir/compose.yml"

assert_image_size "$runtime_image" "$max_runtime_bytes" runtime
assert_image_size "$dev_image" "$max_dev_bytes" dev
assert_image_size "$browser_image" "$max_browser_bytes" browser

test "$(docker run --rm "$runtime_image" id -u)" = "10001"
test "$(docker run --rm "$runtime_image" id -g)" = "10001"
if docker run --rm "$runtime_image" sh -c 'command -v go'; then
  printf 'runtime image unexpectedly contains Go\n' >&2
  exit 1
fi
docker run --rm "$runtime_image" sh -c '
  node --version >/dev/null
  npm --version >/dev/null
  pnpm --version >/dev/null
  python3 --version >/dev/null
  git --version >/dev/null
  rg --version >/dev/null
  fd --version >/dev/null
'

if [[ "$build_images" == "true" ]]; then
  version_output="$(docker run --rm --entrypoint agentdock "$runtime_image" --version)"
  printf '%s\n' "$version_output"
  grep -Fq "commit: ${build_commit:0:12}" <<<"$version_output"
  grep -Fq "built: $build_date" <<<"$version_output"
fi

if [[ -n "$expected_version" ]]; then
  actual_version="$(docker image inspect "$runtime_image" --format '{{ index .Config.Labels "org.opencontainers.image.version" }}')"
  test "$actual_version" = "$expected_version"
  actual_dev_version="$(docker image inspect "$dev_image" --format '{{ index .Config.Labels "org.opencontainers.image.version" }}')"
  test "$actual_dev_version" = "dev-$expected_version"
  actual_browser_version="$(docker image inspect "$browser_image" --format '{{ index .Config.Labels "org.opencontainers.image.version" }}')"
  test "$actual_browser_version" = "browser-$expected_version"
fi

test_volume="agentdock-image-test-${RANDOM}-${RANDOM}"
docker volume create "$test_volume" >/dev/null
docker run --rm -v "$test_volume:/home/agentdock/.agentdock" "$runtime_image" sh -c '
  touch "$HOME/.agentdock/write-test"
  test "$(stat -c %u "$HOME/.agentdock/write-test")" = "10001"
  test "$(stat -c %g "$HOME/.agentdock/write-test")" = "10001"
'
docker volume rm -f "$test_volume" >/dev/null
test_volume=""

runtime_container="$(docker run -d --rm -e AGENTDOCK_AUTH_TOKEN=runtime-health-value "$runtime_image")"
wait_for_healthy "$runtime_container" runtime
docker exec "$runtime_container" sh -c 'curl -fsS http://127.0.0.1:8765/healthz >/dev/null'
docker exec "$runtime_container" sh -c '
  test -f "$HOME/.agentdock/skill-store/bundled-skills.json"
  for skill in skill-authoring skill-installation skill-vetter-runtime; do
    version="$(jq -r .active_version "$HOME/.agentdock/skill-store/state/$skill.json")"
    test -n "$version"
    test -f "$HOME/.agentdock/skill-store/installed/$skill/$version/SKILL.md"
  done
'
docker rm -f "$runtime_container" >/dev/null
runtime_container=""

test "$(docker run --rm "$dev_image" id -u)" = "10001"
test "$(docker run --rm "$dev_image" id -g)" = "10001"
docker run --rm "$dev_image" sh -c '
  go version
  cc --version >/dev/null
  c++ --version >/dev/null
  tmp="$(mktemp -d)"
  printf "package main\nfunc main() {}\n" >"$tmp/main.go"
  go build -o "$tmp/go-smoke" "$tmp/main.go"
  printf "int main(void) { return 0; }\n" | cc -x c - -o "$tmp/c-smoke"
  "$tmp/go-smoke"
  "$tmp/c-smoke"
'

test "$(docker run --rm "$browser_image" id -u)" = "10001"
test "$(docker run --rm "$browser_image" id -g)" = "10001"
docker run --rm "$browser_image" sh -c '
  test "$AGENTDOCK_BROWSER_RUNNER_DIR" = /opt/agentdock/browser-runner
  test "$AGENTDOCK_BROWSER_EXECUTABLE_PATH" = /usr/bin/chromium
  test -f "$AGENTDOCK_BROWSER_RUNNER_DIR/browser-runner.js"
  test -f "$AGENTDOCK_BROWSER_RUNNER_DIR/package-lock.json"
  test ! -e "$HOME/.agentdock/browser-runner/browser-runner.js"
  npm --prefix "$AGENTDOCK_BROWSER_RUNNER_DIR" ls --omit=dev >/dev/null
'

docker run --rm \
  --entrypoint node \
  --workdir /opt/agentdock/browser-runner \
  "$browser_image" \
  --input-type=module \
  -e '
    import { chromium } from "playwright-core";
    const browser = await chromium.launch({
      executablePath: process.env.AGENTDOCK_BROWSER_EXECUTABLE_PATH,
      headless: true
    });
    const page = await browser.newPage();
    await page.setContent("<title>AgentDock Browser Image</title><main>browser-ok</main>");
    if (await page.title() !== "AgentDock Browser Image") process.exit(1);
    if (await page.locator("main").textContent() !== "browser-ok") process.exit(1);
    await browser.close();
  '

browser_token="browser-smoke-${RANDOM}-${RANDOM}"
browser_container="$(docker run -d --rm -p 127.0.0.1::8765 -e AGENTDOCK_AUTH_TOKEN="$browser_token" "$browser_image")"
wait_for_healthy "$browser_container" browser
browser_port="$(docker port "$browser_container" 8765/tcp | awk -F: 'NR == 1 {print $NF}')"
AGENTDOCK_SMOKE_URL="http://127.0.0.1:$browser_port" \
AGENTDOCK_AUTH_TOKEN="$browser_token" \
AGENTDOCK_SMOKE_BROWSER=true \
AGENTDOCK_SMOKE_TIMEOUT_SECONDS=30 \
  ./scripts/smoke-docker.sh

docker exec "$browser_container" sh -c '
  test ! -e "$HOME/.agentdock/browser-runner/browser-runner.js"
  test -f "$HOME/.agentdock/browser-artifacts/browser-state.json"
'
docker rm -f "$browser_container" >/dev/null
browser_container=""

printf 'AgentDock Docker image verification passed\n'
