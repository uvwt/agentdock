# 排障

## 工具已经更新，但 ChatGPT 侧看不到

1. 确认实际运行二进制来自 `$HOME/agentdock/agentdock`。
2. 执行：

```bash
cd ~/agentdock
make install-macos
make restart-macos
```

3. 检查：

```bash
curl -fsS http://127.0.0.1:18766/healthz
tail -n 100 ~/agentdock-runtime/agentdock.err.log
```

## 桌面操作返回 ok=true 但 UI 没变

`ok=true` 只代表命令发出。请使用：

```json
{"verify": true, "wait_ms": 300}
```

并检查 `effect_verified`、`effect_changed`、`error_layer`。

## Git push 权限失败

检查 remote、credential helper 和 GitHub token，不要在 README 或日志中记录私有 token。

## Docker 构建后仍是旧代码

确认 Compose 使用的是新镜像，必要时：

```bash
docker compose build --no-cache
docker compose up -d
```
