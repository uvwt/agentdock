# VPS systemd 部署

VPS 侧适合 AgentDock 远程入口、FRPS、Caddy、公网反代和 Cloudflare DNS 插件。

## 常用验证

```bash
systemctl status agentdock
journalctl -u agentdock -n 100 --no-pager
curl -fsS http://127.0.0.1:8765/healthz
```

## 注意

VPS 不具备 macOS 桌面自动化能力，不应启用 `desktop_*`。
