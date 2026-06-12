# 改善外部用户部署与运行体验

## Goal

让外部用户更容易部署和运行 AgentDock：本机 Docker 用户可以快速从 clone 到可用 MCP，并用可执行 smoke 验证；公网 VPS 用户有清晰的 production path，包含鉴权、反代、systemd、日志和升级验证。

## What I Already Know

* 用户明确要求“头脑风暴”。
* 当前仓库是 `/Users/xx/agentdock`，Trellis 当前无 active task，已创建临时规划任务 `.trellis/tasks/06-09-brainstorm-tbd`。
* 仓库是单一 Go module，主要规范层为 backend。
* AgentDock 新增具体扩展默认应走原生 Skill Runtime，不走旧动态 plugin 路径。
* 重要 AgentDock / 部署 / 排障 / 偏好敏感任务开始前应先加载 MemoryDock 记忆；本任务已完成一次 `memory_bootstrap(project=agentdock)`。
* 本次主题已明确为“功能体验”。
* 当前用户可见体验入口主要包括 README/docs、MCP tool descriptors/schema、`skill_manage`、MemoryDock 工具、browser/desktop 自动化工具和部署/排障路径。
* 功能体验主线已进一步收敛为“部署/运行体验”。
* 现有部署入口包括 macOS 裸机 `make install-macos` / `make restart-macos` / `make smoke-macos`、Docker `make docker-build` / `make docker-up` / `make logs` / `make docker-down`、VPS systemd 文档验证命令。
* macOS 裸机路径目前最完整：安装脚本会 gofmt check、test、vet、build、codesign、备份并替换二进制；restart 脚本重启 launchd 并查 healthz；smoke 脚本查 healthz、依赖、AppleScript 和截图权限。
* Docker 路径目前偏基础运行闭环，VPS 路径目前偏手工运维验证笔记。
* 既有记忆强调：AgentDock 部署/更新必须先查 MemoryDock runbook；部署完成不能只靠 healthz 或 `server_info`，应包含真实 Mini coding-tool smoke。
* 用户进一步明确目标是“别人部署也很方便”，优先级从本机自用更新转向外部用户首次部署/自托管体验。
* 当前 Dockerfile 已自包含 Go runtime 和常用工具；`docker-entrypoint.sh` 会初始化 `/workspace`、`AGENTDOCK_DIR`、plugins、browser artifacts、state/cache/runbooks，并在浏览器增强镜像中自动复制 browser runner。
* 当前默认 `docker-compose.yml` 暴露本机端口 `127.0.0.1:18766:8765`，挂载 `./workspace` 与 `./AgentDock`，但也默认传入 `--dangerously-skip-all-permissions`，这会影响外部用户默认部署边界。
* `docker-compose.browser.yml` 只是 overlay，启用 `AGENTDOCK_BROWSER_ENABLED=true` 并使用 `agentdock:browser` 镜像；当前 README/Docker 文档没有形成“一条命令部署 + 一条命令验证 + 可选浏览器增强”的完整新手路径。
* 用户选择同时覆盖本机 Docker 用户和公网 VPS 用户：README 提供 Docker quickstart，docs 提供 VPS production path。
* 用户确认 MVP 可以包含 `make smoke-docker` / `make doctor` 这类实际检查命令，而不是只整理文档。
* AgentDock 已有 `/healthz`、`/mcp`、可选 bearer/OAuth 鉴权、workspace/path policy 和 `server_info`，可作为轻量 smoke 的验证基础。
* 用户确认默认边界：localhost quickstart 可以保留 demo 便利模式，但 VPS production path 默认不跳过权限，并要求设置鉴权。

## Assumptions (Temporary)

* Docker quickstart 面向本机/受信环境，优先降低首次体验门槛。
* VPS production path 面向长期公网运行，优先明确鉴权、反代和权限边界。
* `smoke-docker` 先做轻量真实验证；完整 `doctor` 可以作为后续增强。
* macOS 裸机桌面自动化不是本次 MVP 主线。

## Open Questions

* 是否按当前 MVP 范围进入实现阶段？

## Requirements (Evolving)

