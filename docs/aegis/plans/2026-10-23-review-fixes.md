# 代码审查问题修复计划

## 目标与基线

修复 2026-10-23 审查确认的 11 项问题。基线为 README.zh-CN.md、docs/runtime-access-and-recovery.md、docs/desktop-native-ui-rebuild.md 与现有实现；只恢复已承诺的行为，不新增配置格式、运行平台或安装路径规则。

架构保持 Go 核心、WinUI/SwiftUI 原生界面及既有事务日志的职责。ArchitectureReviewRequired: yes。共享行为由原有生产者修复，删除错误的处理分支，不新增兼容适配层。

## 原子任务与验证

1. `internal/installer/payload.go`：已有 generation 使用 journal.Snapshot；回归已有目录完整恢复与新目录失败清理。
2. `internal/publicartifacts/files.go`：归档头保留符号链接目标，不跟随目录链接；回归文件及目录链接的 tar 条目。
3. `internal/tool/file/search.go`：文件搜索使用父目录作为 cwd，查询通过 -e 传递；回归单文件和连字符查询，并比较结果路径。
4. `internal/tool/command/{command.go,session/session.go}`：初始输入在 Session 中串行异步写入，前台等待、请求取消与 async 返回不受管道阻塞；保留 stdin 错误、输入顺序、EOF、进程总超时和进程树清理。回归 async/auto/sync 取消、大输入及完整读取。
5. `desktop/windows/shared/`：配对与身份读取统一消费 manifest 数据目录，保留环境变量/默认目录兼容；隔离测试目录优先级。
6. `desktop/windows/winui/`：内容可收缩；开关具有语义名称；展开状态按调用 ID 保留；自定义 ACP ID 使用有长度限制的 ASCII 标识符。验证真实 C# 逻辑和原生构建。
7. `desktop/macos/`：表单初始化与后台轮询分离，保存成功后按实际配置刷新；ACP ID 与校验一致；补可访问名称。验证现有 Swift 测试及可运行的静态集成检查。
8. 同步 README 与运行恢复文档中的当前行为，完成变更审阅及编码检查。

先运行新增回归观察失败，再实施最小修复，最后运行定向测试、`go test ./... -count=1 -timeout=3m`、`go vet ./...`、Windows 原生构建（环境支持时）。不运行实际安装、配对或用户服务操作。

## 兼容、风险与回退

保留公开工具参数、命令生命周期、旧 manifest 默认目录和现有 UI 操作入口。重点检查初始输入与后续写入顺序、异步错误可见性、旧版本目录恢复、保存后状态刷新。Linux/macOS 服务和原生 GUI 真机检查取决于环境；无法执行的覆盖需明确记录。改动可通过 Git diff 单独审阅并按文件回退；不修改用户现有配置与凭据。

## 旧逻辑退出

替换已有目录的 NoteCreated、查询裸参数、文件 cwd、前台同步初始写入、固定内容宽度、无标签控件和轮询覆盖草稿路径。继续由原事务日志、Session、manifest 和页面状态维护各自事实，不引入第二份配置或回滚来源。

## 实施与验证记录

上述 8 组实施任务已落地，覆盖审查中的 11 项问题。新增回归先确认原实现失败，再确认修复通过；Windows Profile ID 与数据目录测试编译实际 C# 实现执行。命令输入生命周期经独立交叉复核，无新增阻塞发现。

- `go test -p 2 ./... -count=1 -timeout=3m`：退出码 0，全量通过。
- `go vet ./...`：退出码 0。
- `git diff --check`：通过；新增中文文件未发现替换字符乱码。
- WinUI x64 原生构建：使用校验 SHA512 后解压的临时 .NET SDK 8.0.413，执行 `dotnet build desktop/windows/winui/AgentDock.WinUI.csproj -p:Platform=x64 -p:RuntimeIdentifier=win-x64 -p:BaseIntermediateOutputPath=<临时目录>/obj/ -p:BaseOutputPath=<临时目录>/bin/ --verbosity minimal`；退出码 0，0 警告、0 错误。构建产物和 NuGet 包位于系统临时目录，未安装到用户应用目录。
- 默认包并行度的两轮全量测试均在未修改的 `TestStopBinaryProcessesTerminatesProcessThatReappearsDuringStop` 失败，分别为替代进程启动超时和替代进程仍存活；单独连续 3 次及降低包并行度的全量运行通过。保留此负载相关波动，未修改该测试或其生产逻辑。
- macOS Swift 编译、GUI 和读屏真机验证未执行；当前主机为 Windows。macOS 本地化检查及跨平台桌面集成检查通过。置信度 B，原生界面仍需真机验收。

复杂度变化：现有超 800 行文件为 Swift 控制面板 2229 行、Windows RuntimeService 2028 行、SettingsPage 1351 行，无文件新跨阈值；最大文件本次净增 21 行。新增职责分别留在 Session 输入、manifest 路径解析和页面草稿状态中；输入锁与状态分支增加了并发维护成本，已有顺序、取消、EOF 和错误回归约束。未扩展模块职责，暂不拆分既有大型页面。

架构对齐：符合原生桌面界面、Go 核心和事务日志边界；数据目录继续以安装清单为事实来源，旧清单保留环境变量与默认目录规则。ADR 回填跳过：本次恢复既有行为基线，公开参数和配置格式不变；README 与运行恢复文档已同步。错误分支已替换，不保留双实现；上述旧清单兼容规则继续有效。

原生界面验收步骤：Windows 将窗口缩至 900 DIP，逐页检查内容和操作按钮；展开活动详情，等待新调用或点击更多记录后确认仍展开；使用 Narrator 逐个聚焦能力、启动与外观控件，确认读出语义名称。macOS 分别编辑连接服务地址、能力配置、端口与固定域名，等待超过 15 秒确认草稿保留；分别验证保存成功和失败，再用 VoiceOver 核对新增标签。两端添加中文和同名 ACP 配置，保存后确认可重新加载。
