# Docker 部署

Docker 适合文件、Git、原生 Skill、浏览器自动化等能力；macOS 桌面自动化必须使用裸机部署。

## 构建

```bash
make docker-build
```

## Compose

```bash
make docker-up
make logs
make docker-down
```

## 注意

- Compose 文件只放配置原则，不写私有 token。
- workspace 和 AgentDock 控制目录应使用持久化 volume。
