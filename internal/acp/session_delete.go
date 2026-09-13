package acp

import (
	"context"
	"strings"
)

// DeleteRemoteSession 删除 Adapter 原生会话。若该 remote session 已经被 AgentDock
// 管理，则复用 DeleteSession 保证本地映射与持久化记录同步清理；否则只调用标准
// session/delete。该方法绝不把“Adapter 不支持 delete”静默降级为本地解绑。
func (m *Manager) DeleteRemoteSession(ctx context.Context, remoteSessionID string) error {
	remoteSessionID = strings.TrimSpace(remoteSessionID)
	if remoteSessionID == "" {
		return newError("ACP_SESSION_ID_INVALID", "ACP remote session id is required", false, nil, nil)
	}
	if record, ok := m.ManagedSessionForRemote(remoteSessionID); ok {
		return m.DeleteSession(ctx, record.ID)
	}
	process, err := m.ensureProcess(ctx)
	if err != nil {
		return err
	}
	if !process.supportsSessionCapability("delete") {
		return capabilityError("sessionCapabilities.delete")
	}
	if err := process.connection.Request(ctx, "session/delete", map[string]any{"sessionId": remoteSessionID}, nil); err != nil {
		return process.wrapError("delete ACP remote session", err)
	}
	return nil
}
