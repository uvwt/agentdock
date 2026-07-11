#!/usr/bin/env python3
import json, os, runpy, sys
from pathlib import Path

payload = sys.stdin.read()
request = json.loads(payload or "{}")
if not isinstance(request, dict):
    raise SystemExit("input must be a JSON object")
action = str(request.pop("skill_action", "status"))
payload = json.dumps(request, ensure_ascii=False)
os.environ["SKILL_ARGS_JSON"] = payload or "{}"
os.environ["SKILL_NAME"] = "rsshub"
operation = action
operation_map = {'build-url': 'buildUrl', 'fetch-feed': 'fetchFeed', 'parse-feed': 'parseFeed', 'probe-route': 'probeRoute'}
sys.argv = ["rsshub_helper.py", operation_map.get(operation, operation)]
runpy.run_path(str(Path(__file__).parent / "scripts" / "rsshub_helper.py"), run_name="__main__")
