package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uvwt/agentdock/internal/fs/atomicfile"
)

func writeSystemdUnit(path, serviceName, serviceUser, serviceGroup, installRoot, envFile, runtimeRoot string) error {
	content := fmt.Sprintf(`[Unit]
Description=AgentDock MCP server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
WorkingDirectory=%s
EnvironmentFile=%s
ExecStart=%s service launch-core --runtime-root %s
Restart=on-failure
RestartSec=3

[Install]
WantedBy=multi-user.target
`, serviceUser, serviceGroup, installRoot, envFile, filepath.Join(installRoot, "bin", "agentdock"), runtimeRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(content), 0o644)
}

func writeSystemdTunnelUnit(path, serviceUser, serviceGroup, dataDir, envFile, binary, runtimeRoot string) error {
	content := fmt.Sprintf(`[Unit]
Description=AgentDock Cloudflare Tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=%s
Group=%s
WorkingDirectory=%s
EnvironmentFile=%s
ExecStart=%s tunnel launch --runtime-root %s
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, serviceUser, serviceGroup, dataDir, envFile, binary, runtimeRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(content), 0o644)
}

func writeOpenRCService(path, serviceName, serviceUser, serviceGroup, installRoot, envFile, runtimeRoot string) error {
	binary := filepath.Join(installRoot, "bin", "agentdock")
	content := fmt.Sprintf(`#!/sbin/openrc-run
name="AgentDock MCP server"
description="AgentDock MCP server"
command="%s"
command_args="service launch-core --runtime-root %s"
command_user="%s:%s"
directory="%s"
pidfile="/run/%s.pid"
command_background="yes"
log_dir="/var/log/%s"
output_log="/dev/null"
error_log="/dev/null"

agentdock_env_file="%s"

start_pre() {
  checkpath -d -m 0750 -o "%s:%s" "$log_dir"
  if [ -r "$agentdock_env_file" ]; then
    set -a
    . "$agentdock_env_file"
    set +a
  else
    eerror "env file not readable: $agentdock_env_file"
    return 1
  fi
}

depend() {
  need net
  after firewall
}
`, binary, runtimeRoot, serviceUser, serviceGroup, installRoot, serviceName, serviceName, envFile, serviceUser, serviceGroup)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(content), 0o755)
}

func writeOpenRCTunnelService(path, serviceName, serviceUser, serviceGroup, dataDir, envFile, binary, runtimeRoot string) error {
	content := fmt.Sprintf(`#!/sbin/openrc-run
name="AgentDock Cloudflare Tunnel"
description="AgentDock Cloudflare Tunnel"
command="%s"
command_args="tunnel launch --runtime-root %s"
command_user="%s:%s"
directory="%s"
pidfile="/run/%s.pid"
command_background="yes"
log_dir="/var/log/%s"
output_log="/dev/null"
error_log="/dev/null"

agentdock_env_file="%s"

start_pre() {
  checkpath -d -m 0750 -o "%s:%s" "$log_dir"
  if [ -r "$agentdock_env_file" ]; then
    set -a
    . "$agentdock_env_file"
    set +a
  else
    eerror "env file not readable: $agentdock_env_file"
    return 1
  fi
}

depend() {
  need net
  after firewall
}
`, binary, runtimeRoot, serviceUser, serviceGroup, dataDir, serviceName, serviceName, envFile, serviceUser, serviceGroup)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(content), 0o755)
}

func writeTunnelLaunchAgent(path, binary, runtimeRoot, workDir string) error {
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>tunnel</string>
    <string>launch</string>
    <string>--runtime-root</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ThrottleInterval</key>
  <integer>5</integer>
  <key>StandardOutPath</key>
  <string>/dev/null</string>
  <key>StandardErrorPath</key>
  <string>/dev/null</string>
</dict>
</plist>
`, darwinCLITunnelLabel, xmlEscape(binary), xmlEscape(runtimeRoot), xmlEscape(workDir))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(content), 0o600)
}

func writeLaunchAgent(path, binary, runtimeRoot, workDir string) error {
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>service</string>
    <string>launch-core</string>
    <string>--runtime-root</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>/dev/null</string>
  <key>StandardErrorPath</key>
  <string>/dev/null</string>
</dict>
</plist>
`, darwinCLICoreLabel, xmlEscape(binary), xmlEscape(runtimeRoot), xmlEscape(workDir))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.Write(path, []byte(content), 0o600)
}

func xmlEscape(value string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	).Replace(value)
}
