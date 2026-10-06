# Cloudflare Tunnel 可选组件架构

> 状态：实现设计基线，尚未代表当前版本已经完成本文全部能力。
>
> 基线：PR #187 `refactor/desktop-native-connection-ux-20261004`，本文创建时 HEAD 为 `c45a182936ff887dfea8ba381d4332792045844f`。
>
> 后续实现使用独立分支 `refactor/optional-cloudflared-component-20261004`，在 PR #187 仍未合并期间使用 stacked PR，base 指向 `refactor/desktop-native-connection-ux-20261004`。

## 1. 背景与最终决策

AgentDock 的普通远程连接已经以 NexusDock 为默认产品路径。Cloudflare Tunnel 属于高级、自托管公网访问能力，不应该继续成为 AgentDock 基础安装、升级和健康检查的强依赖。

此前讨论过一个过渡方案：Windows 改成按需安装 cloudflared，macOS 暂时继续把 cloudflared 固定打进 `AgentDock.app`。重新检查当前代码后，决定不把这个过渡状态作为长期实现，而是直接收敛到最终边界：

1. AgentDock 主安装只负责 AgentDock 自身必需组件。
2. cloudflared 是可选的、独立版本化的 managed component。
3. Cloudflare Tunnel 只在“设置 → 高级连接”中出现，不再属于首次安装流程。
4. 用户进入高级连接页面只读取状态，不自动下载任何第三方二进制。
5. cloudflared 未安装时明确展示“未安装 / 安装”；安装完成后才展示临时地址和固定域名配置。
6. AgentDock Tunnel host / supervisor 继续由 AgentDock 自己承载；cloudflared 只是它管理的子进程，不直接成为 AgentDock 主安装器的服务生命周期职责。
7. macOS 也最终把 cloudflared 移出签名 App Bundle，但绝不在安装后修改 `.app`。外部组件放在用户可写的 AgentDock component store，由 App Bundle 内已签名的 `agentdock tunnel launch` helper 启动。
8. 不在本轮提前建设“任意 Tunnel Provider 插件 SDK”。先把 cloudflared 这个真实可选依赖边界做正确，未来出现第二个 Provider 后再按真实需求抽象。

一句话目标：

```text
AgentDock Installer
    ↓ 只安装 AgentDock
AgentDock Runtime / Tunnel Host
    ↓
Cloudflare Tunnel 配置
    ↓
AgentDock Component Store
    ↓
cloudflared（可选、独立版本、可安装/更新/卸载）
```

## 2. 当前代码现状

本文不是从理想模型推导，而是基于 PR #187 当前代码确认以下事实。

### 2.1 Windows 当前仍把 cloudflared 当基础安装依赖

`scripts/install/install.ps1` 当前无论最终 `TunnelMode` 是否为 `none`，安装事务都会执行 `Install-CloudflaredBinary`。如果没有离线 override，也没有已经可用的目标文件，会直接从 Cloudflare GitHub Release 下载最新 Windows binary。

因此当前存在以下耦合：

- 普通 AgentDock 安装可能因为 Cloudflare/GitHub 网络失败而失败；
- AgentDock 更新会停止、备份、替换和恢复 cloudflared；
- cloudflared 生命周期进入 Installer rollback 范围；
- Windows Installer CI 为基础安装专门准备真实/fake cloudflared 与 Quick/Named Tunnel lifecycle。

Windows 离线 Setup 同样是强耦合：

- `packaging/windows/AgentDock.iss` 把 `cloudflared.exe` 作为 `dontcopy` payload 放入 Setup；
- 当时的 Windows Setup 构建脚本要求 cloudflared 必须存在，并检查 Authenticode；
- `packaging/windows/includes/code.iss` 会提取 `cloudflared.exe`，再通过 `-OfflineCloudflaredBinary` 交给 `install.ps1`；
- Setup 仍存在 Local / Quick / Named 连接选择和 Tunnel Token 输入。

这些都属于旧的“安装器负责公网能力”模型。

