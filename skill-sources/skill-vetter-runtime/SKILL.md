---
name: skill-vetter-runtime
description: Review ClawHub or local Skill packages before installation, classify risk, and return a structured security report.
version: 0.1.5
---

# Skill Vetter

This Skill provides a security-first review workflow and an optional helper script for inspecting ClawHub slugs or local Skill directories.

Use it before installing unknown skills, when comparing candidate skills, or when checking whether a local Skill has suspicious code, broad permissions, credential access, persistence hooks, or risky network behavior.

The output is a structured JSON report with reviewed files, detected red flags, permission clues, risk level, verdict, and notes.

## 辅助脚本执行

Skill 本体是本说明文档。确需调用包内辅助脚本时，在 Skill 包根目录使用相对路径执行；运行宿主负责切换到包根目录并把所需变量注入当前子进程。

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 run.py
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | Check Python runtime and optional ClawHub CLI availability. |
| `vet-clawhub-slug` | Fetch and vet a ClawHub skill by slug without installing it. |
| `vet-local-path` | Vet a local skill folder. By default only AgentDock workspace and installed skill roots are allowed. |
