#!/usr/bin/env python3
"""交互式配置 Vaultwarden，并把临时 BW_SESSION 放入 macOS 钥匙串。"""
import json
import os
import shutil
import subprocess
import sys
from urllib.parse import urlparse

SERVICE = "agentdock-vaultwarden-cli"
ACCOUNT = "bw-session"
SECURITY = "/usr/bin/security"


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, text=True, **kwargs)


def valid_server(value):
    parsed = urlparse(value)
    if parsed.scheme == "https" and parsed.hostname:
        return True
    return parsed.scheme == "http" and parsed.hostname in {"localhost", "127.0.0.1", "::1"}


def main():
    bw = shutil.which("bw") or "/opt/homebrew/bin/bw"
    if not os.path.isfile(bw):
        raise SystemExit("未找到官方 bw CLI")
    status_raw = subprocess.run([bw, "status"], text=True, capture_output=True, check=False).stdout
    try:
        status = json.loads(status_raw)
    except Exception:
        status = {}
    current = status.get("serverUrl") or ""
    prompt = f"Vaultwarden 地址 [{current}]: " if current else "Vaultwarden 地址（必须为 HTTPS）: "
    server = input(prompt).strip() or current
    if not valid_server(server):
        raise SystemExit("地址无效：远程 Vaultwarden 必须使用 HTTPS")
    if server.rstrip("/") != current.rstrip("/"):
        run([bw, "config", "server", server.rstrip("/")])
        status = {}
    status_name = status.get("status")
    if status_name == "unauthenticated" or not status_name:
        print("\n接下来由官方 bw CLI 直接提示输入邮箱、主密码和两步验证码。输入不会经过 AgentDock。\n")
        run([bw, "login"])
    print("\n正在解锁密码库。主密码由官方 bw CLI 在本终端读取。\n")
    unlocked = subprocess.run([bw, "unlock", "--raw"], text=True, capture_output=True, check=False)
    if unlocked.returncode != 0:
        sys.stderr.write(unlocked.stderr)
        raise SystemExit("解锁失败")
    session = unlocked.stdout.rstrip("\r\n")
    if not session:
        raise SystemExit("官方 bw CLI 未返回会话密钥")
    subprocess.run([SECURITY, "delete-generic-password", "-s", SERVICE, "-a", ACCOUNT], capture_output=True, check=False)
    # 会话密钥只短暂存在于本进程内存和 macOS 钥匙串，不写入文件。
    stored = subprocess.run(
        [SECURITY, "add-generic-password", "-U", "-s", SERVICE, "-a", ACCOUNT, "-w", session],
        text=True,
        capture_output=True,
        check=False,
    )
    session = ""
    if stored.returncode != 0:
        sys.stderr.write(stored.stderr)
        raise SystemExit("写入 macOS 钥匙串失败")
    print("\n配置完成：会话已存入 macOS 钥匙串。现在可以直接调用 vaultwarden-cli Skill。")


if __name__ == "__main__":
    main()
