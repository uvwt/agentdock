---
name: find-skills-skill
description: Search and discover OpenClaw skills from various sources. Use when: user wants to find available skills, search for specific functionality, or discover new skills to install.
version: 1.0.4
---

# Find Skills Skill

Search and discover OpenClaw skills from various sources.

## When to Use

✅ **USE this skill when:**

- "Find skills for [task]"
- "Search for OpenClaw skills"
- "What skills are available?"
- "Discover new skills"
- "Find skills by category"

## When NOT to Use

❌ **DON'T use this skill when:**

- Installing skills → use `clawhub install`
- Managing installed skills → use `openclaw skills list`
- Creating new skills → use skill-creator skill

## Sources for Finding Skills

### 1. ClawHub (Primary)
```bash
# Search skills
npx clawhub search "keyword"

# Browse categories
npx clawhub browse
```

### 2. OpenClaw Directory
- Website: https://www.openclawdirectory.dev/skills
- Browse by category, popularity, or search

### 3. LobeHub Skills Marketplace
- Website: https://lobehub.com/skills
- Community-contributed skills

### 4. GitHub
- Search: `openclaw skill` or `agent-skill`
- Look for repositories with `SKILL.md` files

### 5. Community Forums
- SitePoint: https://www.sitepoint.com/community/
- Discord: https://discord.com/invite/clawd

## Search Strategies

### By Functionality
```bash
# Web search skills
npx clawhub search "web search"

# Weather skills
npx clawhub search "weather"

# Document skills
npx clawhub search "document"
```

### By Provider
```bash
# Tavily skills
npx clawhub search "tavily"

# GitHub skills
npx clawhub search "github"

# Calendar skills
npx clawhub search "calendar"
```

### By Popularity
```bash
# Most installed skills
npx clawhub search --sort installs

# Most starred skills
npx clawhub search --sort stars
```

## Installation Tips

1. **Check requirements** before installing
2. **Read SKILL.md** for usage instructions
3. **Test in isolation** before production use
4. **Check for updates** regularly

## Common Skill Categories

### Core Skills
- `weather` - Weather forecasts
- `skill-creator` - Create new skills
- `healthcheck` - Security audits

### Integration Skills
- `github` - GitHub operations
- `feishu` - Feishu integration
- `notion` - Notion API

### Search Skills
- `tavily-search` - Web search via Tavily
- `web-search-plus` - Enhanced web search

### Agent Skills
- `proactive-agent` - Proactive automation
- `coding-agent` - Code generation

## Troubleshooting

### Rate Limits
If you hit rate limits with clawhub:
1. Wait 1 hour before retrying
2. Use alternative sources (websites)
3. Search manually on GitHub

### Installation Issues
1. Check skill requirements
2. Verify network connectivity
3. Check OpenClaw version compatibility

## Best Practices

1. **Search before creating** - Don't reinvent the wheel
2. **Read documentation** - Understand skill capabilities
3. **Start simple** - Install one skill at a time
4. **Test thoroughly** - Verify skill works as expected
5. **Provide feedback** - Help improve skills

## Related Skills

- `clawhub` - ClawHub CLI tool
- `skill-creator` - Create new skills
- `healthcheck` - System health checks

## 辅助脚本执行

Skill 本身只提供流程说明。确需调用包内辅助脚本时，使用 `exec_command` 直接运行当前版本脚本，不调用任何 Skill 执行入口。

当前版本目录：

```bash
AGENTDOCK_HOME="${AGENTDOCK_HOME:-$HOME/.agentdock}"
SKILL_DIR="$AGENTDOCK_HOME/skill-store/installed/find-skills-skill/1.0.4"
ENV_FILE="$AGENTDOCK_HOME/skill-data/find-skills-skill/.env"
```

如存在私有环境文件，先加载：

```bash
set -a
[ ! -f "$ENV_FILE" ] || . "$ENV_FILE"
set +a
```

调用动作：

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 "$SKILL_DIR/run.py"
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `status` | 检查 ClawHub CLI 是否可用。 |
| `search` | 按关键词搜索 ClawHub Skills。 |
| `explore` | 浏览 ClawHub 最新、热门或趋势 Skills。 |
| `sources` | 列出可用于发现 Skills 的主要目录和社区来源。 |
