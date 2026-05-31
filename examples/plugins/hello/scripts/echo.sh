#!/usr/bin/env sh
set -eu
printf '{"ok":true,"plugin":"%s","action":"%s","args":%s}\n' "$PLUGIN_NAME" "$PLUGIN_ACTION" "${PLUGIN_ARGS_JSON:-{}}"