### 2.2 macOS 当前把 cloudflared 作为 App Bundle 强依赖

`packaging/macos/build-app.sh` 当前会：

- 为各架构准备 cloudflared payload；
- 合并成 universal binary；
- 写入 `AgentDock.app/Contents/Helpers/cloudflared`；
- 单独 codesign cloudflared；
- 再签整个 AgentDock.app。

`AppPaths.cloudflared` 也固定指向 `Contents/Helpers/cloudflared`。`InstallerRunner.validateBundledRuntime()` 会同时验证 AgentDock Core 与 cloudflared 必须存在、是普通文件、可执行，并实际执行 `cloudflared --version`。

所以当前即使用户永远不用 Cloudflare Tunnel，macOS App 仍必须携带并校验 cloudflared。

### 2.3 Tunnel Runtime 已经具备解耦基础

现有 `agentdock tunnel ...` 已经承担真实 Tunnel domain：

- `launch`
- `status`
- `start / stop / restart / regenerate`
- `configure`
- `autostart`

Windows `SetTunnelModeAsync()` 和 macOS `applyTunnel(...)` 最终也都通过 AgentDock 自己的 Tunnel 生命周期完成配置，不需要 UI 直接调用 cloudflared。

Windows `runtime.json` 中 `cloudflared_binary` 本身是 `omitempty`，Manifest 校验并没有要求 `tunnel_mode=none` 时 cloudflared 必须存在。真正需要 binary 的边界是在 Tunnel 启动时。

因此这次重构的核心不是重写 Tunnel，而是把“cloudflared 从哪里来、当前哪个版本可用、怎么更新/卸载”从 Installer/Bundle 路径中抽出来。

### 2.4 高级连接已经是正确的产品入口

PR #187 两端已经有“设置 → 高级连接”：

- 本地 MCP / 自定义端口；
- 访问凭据；
- 公网地址；
- 临时域名；
- 固定域名 / Cloudflare Tunnel Token。

这正是 Cloudflare Tunnel 可选组件的唯一 UI 入口。首次安装器不需要再维护第二套连接配置 UI。

## 3. 职责边界

长期只保留四层职责，不引入额外通用框架。

### 3.1 Installer

Installer 只负责：

- AgentDock Core；
- Tray / Native Control Panel；
- AgentDock 自己的 helper / shim / arbiter；
- 官方 Core Skills；
- AgentDock 自身服务、自启动与升级事务。

Installer 不负责：

- 下载 cloudflared；
- Cloudflare Token；
- Quick / Named Tunnel 选择；
- Cloudflare 网络 ready；
- cloudflared 版本更新；
- cloudflared 进程 rollback。

### 3.2 Tunnel domain

`agentdock tunnel ...` 继续拥有：

- `none / quick / named` 配置；
- Quick Tunnel URL；
- Named Tunnel Token 和 public origin；
- Tunnel 自启动策略；
- Tunnel supervisor；
- Core 与公网地址变化时的配置协调。

Tunnel domain 不再假定 cloudflared 位于固定安装路径。

### 3.3 Component subsystem

Component subsystem 第一阶段只管理 `cloudflared`，但数据模型允许独立版本、状态和 active pointer。

它负责：

- 查询组件状态；
- 下载固定版本；
- 校验完整性和平台签名；
- staging 到新版本目录；
- 原子切换 active pointer；
- 更新；
- 卸载；
- 旧安装迁移；
- 返回当前可执行文件的可信路径。

它不负责 Cloudflare Tunnel 的 Token、URL 或模式配置。

### 3.4 Native UI

Native UI 只负责：

- 展示 component 状态；
- 发起安装/更新/卸载；
- component ready 后展示并调用现有 Tunnel 配置动作；
- 显示错误和恢复操作。

UI 不直接拼 GitHub URL、不下载 binary、不维护 SHA-256 catalog、不自己实现解压/替换。

## 4. Component Store

