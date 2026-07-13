---
name: baidu-netdisk
description: Use this skill when managing Baidu Netdisk files through the bdpan CLI inside the application data scope.
version: 0.3.5
---

# Baidu Netdisk

通过官方 `bdpan` CLI 管理"我的应用数据/bdpan/"范围内的文件。

支持：状态、列表、上传、下载、分享链接转存、分享、移动、复制、重命名、创建目录、受控删除和退出登录。

- **move**: 将文件/文件夹移动到目标目录
- **copy**: 将文件/文件夹复制到目标目录
- **rename**: 重命名文件/文件夹
- **mkdir**: 创建远程目录

删除操作要求 `confirmed=true`，仅允许相对路径，禁止删除应用根目录。

## 辅助脚本执行

Skill 本体是本说明文档。确需调用包内辅助脚本时，在 Skill 包根目录使用相对路径执行；运行宿主负责切换到包根目录并把所需变量注入当前子进程。

```bash
printf '%s' '{"skill_action":"<动作>"}' | python3 run.py
```

输入必须是 JSON 对象。写操作仍按本文档中的确认规则执行。

| 动作 | 用途 |
|---|---|
| `download` | Download a remote file/folder or Baidu share link to local path. |
| `delete` | Delete one or more remote files/folders under 我的应用数据/bdpan/. Requires confirmed=true. Root deletion is forbidden. |
| `logout` | Logout bdpan local authorization. Requires confirmed=true. |
| `ls` | List a remote path under the bdpan application data scope. |
| `share` | Create share link for one or more remote paths. |
| `status` | Check bdpan CLI path, version, and login status. |
| `transfer` | Transfer a Baidu Netdisk share link into application data scope without downloading locally. |
| `upload` | Upload a local file or directory to Baidu Netdisk application data scope. |
| `move` | Move a remote file/folder to a destination directory under 我的应用数据/bdpan/. |
| `copy` | Copy a remote file/folder to a destination directory under 我的应用数据/bdpan/. |
| `rename` | Rename a remote file/folder under 我的应用数据/bdpan/. |
| `mkdir` | Create a remote directory under 我的应用数据/bdpan/. |
