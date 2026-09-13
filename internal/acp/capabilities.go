package acp

import "strings"

func (p *agentProcess) supportsLoadSession() bool {
	if p == nil {
		return false
	}
	value, _ := p.initialize.AgentCapabilities["loadSession"].(bool)
	return value
}

func (p *agentProcess) supportsSessionCapability(name string) bool {
	if p == nil || strings.TrimSpace(name) == "" {
		return false
	}
	session, ok := p.initialize.AgentCapabilities["sessionCapabilities"].(map[string]any)
	if !ok {
		return false
	}
	value, exists := session[name]
	if !exists || value == nil {
		return false
	}
	if enabled, ok := value.(bool); ok {
		return enabled
	}
	_, object := value.(map[string]any)
	return object
}

func (p *agentProcess) supportsPromptCapability(name string) bool {
	if p == nil || strings.TrimSpace(name) == "" {
		return false
	}
	prompt, ok := p.initialize.AgentCapabilities["promptCapabilities"].(map[string]any)
	if !ok {
		return false
	}
	enabled, _ := prompt[name].(bool)
	return enabled
}

func (p *agentProcess) supportsSteering() bool {
	if p == nil {
		return false
	}
	steering, ok := p.initialize.Meta["steering"].(map[string]any)
	if !ok {
		return false
	}
	supported, _ := steering["supported"].(bool)
	return supported
}

func capabilityError(capability string) error {
	return newError(
		"ACP_CAPABILITY_UNSUPPORTED",
		"ACP agent does not advertise the required capability",
		false,
		map[string]any{"capability": capability},
		nil,
	)
}