组件不能写入已经签名的 macOS App Bundle，也不应该继续和 Windows stable `bin/` 混放。

建议路径：

### macOS

```text
~/Library/Application Support/AgentDock/components/
└── cloudflared/
    ├── active.json
    └── versions/
        └── <version>/
            └── cloudflared
```

### Windows

Windows runtime root 当前是 `%LOCALAPPDATA%\AgentDock`，建议：

```text
%LOCALAPPDATA%\AgentDock\components\
└── cloudflared\
    ├── active.json
    └── versions\
        └── <version>\
            └── cloudflared.exe
```

不要使用依赖平台 symlink 语义的 `current` 链接。使用原子写入的 `active.json`，例如：

```json
{
  "schema_version": 1,
  "component": "cloudflared",
  "version": "2026.x.y",
  "sha256": "..."
}
```

读取 active component 时必须：

1. 校验 component 名称和版本格式；
2. 解析到版本目录；
3. 确认目标是 component root 内的普通文件，不接受任意外部路径；
4. 校验可执行性；
5. 必要时检查记录的 digest / 平台签名；
6. 失败时返回“未安装 / 损坏”，不要静默回退到未知 PATH binary。

## 5. Component CLI 契约

具体命令名称实现前可以根据当前 CLI 风格微调，但需要有一个 AgentDock 自己拥有的稳定边界。建议：

```text
agentdock component status cloudflared --json
agentdock component install cloudflared
agentdock component update cloudflared
agentdock component uninstall cloudflared
```

要求：

- `status` 不产生网络请求和写入；
- `install` 幂等，已是目标版本时直接成功；
- `update` 只改变 component，不升级 AgentDock；
- `uninstall` 不能留下仍指向已删除 binary 的 active pointer；
- Tunnel 正在运行或配置为 quick/named 时，卸载必须显式先停用/切回 `none`，不能删掉正在运行的 dependency 后留下半状态；
- CLI 输出要支持 Native UI 稳定消费，优先结构化 JSON；
- 不允许 Native UI 复制 component 安装逻辑。

不要为了未来可能存在的其他 Provider 先定义复杂 interface / plugin SDK。当前只有一个真实实现时，保持具体类型和清楚调用链。

## 6. Tunnel 解析 cloudflared 的方式

目标调用链：

```text
agentdock tunnel launch
        ↓
读取 Tunnel 配置
        ↓
解析 active cloudflared component
        ↓
启动该版本 cloudflared
```

不再把 `runtime.json.cloudflared_binary` 当长期权威路径。

兼容期可以继续读取旧字段，但只能作为 legacy migration source，不能继续成为新安装写入的稳定 contract。

当模式为 `none`：

- 不要求 component 已安装；
- 不解析 cloudflared；
- Core/Tray/AgentDock 更新必须完全不受 component 状态影响。

当模式为 `quick` 或 `named` 且 component 缺失：

- 返回明确的 dependency-not-installed 错误；
- UI 映射成“Cloudflare Tunnel 未安装”；
- 不再提示“运行 Setup.exe 修复安装”，因为 Setup 不再拥有该 dependency。

## 7. 高级连接 UX

### 7.1 未安装

“设置 → 高级连接 → Cloudflare Tunnel”显示：

```text
Cloudflare Tunnel
用于自托管公网访问。NexusDock 远程连接无需安装。

状态    未安装
                                    [安装]
```

此时：

- 不展示 Quick / Named 详细表单；
- 不自动下载；
- 打开页面不会修改磁盘或网络状态。

### 7.2 安装中

显示明确进度和当前阶段，例如：

```text
正在安装 Cloudflare Tunnel…
下载 → 校验 → 安装
```

不要把任意 GitHub/cloudflared 原始 stderr 直接堆到主 UI；失败时显示可理解错误，详细信息进入日志/诊断。

### 7.3 已安装

安装完成后再显示当前高级连接能力：

