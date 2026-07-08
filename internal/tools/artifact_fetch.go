package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/artifactrelay"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/nexusclient"
)

func (r *Runtime) artifactFetchCreate(ctx context.Context, args map[string]any) (Result, error) {
	requester, _, err := r.artifactFetchRequester()
	if err != nil {
		return nil, err
	}
	deviceID := strings.TrimSpace(stringArg(args, "source_device_id", ""))
	sourcePath := strings.TrimSpace(stringArg(args, "source_path", ""))
	if deviceID == "" || sourcePath == "" {
		return nil, toolError("ARTIFACT_FETCH_INPUT_REQUIRED", "source_device_id and source_path are required", "validation")
	}
	job, err := requester.Create(ctx, artifactrelay.FetchCreateInput{
		SourceDeviceID: deviceID, SourcePath: sourcePath, Archive: boolArg(args, "archive", false),
		RetentionSeconds: int64(intArg(args, "retention_seconds", 86400)),
	})
	if err != nil {
		return nil, err
	}
	return Result{"ok": true, "fetch": job}, nil
}

func (r *Runtime) artifactFetchStatus(ctx context.Context, args map[string]any) (Result, error) {
	requester, _, err := r.artifactFetchRequester()
	if err != nil {
		return nil, err
	}
	fetchID := strings.TrimSpace(stringArg(args, "fetch_id", ""))
	if fetchID == "" {
		return nil, toolError("ARTIFACT_FETCH_ID_REQUIRED", "fetch_id is required", "validation")
	}
	job, err := requester.Status(ctx, fetchID)
	if err != nil {
		return nil, err
	}
	return Result{"ok": true, "fetch": job}, nil
}

func (r *Runtime) artifactFetchDownload(ctx context.Context, args map[string]any) (Result, error) {
	requester, _, err := r.artifactFetchRequester()
	if err != nil {
		return nil, err
	}
	fetchID := strings.TrimSpace(stringArg(args, "fetch_id", ""))
	if fetchID == "" {
		return nil, toolError("ARTIFACT_FETCH_ID_REQUIRED", "fetch_id is required", "validation")
	}
	if boolArg(args, "mounted", false) {
		job, err := requester.ConfirmMounted(ctx, fetchID)
		if err != nil {
			return nil, err
		}
		return Result{"ok": true, "fetch": job, "mounted": true}, nil
	}
	output, err := requester.Download(ctx, fetchID)
	if err != nil {
		return nil, err
	}
	base := strings.TrimRight(r.cfg.OAuthServerURL, "/")
	if base == "" {
		base = fmt.Sprintf("http://127.0.0.1:%d", r.cfg.Port)
	}
	resourceURI := base + "/artifacts/fetch/" + url.PathEscape(fetchID) + "?token=" + url.QueryEscape(output.OutputToken)
	return Result{
		"ok": true, "fetch_id": output.FetchID, "status": output.Status,
		"file_path": output.FilePath, "file_name": output.FileName, "mime_type": output.MIMEType,
		"size": output.Size, "sha256": output.SHA256, "resource_uri": resourceURI,
		"output_expires_at": output.OutputExpiresAt, "mounted": false,
	}, nil
}

func (r *Runtime) artifactFetchRequester() (*artifactrelay.FetchRequester, *artifactrelay.FetchStore, error) {
	if !r.cfg.ArtifactFetchEnabled {
		return nil, nil, errors.New("artifact fetch is disabled")
	}
	stateDir, err := config.NexusStateDir(r.cfg)
	if err != nil {
		return nil, nil, err
	}
	client, err := nexusclient.New(nexusclient.Config{BaseURL: r.cfg.NexusEndpoint, RequestTimeout: 30 * time.Second, PollTimeout: 35 * time.Second, UserAgent: "agentdock/" + config.Version})
	if err != nil {
		return nil, nil, err
	}
	deviceState, err := nexusclient.OpenStateStore(filepath.Join(stateDir, "device"))
	if err != nil {
		return nil, nil, err
	}
	credentials := func() (artifactrelay.DeviceCredentials, error) {
		state, err := deviceState.Load()
		if err != nil {
			return artifactrelay.DeviceCredentials{}, err
		}
		if !state.Valid(time.Now()) {
			return artifactrelay.DeviceCredentials{}, errors.New("valid Nexus device enrollment is required")
		}
		return artifactrelay.DeviceCredentials{DeviceID: state.DeviceID, DeviceToken: state.DeviceToken}, nil
	}
	store, err := artifactrelay.OpenFetchStore(filepath.Join(stateDir, "fetches"))
	if err != nil {
		return nil, nil, err
	}
	requester, err := artifactrelay.NewFetchRequester(artifactrelay.FetchRequesterConfig{Client: client, Credentials: credentials, Store: store})
	return requester, store, err
}

func (r *Runtime) ResolveArtifactFetchOutput(fetchID, token string) (artifactrelay.FetchLocalState, error) {
	_, store, err := r.artifactFetchRequester()
	if err != nil {
		return artifactrelay.FetchLocalState{}, err
	}
	return store.ResolveOutput(fetchID, token, time.Now().UTC())
}
