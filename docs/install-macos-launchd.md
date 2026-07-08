# macOS 裸机 launchd 部署

macOS 裸机部署使用当前登录用户运行 AgentDock。AgentDock 使用单一 Host 路径模型：

```text
~/.agentdock   内部状态目录
~/AgentDock    默认工作目录
```

裸机模式是可信本机高权限模式；实际能访问的文件由当前 macOS 用户权限决定。需要 macOS 桌面自动化时，使用原生 Skill Runtime 的 `desktop` Skill。

## 安装与启动

```bash
cd ~/agentdock
make check
make install-macos
make restart-macos
make smoke-macos
```

## 配置建议

本机只监听 `127.0.0.1` 时通常用于本地 Agent。需要跨设备或公网访问时，建议配置 bearer token、HTTPS 反代，并只暴露给可信客户端。