- 临时公网地址；
- 固定域名；
- Tunnel Token；
- 状态；
- 当前 component 版本；
- 有更新时的更新动作；
- 卸载入口放在次要管理位置，不和“启动 Tunnel”并列成主要动作。

如果当前已经是 quick：保留“重新生成临时地址”。

如果当前已经是 named：保留已保存 Token 复用和固定 origin。

### 7.4 损坏 / 缺失

配置是 quick/named，但 active component 不存在或校验失败时：

```text
Cloudflare Tunnel
需要修复
                                    [重新安装]
```

配置、Token、Named URL 不应因为 binary 损坏而被删除。修复 component 后继续使用原配置。

## 8. 首次安装与 Setup 收敛

### 8.1 Windows

最终目标：

- `install.ps1` 在普通 AgentDock 安装中不调用 `Install-CloudflaredBinary`；
- Windows Setup 不再携带 `cloudflared.exe`；
- `build-windows-setup.ps1` 不要求 cloudflared 输入；
- Inno Setup 删除 Local / Quick / Named 页面和 Token 页面；
- 新安装固定以 `tunnel_mode=none` 初始化；
- AgentDock 安装成功不依赖 Cloudflare/GitHub 可达性；
- 主 Installer rollback 不再备份/恢复 cloudflared 进程和 binary。

历史 `MODE/TunnelMode/ServerUrl/TunnelToken` 等公开自动化参数如果仍有兼容承诺，不要为了本次重构粗暴删除。可以保留一个明确标记为 deprecated 的兼容入口，但它必须在 AgentDock 主安装成功后调用 component + tunnel CLI，不能重新把 Cloudflare 状态机塞回 Installer。需要先用测试和文档确认哪些属于真实公开 contract。

### 8.2 macOS

最终目标：

- Release 不再下载 cloudflared 来构建 App Bundle；
- `AgentDock.app/Contents/Helpers/cloudflared` 消失；
- `InstallerRunner.validateBundledRuntime()` 不再要求 cloudflared；
- `AppPaths.cloudflared` 不再指向 Bundle Helper；
- App Bundle 仍保留 `Contents/Helpers/agentdock`；
- `com.uvwt.agentdock.tunnel` 的 SMAppService 仍启动 `agentdock tunnel launch`；
- `agentdock tunnel launch` 再从外部 component store 解析 active cloudflared。

这不会修改已签名 `.app`，也不会把下载后的第三方 binary 写回 App Bundle。

macOS 首次设置不再要求用户选择 Local / Quick / Named。Cloudflare Tunnel 只在高级连接中启用。

## 9. Component 供应链与发布

运行时禁止直接跟踪 Cloudflare `latest`。AgentDock 不把 cloudflared 混入自身 GitHub Release，但会在 R2 的独立 component namespace 中维护经过上游校验的原始字节镜像，以消除客户端对 GitHub Release 可达性的硬依赖。

当前长期契约已经进一步收敛为：

1. 仓库中的 `internal/component/catalog-v1.json` 是审计过的 pinned metadata，记录明确版本、Cloudflare 官方固定 Release URL、artifact format 和下载物 SHA-256；
2. 独立 component publish workflow 从官方 Cloudflare 固定版本来源下载对应 artifact，并验证仓库 pinned SHA-256、版本与平台签名/信任；AgentDock Release CI 继续做同类 upstream gate；
3. cloudflared 不上传到 AgentDock GitHub Release；只有独立 component workflow 通过全部验证后的**原始上游字节**可进入 R2 component mirror；Release CI 只验证 mirror readiness；
4. R2 在 `components/cloudflared/<version>/` 下维护不可变镜像，同一 cloudflared 版本可供多个 AgentDock 版本共享，不按 AgentDock Release 复制；
5. `agentdock-component-catalog.json` 保留 Cloudflare 官方固定 `url`，并增加固定 `mirror_url`；两者都必须是版本化不可变路径，mirror prepare 不得改写 upstream URL；
6. 客户端优先 mirror，镜像网络失败时回退官方 upstream；任何成功响应都必须再次校验固定版本、format 和同一个 upstream SHA-256；
7. Windows 官方 artifact 是直接 binary；下载后继续执行 Authenticode 和版本验证；
8. macOS 官方 artifact 是 `.tgz`；客户端先验证 archive 的 pinned SHA-256，再在受控 staging 中只解出预期普通文件，拒绝 symlink、hardlink、path traversal 和额外条目，然后执行 codesign 与版本验证；
9. catalog 中的 SHA-256 表示**下载 artifact digest**；`active.json` 中的 SHA-256 表示**最终已安装 binary digest**，两者不能混用；
10. staging、平台信任、版本验证全部通过后才原子更新 `active.json`，失败时旧 active component 不受影响。

