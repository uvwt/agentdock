from __future__ import annotations

import json
import os
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class SkillAuthoringLintTests(unittest.TestCase):
    def run_skill(self, payload):
        env = os.environ.copy()
        env["PYTHONDONTWRITEBYTECODE"] = "1"
        proc = subprocess.run(
            [str(ROOT / "run.py")],
            input=json.dumps(payload),
            text=True,
            capture_output=True,
            env=env,
            check=False,
            timeout=5,
        )
        return proc, json.loads(proc.stdout)

    def make_skill(self, skill_md, run_py="print('ok')\n"):
        temp = tempfile.TemporaryDirectory()
        root = pathlib.Path(temp.name)
        (root / "SKILL.md").write_text(skill_md, encoding="utf-8")
        if run_py is not None:
            (root / "run.py").write_text(run_py, encoding="utf-8")
        return temp, root

    def test_status(self):
        proc, result = self.run_skill({"skill_action": "status"})
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(result["ok"])
        self.assertGreater(result["lint_rule_count"], 0)

    def test_clean_portable_skill_passes(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

# Demo

在 Skill 包根目录执行 `python3 run.py`，环境变量由运行宿主注入。
""")
        self.addCleanup(temp.cleanup)

        proc, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(result["portable"])
        self.assertEqual(result["error_count"], 0)
        self.assertEqual(result["warning_count"], 0)

    def test_hardcoded_install_path_and_env_file_access_fail(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

运行 `python3 ~/.agentdock/skill-store/installed/demo-skill/1.0.0/run.py`。
""", "from pathlib import Path\nPath('~/.agentdock/env/skill/demo-skill.env').expanduser().read_text()\n")
        self.addCleanup(temp.cleanup)

        _, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        codes = {issue["code"] for issue in result["issues"]}
        self.assertFalse(result["portable"])
        self.assertIn("HARDCODED_AGENTDOCK_INSTALL_PATH", codes)
        self.assertIn("AGENTDOCK_ENV_FILE_ACCESS", codes)

    def test_agentdock_home_installed_path_fails(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

```bash
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/demo-skill/1.0.0"
python3 "$SKILL_DIR/run.py"
```
""")
        self.addCleanup(temp.cleanup)

        _, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        self.assertFalse(result["portable"])
        self.assertIn("HARDCODED_AGENTDOCK_INSTALL_PATH", {issue["code"] for issue in result["issues"]})

    def test_explicit_prohibition_list_is_not_reported_as_dependency(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

在 Skill 包根目录执行 `python3 run.py`。

禁止把以下内容作为运行依赖：

- `AGENTDOCK_SKILL_DIR`
- `~/.agentdock/skill-store/installed/demo-skill/1.0.0`
""")
        self.addCleanup(temp.cleanup)

        _, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        self.assertTrue(result["portable"])
        self.assertEqual(result["error_count"], 0)

    def test_unrelated_advice_does_not_hide_real_dependency(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

不要泄露密钥。

运行时读取 AGENTDOCK_SKILL_DIR 并执行 `python3 run.py`。
""")
        self.addCleanup(temp.cleanup)

        _, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        self.assertFalse(result["portable"])
        self.assertIn("AGENTDOCK_SKILL_DIR_DEPENDENCY", {issue["code"] for issue in result["issues"]})

    def test_agentdock_adapter_terms_are_warnings(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

通用执行：`python3 run.py`。
AgentDock 可用 exec_command、skill_env 和 skill://demo-skill/references/api.md 验证。
""")
        self.addCleanup(temp.cleanup)

        _, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        codes = {issue["code"] for issue in result["issues"]}
        self.assertTrue(result["portable"])
        self.assertEqual(result["warning_count"], 3)
        self.assertEqual(codes, {
            "AGENTDOCK_EXEC_COMMAND_USAGE",
            "AGENTDOCK_SKILL_ENV_USAGE",
            "AGENTDOCK_SKILL_URI_USAGE",
        })

    def test_missing_relative_run_instruction_warns(self):
        temp, root = self.make_skill("""---
name: demo-skill
description: Demo.
version: 1.0.0
---

# Demo
""")
        self.addCleanup(temp.cleanup)

        _, result = self.run_skill({"skill_action": "lint", "source": str(root)})

        self.assertTrue(result["portable"])
        self.assertIn("MISSING_PORTABLE_EXECUTION", {issue["code"] for issue in result["issues"]})

    def test_unknown_action_fails_with_structured_error(self):
        proc, result = self.run_skill({"skill_action": "unknown"})
        self.assertNotEqual(proc.returncode, 0)
        self.assertEqual(result["error"]["code"], "UNKNOWN_ACTION")


if __name__ == "__main__":
    unittest.main()
