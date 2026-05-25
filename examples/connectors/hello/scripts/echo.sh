#!/usr/bin/env sh
set -eu
printf '{"ok":true,"connector":"%s","action":"%s","args":%s}\n' "$CONNECTOR_NAME" "$CONNECTOR_ACTION" "${CONNECTOR_ARGS_JSON:-{}}"