component catalog 不包含 Token、用户配置或机器状态。

## 10. 更新策略

AgentDock 与 cloudflared 更新彻底分离：

- AgentDock 更新不能因为 Cloudflare 网络失败而失败；
- AgentDock 更新不停止/替换 cloudflared，除非 Tunnel host 自身需要有序重启；
- cloudflared 更新采用新版本目录 + active pointer 原子切换；
- 正在运行的 cloudflared 不做原地覆盖；先完成新版本 staging，再由 Tunnel supervisor 在明确重启边界使用新 active 版本；
- component 更新失败继续运行旧版本；
- 不默认把“打开高级连接页面”作为更新触发器。

是否将安全更新设为自动策略属于后续产品决策；本轮先提供可靠、显式的安装/更新能力，不偷偷改变用户组件版本。

## 11. 旧安装迁移

迁移必须保护已经配置的公网访问，不能要求用户重新输入 Token 或重新配置域名。

### 11.1 Windows legacy source

旧路径通常为：

```text
%LOCALAPPDATA%\AgentDock\bin\cloudflared.exe
```

升级时：

1. 读取旧 `runtime.json`、`cloudflared-mode.txt`、Named URL、Token store；
2. 如果 mode=`none`，不要求为了迁移而安装 component；旧 binary 可以在明确清理阶段删除；
3. 如果 mode=`quick|named` 且 component store 尚未就绪，先把当前可信 binary 导入/stage 到 component store，或安装 catalog 中对应/受支持版本；
4. component 验证成功后再写 `active.json`；
5. Tunnel 使用新 component 成功后再清理旧 `bin/cloudflared.exe`；
6. 保留 `cloudflared-token.dpapi`、Named origin 和 mode 配置。

### 11.2 macOS legacy source

旧 binary 位于：

```text
AgentDock.app/Contents/Helpers/cloudflared
```

新版本 App 不再携带它，因此升级事务必须考虑“旧 App 被替换后 legacy source 消失”。

对于当前已配置 quick/named 的用户，必须在替换旧 App 前完成以下之一：

- 将旧已验证 helper 导入 component store；或
- 安装 catalog 中受支持版本并确认 component ready。

只有 component store 已形成可恢复状态，才允许完成会删除旧 bundled cloudflared 的更新 handoff。

mode=`none` 用户不需要为升级额外下载 cloudflared。

### 11.3 配置兼容

本轮优先保持以下用户状态格式兼容：

- Tunnel mode；
- Quick URL ready 文件；
- Named server URL；
- Windows DPAPI Token；
- macOS Token store；
- Tunnel autostart 意图。

不要把“component migration”和“用户 Tunnel 配置格式重写”绑成同一次大迁移，除非现有格式明确阻碍新架构。

## 12. `runtime.json` 演进

当前 Windows `runtime.json` 含：

- `cloudflared_binary`
- `cloudflared_launcher`
- `cloudflared_startup_value_name`
- `tunnel_mode`

最终：

- `tunnel_mode` 仍属于 Tunnel domain，可以继续保留或由统一配置源投影；
- `cloudflared_binary` 不再是新安装权威路径；
- `cloudflared_launcher` / legacy Run value 只保留迁移所需的兼容读取；
- component active path 由 component store 自己解析。

