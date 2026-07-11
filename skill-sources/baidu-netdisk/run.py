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
os.environ["SKILL_NAME"] = "baidu-netdisk"
sys.argv = ["bdpan_helper.py", action]
runpy.run_path(str(Path(__file__).parent / "scripts" / "bdpan_helper.py"), run_name="__main__")