* 需求发现阶段每次只问一个高价值问题。
* 能从仓库、规范、记忆或快速检查得出的信息，由 Codex 先查再总结。
* 收敛后应形成明确 MVP、验收标准和 out-of-scope。
* 优先从真实用户工作流出发，而不是先罗列内部模块改动。
* 部署完成的定义应包含“服务活着 + 能执行真实工具动作 + 失败时能指出下一步”，而不是只返回 healthz 成功。
* 面向外部用户的默认路径应避免机器特定路径、私有 token、Mac mini 专属签名身份和本地秘密进入 README/compose。
* 新手部署体验需要明确默认权限边界：开箱即用、但高风险动作如何授权/提示，不能只把 `--dangerously-skip-all-permissions` 当成无解释默认值。
* README quickstart 应尽量短：clone/build/up/smoke/connect，详细参数和生产建议放到 docs。
* VPS production path 应覆盖 systemd、反代、鉴权、日志、升级、回滚/验证，但避免记录私有值。
* MVP 应新增或整理可执行 smoke：至少一条 Docker 本地 smoke 命令，验证服务可用和 MCP 基本交互，而不是只检查容器进程。
* localhost quickstart 可以保留 `--dangerously-skip-all-permissions` 便利模式，但文档必须明确它只适合本机/受信 demo 环境。
* VPS production path 默认不使用 `--dangerously-skip-all-permissions`，并要求配置 `AGENTDOCK_AUTH_TOKEN` 或等价鉴权方案。

## MVP Scope

* 更新 `README.md`，提供最短 Docker quickstart：build、up、smoke、连接 MCP、可选浏览器增强入口。
* 更新 `docs/install-docker.md`，补齐 compose 目录结构、volume、端口、权限模式、鉴权提示、browser overlay、smoke 失败排查入口。
* 更新 `docs/install-vps-systemd.md`，补齐 production path：systemd、反代、鉴权、日志、升级、回滚/验证原则。
* 更新 `docs/troubleshooting.md`，加入 Docker smoke 和 VPS health/MCP 失败时的下一步检查。
* 新增 `make smoke-docker` 与对应脚本，验证本地 Docker 部署的 healthz 和 MCP 基本交互。
* 如需要，调整 `docker-compose.yml` 注释，使 demo 便利模式和 production 权限边界更清晰。

## Acceptance Criteria (Evolving)

* [x] 明确本次头脑风暴主题。
* [x] 明确功能体验主线为部署/运行体验。
* [x] 明确用户目标是让外部用户部署更方便。
* [x] 明确默认外部部署路径：Docker quickstart + VPS production path。
* [x] 确认 MVP 包含轻量可执行 smoke/doctor 入口。
* [x] 明确 MVP 范围与不做范围。
* [x] 如进入实现，先补充相关规范上下文并再启动任务。

## Definition of Done (Team Quality Bar)

* Tests added/updated when implementation changes behavior.
* `make check` passes before completion when code changes are made.
* Docs/notes updated only when behavior, workflow, or stable project knowledge changes.
* Rollout/rollback considered if the change affects deployed AgentDock.

## Out of Scope (Explicit)

* 不做完整跨环境 `doctor` 命令。
* 不做 VPS 一键安装器。
* 不做公网反代配置自动生成。
* 不改 macOS 裸机签名/launchd 更新路径。
* 不新增旧动态 plugin 能力。
* 不把临时猜测写入长期记忆。

## Technical Notes

* Loaded Trellis start and brainstorm guidance.
* Read `.trellis/spec/guides/index.md` and `.trellis/spec/backend/index.md`.
* Current repo status before PRD seed was clean except the newly created task directory.
* Quick scan found user-facing surfaces in `README.md`, `docs/*.md`, `internal/mcp/registry.go`, `internal/mcp/input_schema.go`, `internal/tools/skill_manage.go`, `internal/tools/memory*.go`, `internal/tools/browser.go`, and `internal/tools/desktop*.go`.
* Deployment/operation scan covered `Makefile`, `docs/install-macos-launchd.md`, `docs/install-docker.md`, `docs/install-vps-systemd.md`, `scripts/install-macos.sh`, `scripts/restart-macos.sh`, and `scripts/smoke-desktop-macos.sh`.
* External deployment scan covered `Dockerfile`, `docker-compose.yml`, `docker-compose.browser.yml`, `docker-entrypoint.sh`, `README.md`, and `docs/troubleshooting.md`.
* Implementation started after reading Trellis backend quality, error-handling, logging, directory-structure, and shared code-reuse guidelines.
* Verification completed with `make check`, `docker compose config`, browser overlay compose config, `make docker-build`, local AgentDock smoke, and a temporary Docker container smoke on port 18767.
* Docker build initially failed because Dockerfile did not copy `generated/`; fixed both `Dockerfile` and `Dockerfile.browser`, and recorded the build-context contract in `.trellis/spec/backend/quality-guidelines.md`.