Schema 迁移要兼容旧 manifest。不要为了删除三个字段立刻 bump 一个破坏性 schema；优先做到“旧字段可读、新安装不再写、迁移完成后逐步删除”。

## 13. CI 重构

主 Installer CI 与 Tunnel Component CI 要拆开。

### 13.1 Installer Gate

必须验证：

- Windows/macOS 基础安装以及 Linux 本地模式不依赖 cloudflared；
- Core/Tray/Skills/更新/卸载正常；
- `tunnel_mode=none` 下没有 cloudflared 网络请求；
- Cloudflare/GitHub 不可用时基础安装仍成功；
- 主安装产物不再包含 cloudflared payload。

### 13.2 Component Gate

独立验证：

- catalog 解析；
- 下载失败；
- digest 不匹配；
- 平台签名失败；
- staging + active pointer 原子切换；
- reinstall / update / uninstall；
- 旧版本回退不受损；
- Windows/macOS/Linux 权限和路径安全；
- legacy import。

### 13.3 Tunnel Lifecycle Gate

Quick / Named Tunnel lifecycle 继续测试，但不再作为“AgentDock 基础安装必须携带 cloudflared”的证明。

使用 fake cloudflared 时应通过 component store 注入测试组件，不重新给 Installer 增加 `OfflineCloudflaredBinary` 强耦合。

至少验证：

- component 未安装 → 明确 dependency error；
- component installed → quick ready；
- regenerate；
- named token；
- restart/autostart；
- component 损坏 → repair 状态；
- Tunnel 停用后 component 可安全卸载。

## 14. 安全边界

必须保持：

- 不从 PATH 静默采用未知 `cloudflared`；
- 不执行 component root 外任意路径；
- 下载必须 HTTPS + pinned catalog digest；
- Windows Authenticode 失败拒绝安装；
- Token 不写入 catalog、日志、命令行或普通 JSON；
- component 解压防 zip-slip/path traversal；
- staging 目录和 active pointer 使用原子写入；
- component 版本目录不能被符号链接/重解析点绕出 component root；
- UI 错误信息不泄露 Token；
- 卸载 component 不删除用户 Tunnel 配置，除非用户明确要求重置配置。

## 15. 不做的事情

本轮明确不做：

- 通用第三方插件市场；
- 任意 Provider SDK；
- Tailscale/FRP 等第二 Provider；
- 自动发现系统 PATH 中的 cloudflared；
- 进入高级连接页面自动安装；
- 因为 component 缺失阻塞 NexusDock 配对或 Core 启动；
- 把 cloudflared component 生命周期重新塞回 PowerShell / Inno / Swift UI / WinUI 各自实现。

## 16. 推荐实施顺序

这是一条最终架构的实现顺序，不是多个长期阶段。

### Step 1：Component store 与 CLI

- 定义 component metadata / active pointer；
- 实现 cloudflared status/install/update/uninstall；
- 实现 catalog 校验与原子切换；
- 单元测试路径、digest、更新失败、卸载状态。

### Step 2：Tunnel Runtime 切换 dependency resolver

- Windows/macOS Tunnel host 与 Linux CLI Installer Engine 都从 component store 解析 cloudflared；
- mode=`none` 完全不需要 component；
- 缺失/损坏返回结构化可识别错误；
- 保留 legacy path 迁移入口，而不是长期 fallback。

### Step 3：Native UI

- 两端高级连接展示 component 状态；
- 未安装只显示安装；
- 安装完成后再显示 Quick/Named 配置；
- repair/update/uninstall 行为补齐；
- 不复制下载逻辑。

### Step 4：Windows Installer 去耦

- 删除基础 install 的 cloudflared 获取/替换/rollback；
- 删除 Setup 内 cloudflared payload；
- 删除首次安装 Cloudflare 页面；
- 保留必要 legacy migration adapter；
- 基础 Installer 构建与安装 E2E 不再依赖 cloudflared payload；独立 component / Tunnel lifecycle CI 仍验证官方 pinned artifact 与运行时行为。

