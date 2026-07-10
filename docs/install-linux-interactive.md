# Linux 问答式一键部署

`scripts/install-linux.sh` 用于在 systemd Linux 上交互式安装 AgentDock。它适合 VPS、云主机、家用 Linux 主机等长期运行场景。

默认部署方式是：

```text
client -> HTTPS reverse proxy -> 127.0.0.1:8765 -> agentdock systemd/OpenRC service
```

默认安装方式是 `binary`：下载 GitHub Release 预编译二进制，只安装运行依赖，不安装 Go/gcc 编译链。需要从源码构建时再选择 `source`。

生产环境建议只让 AgentDock 监听 `127.0.0.1`，再用 Caddy、Nginx 或其他反代提供 HTTPS。不要把未鉴权的 `/mcp` 直接暴露到公网。

## 快速使用

在已经 clone 的仓库里运行：

```bash
bash scripts/install-linux.sh
```

单文件远程安装：

```bash
curl -fsSL https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-linux.sh -o /tmp/agentdock-install.sh
bash /tmp/agentdock-install.sh
```

Alpine / 极简系统如果连 `curl` 和 `bash` 都没有，先用 BusyBox `wget` 跑 bootstrap：

```sh
wget -O /tmp/agentdock-bootstrap.sh https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-linux-bootstrap.sh
sh /tmp/agentdock-bootstrap.sh
```

或者手动补齐最小依赖后再运行主脚本：

```sh
apk add --no-cache bash curl ca-certificates
curl -fsSL https://raw.githubusercontent.com/uvwt/agentdock/main/scripts/install-linux.sh -o /tmp/agentdock-install.sh
bash /tmp/agentdock-install.sh
```

远程安装模式下，脚本会按提示 clone 默认仓库：

```text
https://github.com/uvwt/agentdock.git
```

## Alpine / OpenRC 支持

脚本会自动识别服务管理器：

```text
systemd：写入 /etc/systemd/system/agentdock.service，并使用 systemctl 启动
OpenRC：写入 /etc/init.d/agentdock，并使用 rc-update / rc-service 启动
none：只构建二进制和写入 env，不安装系统服务
```

Alpine 默认会走 OpenRC。安装完成后常用命令是：

```sh
rc-service agentdock status
rc-service agentdock restart
tail -n 100 /var/log/agentdock.log /var/log/agentdock.err
```

## 脚本会询问什么

主要问题包括：

```text
Git 仓库 URL
Git 分支
安装目录
运行数据根目录
环境变量文件
安装方式：binary/source/auto
Release 版本：latest 或 vX.Y.Z
服务管理器：auto/systemd/openrc/none
服务名
运行用户
监听地址和端口
工具 profile
运行模式、路径策略、命令沙箱
Bearer token
是否配置 RecallDock endpoint/token
是否配置 NexusDock workflow endpoint
公网域名提示
```

常用默认值：

```text
安装目录：/opt/agentdock
运行数据根目录：/srv/agentdock
env 文件：/etc/agentdock/agentdock.env
服务名：agentdock
运行用户：agentdock
监听：127.0.0.1:8765
```

## 执行内容

脚本会执行这些步骤：

1. 检查 Linux 和 systemd。
2. 默认 `binary` 模式只安装运行依赖：`curl`、`ca-certificates`、`tar`、`gzip`、`openssl`，Alpine 额外安装 `openrc`。
3. 下载 `agentdock_linux_amd64.tar.gz` 或 `agentdock_linux_arm64.tar.gz` 到安装目录。
4. 只有选择 `source` 或 `auto` fallback 时才安装 `git`、Go、gcc/build-base 等编译依赖。
5. 创建运行用户和数据目录。
6. 写入 root-only 环境变量文件。
7. 按服务管理器写入 systemd unit 或 OpenRC init 脚本。
8. 使用 `systemctl` 或 `rc-service` 启动/重启服务。
9. 验证 `/healthz`。安装目录存在 smoke 脚本时额外执行 MCP smoke。

## 环境变量覆盖默认值

所有关键默认值都可以在运行脚本前覆盖：

```bash
AGENTDOCK_BRANCH=main \
AGENTDOCK_SOURCE_DIR=/opt/agentdock \
AGENTDOCK_DATA_DIR=/srv/agentdock \
AGENTDOCK_PORT=8765 \
bash scripts/install-linux.sh
```

常用变量：

```text
AGENTDOCK_REPO_URL
AGENTDOCK_INSTALL_MODE          binary/source/auto，默认 binary
AGENTDOCK_RELEASE_VERSION       latest 或 vX.Y.Z，默认 latest
AGENTDOCK_BRANCH
AGENTDOCK_SOURCE_DIR
AGENTDOCK_DATA_DIR
AGENTDOCK_ENV_FILE
AGENTDOCK_SERVICE_NAME
AGENTDOCK_SERVICE_USER
AGENTDOCK_HOST
AGENTDOCK_PORT
AGENTDOCK_AUTH_TOKEN
AGENTDOCK_GO_VERSION
```

不要把真实 token 写进可提交文件或 shell history。推荐在脚本交互中输入 token，或留空让脚本自动生成。

## 安装后的文件

```text
/opt/agentdock/bin/agentdock              AgentDock 二进制
/srv/agentdock/.agentdock                 AgentDock 内部状态目录
/srv/agentdock/AgentDock                  默认工作目录
/etc/agentdock/agentdock.env              root-only 环境变量
/etc/systemd/system/agentdock.service     systemd unit，systemd 系统使用
/etc/init.d/agentdock                     OpenRC init 脚本，Alpine/OpenRC 使用
```

## 安装后验证

```bash
sudo systemctl status agentdock --no-pager
sudo journalctl -u agentdock -n 100 --no-pager
curl -fsS http://127.0.0.1:8765/healthz
```

MCP smoke：

```bash
cd /opt/agentdock
AGENTDOCK_SMOKE_URL=http://127.0.0.1:8765 \
AGENTDOCK_AUTH_TOKEN="$(sudo awk -F= '/^AGENTDOCK_AUTH_TOKEN=/{print $2}' /etc/agentdock/agentdock.env)" \
make smoke-docker
```

## 反代示例

Caddy：

```caddyfile
agentdock.example.com {
  reverse_proxy 127.0.0.1:8765
}
```

客户端 MCP URL：

```text
https://agentdock.example.com/mcp
```

## 升级

重新运行脚本即可。默认 `binary` 模式会重新下载 GitHub Release 的预编译二进制并重启服务。

如果选择 `source` 模式，安装目录存在且干净时，脚本会按提示执行：

```bash
git pull --ff-only origin <branch>
```

然后重新构建、重启服务并验证。如果安装目录有未提交改动，脚本会跳过 `git pull`，避免覆盖本地修改。

## 与手动 VPS systemd 文档的关系

- 想全自动、问答式填写配置：使用本文脚本。
- 想逐项手动审查 systemd/env/反代：阅读 [VPS systemd 部署](install-vps-systemd.md)。
