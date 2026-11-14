# Linux 配置与生效

适用于官方 Linux 安装脚本创建的 systemd/OpenRC 服务，以及用户自己用服务管理器托管 AgentDock 的场景。

## 配置事实源

官方 Linux 安装脚本允许用户选择：

- 环境文件路径；
- 服务名；
- 服务用户；
- systemd / OpenRC / 不安装系统服务。

默认环境文件是：

```text
/etc/agentdock/agentdock.env
```

默认服务名是：

```text
agentdock
```

这些都可以被安装参数/环境变量覆盖，所以修改前先检查真实 service 定义：

```bash
systemctl cat agentdock
```

重点确认 `EnvironmentFile=`、`ExecStart=` 和实际 service 名称。OpenRC 则检查实际 `/etc/init.d/<service>` 中引用的环境文件。

官方安装器还会在环境文件所在目录写 `desktop-runtime.json`，其中记录 service manager、service name、Core binary 和 environment file 等运行信息。它是运行清单，不是所有配置键的替代文件。

## CLI 管理运行服务与 Tunnel

官方默认安装下，`service` 和 `tunnel` 会自动使用 `/etc/agentdock`，无需每次重复传 `--runtime-root`：

```bash
agentdock service status
agentdock service restart
agentdock tunnel status
agentdock tunnel restart
```

运行配置目录的解析优先级为：显式 `--runtime-root`、`AGENTDOCK_RUNTIME_ROOT`、平台默认目录。自定义环境文件目录或多实例部署仍应显式指定对应目录，避免操作错误实例。

## NexusDock 手动配对

官方一键安装会把服务真实的 `AGENTDOCK_HOME` 写入运行配置目录（默认 `/etc/agentdock/agentdock.env`）。
**后续手动执行** `sudo agentdock nexus pair` 时，CLI 会自动从服务运行配置目录解析 `AGENTDOCK_HOME`，保持服务用户对 `nexus/device.json` 的读取权限，并在保存凭据后**自动重启对应的 systemd/OpenRC 服务**使其立即生效：

```bash
sudo agentdock nexus pair \
  --endpoint https://mcp.nexusdock.co --code "你的配对码"
sudo agentdock nexus status
```

如果使用自定义运行配置目录或多实例部署，请显式传入 `--runtime-root <目录>`；如果希望仅写入凭据而不立即重启服务，可附加 `--no-restart`。

当仅以低权限服务用户身份执行配对（无权直接调用 `systemctl`/`rc-service` 重启系统服务）时，CLI 仍会成功保存凭据（不会因重启权限不足而报错浪费一次性配对码），并提示需要补充执行的重启命令：

```bash
sudo -u agentdock agentdock nexus pair \
  --endpoint https://mcp.nexusdock.co --code "你的配对码" \
  --agentdock-home /srv/agentdock/.agentdock
sudo systemctl restart agentdock
sudo -u agentdock agentdock nexus status \
  --agentdock-home /srv/agentdock/.agentdock
```

配对成功时 CLI 会显示实际保存的 `nexus/device.json` 路径，必须与
Core 使用的 `AGENTDOCK_HOME/nexus/device.json` 一致。不能在普通登录用户的
家目录（如 `~/.agentdock`）中无 `sudo`、无 `--agentdock-home` 配对后就认为系统服务已连接。仅查询状态时可使用 `--json`，不会输出 Device Token。

## 修改方式

systemd/OpenRC 部署时，修改服务实际加载的环境文件，而不是另开一个 shell `export` 后期待后台服务继承。

官方安装器默认以 root:root、0600 保存环境文件。修改时保持现有 owner/mode，不要把认证配置暴露给其他用户。

如果需要给 `exec_command` 透传宿主环境变量，必须同时满足：

1. 源变量真实存在于 AgentDock **服务进程**的环境中；
2. `AGENTDOCK_COMMAND_ENV_FROM_ENV_JSON` 显式声明子进程变量到宿主变量的映射。

服务不会自动读取某个登录用户的 `.bashrc`、`.zshrc` 或交互式 Shell 环境。

## 生效

systemd：

```bash
sudo systemctl restart <实际服务名>
sudo systemctl status <实际服务名> --no-pager
```

OpenRC：

```bash
sudo rc-service <实际服务名> restart
sudo rc-service <实际服务名> status
```

只修改 EnvironmentFile 的内容通常不需要 `daemon-reload`；如果同时修改了 systemd unit 本身，则先执行：

```bash
sudo systemctl daemon-reload
```

再重启服务。

如果安装时选择了“不安装系统服务”，则按直接运行二进制处理：重新加载真实启动环境并重启进程。

## 验证与排障

优先检查：

```bash
curl -fsS http://127.0.0.1:<实际端口>/healthz
```

systemd 日志：

```bash
sudo journalctl -u <实际服务名> -n 100 --no-pager
```

OpenRC 默认日志位置取决于安装脚本/服务定义；官方脚本使用 `/var/log/<service>.log` 和 `/var/log/<service>.err`。

如果“文件已经改了但行为没变”，重点确认：

1. 是否编辑了 service 真正引用的 EnvironmentFile；
2. 服务是否真的重启成功；
3. 新进程是否因为配置校验失败而反复退出；
4. 是否有 systemd unit 的额外 `Environment=`、容器层或外部进程管理器覆盖了预期值。