### Step 5：macOS Bundle 去耦

- Release 不再生成 bundled cloudflared；
- AppPaths / runtime loader 不再写死 Bundle cloudflared；
- 保持 AgentDock tunnel SMAppService host；
- 完成旧 Bundle helper → component store 的升级迁移；
- 重新验证签名、更新 handoff 和首次安装。

### Step 6：Release / CI / 文档收口

- component catalog 纳入 Release；
- R2 在独立 `components/cloudflared/<version>/` namespace 中同步经过 CI 验证的原始 cloudflared artifact 与 LICENSE；不把第三方二进制放进 AgentDock Release prefix；
- Installer / Component / Tunnel lifecycle CI 分层；
- 更新用户文档；
- 清理不再使用的 cloudflared installer contract。

## 17. 验收标准

只有同时满足以下条件才算完成：

1. 全新 Windows AgentDock 安装包不含 cloudflared，离线/在线安装都不会访问 Cloudflare 下载源。
2. 全新 macOS AgentDock.app 不含 `Contents/Helpers/cloudflared`，codesign / bundle 验证通过。
3. 两端不安装 cloudflared 时 Core、Tray、NexusDock 配对和普通使用完全正常。
4. 两端高级连接能识别“未安装”，且只有用户点击安装才发生网络下载。
5. 安装 component 后 Quick / Named Tunnel 都能真实工作。
6. AgentDock 更新与 cloudflared 更新互不阻塞。
7. 已有 Quick / Named 用户升级后不丢 Token、URL、mode，也不要求重新配置。
8. component 更新失败时旧版本仍可运行；active pointer 不进入半状态。
9. 主 Installer CI 不再执行 cloudflared compatibility payload 下载。
10. Quick / Named lifecycle 有独立 component/tunnel 测试。
11. Windows PowerShell 5.1、WinUI Release、macOS Swift typecheck/签名与 Go 全量相关测试通过。
12. Mini 和 TianYi 至少各做一次真实 UI + Tunnel 行为验证；不要只依赖契约测试。

## 18. Stacked PR 约定

本开发分支直接基于 PR #187：

```text
base PR:   #187
base ref:  refactor/desktop-native-connection-ux-20261004
worktree:  /Users/xx/Project/worktrees/agentdock-optional-cloudflared-component-20261004
branch:    refactor/optional-cloudflared-component-20261004
```

在 PR #187 尚未合并时，新 PR 必须以 `refactor/desktop-native-connection-ux-20261004` 为 base，只展示本文档和可选 cloudflared component 的增量。

如果 #187 在开发期间继续推进：

1. 先 fetch 最新 #187 head；
2. 在当前开发分支保持可恢复的前提下 rebase/merge 最新 base；
3. 重新跑相关测试；
4. 不要通过 cherry-pick 把 #187 的提交复制成新 PR 的独立历史。

如果 #187 已经合并到 `main`：

- 更新开发分支到最新 `main`；
- PR base 改成 `main`；
- 确认 diff 中不重复包含 #187 已合并内容。

## 19. 开发纪律

- 开始实现前重新读取 PR #187 最新真实状态、本文档和 `personal-dev-guard`；
- 先检查现状，再修改；每个跨平台边界修改后真实验证；
- 不用延长 timeout、跳过测试或把 Quick Tunnel CI 从主流程删除来掩盖旧问题；CI 的重构必须建立在职责已经真正解耦之后；
- 关键兼容代码写中文注释，说明来源、退出条件和为什么不能直接删除；
- 不为了测试定义不合理的生产接口；
- 不在 WinUI/SwiftUI 各写一份 downloader；
- 不引入大而空泛的 Component/Provider 框架；实现应围绕当前真实 cloudflared 生命周期保持简单直接；
- 完成后做代码审查，确认没有 Installer、Runtime、UI、Release、CI 中的半解耦残留。
