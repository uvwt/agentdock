package tools

import (
	"context"
	"strings"

	"github.com/uvwt/agentdock/internal/device"
	"github.com/uvwt/agentdock/internal/tunnel"
)

func (r *Runtime) RuntimeTunnelStatus() Result {
	if r.tunnel == nil {
		return Result{"ok": false, "error": "tunnel manager unavailable"}
	}
	st := r.tunnel.Status()
	cfg, _ := r.tunnel.LoadConfig()
	return Result{"ok": true, "source": runtimeAPISource, "tunnel": st, "config": cfg}
}

// TunnelStartOptions configures how the MCP endpoint is exposed.
type TunnelStartOptions struct {
	Mode           string
	CustomURL      string
	TunnelToken    string
	TunnelName     string
	CloudflaredBin string
	ClearCustomURL bool
}

func (r *Runtime) RuntimeTunnelStart(ctx context.Context, mode string) (Result, error) {
	return r.RuntimeTunnelStartOpts(ctx, TunnelStartOptions{Mode: mode})
}

func (r *Runtime) RuntimeTunnelStartOpts(ctx context.Context, opts TunnelStartOptions) (Result, error) {
	if r.tunnel == nil {
		return nil, toolError("TUNNEL_UNAVAILABLE", "tunnel manager is not initialized", "runtime")
	}
	cfg, err := r.tunnel.LoadConfig()
	if err != nil {
		return nil, err
	}
	if mode := strings.ToLower(strings.TrimSpace(opts.Mode)); mode != "" {
		cfg.Mode = tunnel.Mode(mode)
	}
	if opts.ClearCustomURL {
		cfg.CustomURL = ""
	} else if u := strings.TrimSpace(opts.CustomURL); u != "" {
		cfg.CustomURL = strings.TrimRight(u, "/")
	}
	if tok := strings.TrimSpace(opts.TunnelToken); tok != "" {
		cfg.TunnelToken = tok
	}
	if name := strings.TrimSpace(opts.TunnelName); name != "" {
		cfg.TunnelName = name
	}
	if bin := strings.TrimSpace(opts.CloudflaredBin); bin != "" {
		cfg.CloudflaredBin = bin
	}
	if cfg.LocalPort == 0 {
		cfg.LocalPort = r.cfg.Port
	}
	if cfg.LocalHost == "" {
		cfg.LocalHost = r.cfg.Host
		if cfg.LocalHost == "" || cfg.LocalHost == "0.0.0.0" {
			cfg.LocalHost = "127.0.0.1"
		}
	}
	if cfg.Mode == tunnel.ModeCustom && strings.TrimSpace(cfg.CustomURL) == "" {
		return nil, toolErrorDetails("VALIDATION_ERROR", "custom_url is required for custom mode", "validation", map[string]any{"field": "custom_url"})
	}
	if err := r.tunnel.SaveConfig(cfg); err != nil {
		return nil, err
	}
	st, err := r.tunnel.Start(ctx)
	if err != nil {
		return Result{"ok": false, "tunnel": st, "config": cfg, "error": err.Error()}, err
	}
	return Result{"ok": true, "source": runtimeAPISource, "tunnel": st, "config": cfg}, nil
}

func (r *Runtime) RuntimeTunnelStop() (Result, error) {
	if r.tunnel == nil {
		return nil, toolError("TUNNEL_UNAVAILABLE", "tunnel manager is not initialized", "runtime")
	}
	st, err := r.tunnel.Stop()
	if err != nil {
		return nil, err
	}
	return Result{"ok": true, "source": runtimeAPISource, "tunnel": st}, nil
}

func (r *Runtime) RuntimeDevices() (Result, error) {
	if r.devices == nil {
		return nil, toolError("DEVICE_REGISTRY_UNAVAILABLE", "device registry is not initialized", "runtime")
	}
	items, err := r.devices.ListDevices()
	if err != nil {
		return nil, err
	}
	return Result{"ok": true, "source": runtimeAPISource, "devices": items, "count": len(items)}, nil
}

func (r *Runtime) RuntimeDeviceUpsert(d device.Device) (Result, error) {
	if r.devices == nil {
		return nil, toolError("DEVICE_REGISTRY_UNAVAILABLE", "device registry is not initialized", "runtime")
	}
	out, err := r.devices.UpsertDevice(d)
	if err != nil {
		return nil, toolErrorDetails("VALIDATION_ERROR", err.Error(), "validation", map[string]any{})
	}
	return Result{"ok": true, "source": runtimeAPISource, "device": out}, nil
}

func (r *Runtime) RuntimeHandoffs(goalID string) (Result, error) {
	if r.devices == nil {
		return nil, toolError("DEVICE_REGISTRY_UNAVAILABLE", "device registry is not initialized", "runtime")
	}
	items, err := r.devices.ListHandoffs(goalID)
	if err != nil {
		return nil, err
	}
	return Result{"ok": true, "source": runtimeAPISource, "handoffs": items, "count": len(items)}, nil
}

func (r *Runtime) RuntimeCreateHandoff(goalID, from, to, role, summary string) (Result, error) {
	if r.devices == nil {
		return nil, toolError("DEVICE_REGISTRY_UNAVAILABLE", "device registry is not initialized", "runtime")
	}
	h, err := r.devices.CreateHandoff(goalID, from, to, role, summary)
	if err != nil {
		return nil, toolErrorDetails("VALIDATION_ERROR", err.Error(), "validation", map[string]any{})
	}
	return Result{"ok": true, "source": runtimeAPISource, "handoff": h}, nil
}
