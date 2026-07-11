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
os.environ["SKILL_NAME"] = "cloudsaver"
operation = action
operation_map = {'search-with-login': 'searchWithLogin', 'douban-hot': 'doubanHot', 'get-setting': 'getSetting', 'save-setting': 'saveSetting', 'cloud115-share-info': 'cloud115ShareInfo', 'cloud115-folders': 'cloud115Folders', 'cloud115-save': 'cloud115Save', 'quark-share-info': 'quarkShareInfo', 'quark-folders': 'quarkFolders', 'quark-save': 'quarkSave', 'tele-images': 'teleImages'}
sys.argv = ["cloudsaver_helper.py", operation_map.get(operation, operation)]
runpy.run_path(str(Path(__file__).parent / "scripts" / "cloudsaver_helper.py"), run_name="__main__")
