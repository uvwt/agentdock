package tools

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/artifactrelay"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/nexusclient"
)

func (r *Runtime) artifactSend(ctx context.Context, args map[string]any) (Result, error) {
	targets := stringSliceArg(args, "target_devices")
	if len(targets) == 0 {
		return nil, toolError("ARTIFACT_TARGET_REQUIRED", "target_devices must contain at least one device id", "validation")
	}
	stateDir, err := config.NexusStateDir(r.cfg)
	if err != nil {
		return nil, err
	}
	source, cleanup, err := resolveArtifactSendSource(
		ctx,
		r.ws,
		args["file"],
		strings.TrimSpace(stringArg(args, "path", "")),
		filepath.Join(stateDir, "artifacts", "connector-input"),
		newConnectorFileHTTPClient(),
	)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	client, err := nexusclient.New(nexusclient.Config{BaseURL: r.cfg.NexusEndpoint, RequestTimeout: 30 * time.Second, PollTimeout: 35 * time.Second, UserAgent: "agentdock/" + config.Version})
	if err != nil {
		return nil, err
	}
	stateStore, err := nexusclient.OpenStateStore(filepath.Join(stateDir, "device"))
	if err != nil {
		return nil, err
	}
	credentials := func() (artifactrelay.DeviceCredentials, error) {
		state, err := stateStore.Load()
		if err != nil {
			return artifactrelay.DeviceCredentials{}, err
		}
		if !state.Valid(time.Now()) {
			return artifactrelay.DeviceCredentials{}, errors.New("valid Nexus device enrollment is required")
		}
		return artifactrelay.DeviceCredentials{DeviceID: state.DeviceID, DeviceToken: state.DeviceToken}, nil
	}
	sender, err := artifactrelay.NewSender(artifactrelay.SenderConfig{Client: client, Credentials: credentials, TempRoot: filepath.Join(stateDir, "artifacts", "outbox")})
	if err != nil {
		return nil, err
	}
	result, err := sender.Send(ctx, artifactrelay.SendRequest{
		SourcePath: source, TargetDeviceIDs: targets,
		Dispatch: boolArg(args, "dispatch", true), RetentionSeconds: int64(intArg(args, "retention_seconds", 86400)),
		DeleteAfterAllDelivered: boolArg(args, "delete_after_all_delivered", false),
		ConflictPolicy:          stringArg(args, "conflict_policy", "reject"), Extract: boolArg(args, "extract", false),
		LogicalTarget: stringArg(args, "logical_target", "inbox"),
	})
	if err != nil {
		return nil, err
	}
	return Result{
		"ok": true, "artifact": result.Artifact, "deliveries": result.Deliveries,
		"source": result.Source, "archive": result.Archive, "encrypted": result.Encrypted,
	}, nil
}
