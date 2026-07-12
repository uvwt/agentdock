---
name: skill-vetter-runtime
description: Review ClawHub or local Skill packages before installation, classify risk, and return a structured security report.
version: 0.1.4
---

# Skill Vetter

This Skill provides a security-first review workflow and an optional helper script for inspecting ClawHub slugs or local Skill directories.

Use it before installing unknown skills, when comparing candidate skills, or when checking whether a local Skill has suspicious code, broad permissions, credential access, persistence hooks, or risky network behavior.

The output is a structured JSON report with reviewed files, detected red flags, permission clues, risk level, verdict, and notes.

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/skill-vetter-runtime/0.1.4"
```

调用 `exec_command` 时传入 `skill_env: "skill-vetter-runtime"`，由 AgentDock 自动加载该 Skill 的独立环境；不要在命令中手工 `source` 环境文件。

调用动作（`exec_command` 同时传入 `"skill_env": "skill-vetter-runtime"`）：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Check Python runtime and optional ClawHub CLI availability. |
| `vet-clawhub-slug` | Fetch and vet a ClawHub skill by slug without installing it. |
| `vet-local-path` | Vet a local skill folder. By default only AgentDock workspace and installed skill roots are allowed. |
