package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/artifactrelay"
	"github.com/uvwt/agentdock/internal/workspace"
)

type connectorFileReference struct {
	LocalPath   string
	DownloadURL string
	Filename    string
}

func resolveArtifactSendSource(
	ctx context.Context,
	ws *workspace.Workspace,
	fileValue any,
	pathValue string,
	tempRoot string,
	httpClient *http.Client,
) (string, func(), error) {
	cleanup := func() {}
	if fileValue == nil {
		pathValue = strings.TrimSpace(pathValue)
		if pathValue == "" {
			return "", cleanup, toolError("ARTIFACT_SOURCE_REQUIRED", "file or path is required", "validation")
		}
		resolved, err := ws.ResolveExisting(pathValue)
		return resolved.Abs, cleanup, err
	}

	switch value := fileValue.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return resolveArtifactSendSource(ctx, ws, nil, pathValue, tempRoot, httpClient)
		}
		resolved, err := ws.ResolveExisting(value)
		return resolved.Abs, cleanup, err
	case map[string]any:
		reference := parseConnectorFileReference(value)
		var localErr error
		if reference.LocalPath != "" {
			resolved, err := ws.ResolveExisting(reference.LocalPath)
			if err == nil {
				return resolved.Abs, cleanup, nil
			}
			localErr = err
		}
		if reference.DownloadURL != "" {
			path, remove, err := downloadConnectorFile(ctx, httpClient, reference.DownloadURL, reference.Filename, tempRoot)
			if err != nil {
				return "", cleanup, err
			}
			return path, remove, nil
		}
		if localErr != nil {
			return "", cleanup, localErr
		}
		return "", cleanup, toolError("ARTIFACT_FILE_REFERENCE_INVALID", "connector file reference has no mounted path or HTTPS download address", "validation")
	default:
		return "", cleanup, toolError("ARTIFACT_FILE_REFERENCE_INVALID", "file must be a path or connector file reference", "validation")
	}
}

func parseConnectorFileReference(value map[string]any) connectorFileReference {
	return connectorFileReference{
		LocalPath:   firstMapString(value, "local_path", "file_path", "mount_path", "path"),
		DownloadURL: firstMapString(value, "download_url"),
		Filename:    firstMapString(value, "filename", "name"),
	}
}

func firstMapString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if item, ok := value[key].(string); ok {
			if item = strings.TrimSpace(item); item != "" {
				return item
			}
		}
	}
	return ""
}

func newConnectorFileHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("connector file download exceeded redirect limit")
			}
			return validateConnectorDownloadURL(request.URL)
		},
	}
}

func downloadConnectorFile(ctx context.Context, client *http.Client, rawURL, filename, tempRoot string) (string, func(), error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", func() {}, toolError("ARTIFACT_FILE_REFERENCE_INVALID", "connector file download address is invalid", "validation")
	}
	if err := validateConnectorDownloadURL(parsed); err != nil {
		return "", func() {}, err
	}
	if client == nil {
		client = newConnectorFileHTTPClient()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return "", func() {}, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "agentdock-connector-file/1")
	response, err := client.Do(request)
	if err != nil {
		return "", func() {}, fmt.Errorf("download connector file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", func() {}, fmt.Errorf("download connector file: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > artifactrelay.MaxCipherBytes {
		return "", func() {}, fmt.Errorf("connector file exceeds %d bytes", artifactrelay.MaxCipherBytes)
	}

	if strings.TrimSpace(filename) == "" {
		filename = responseFilename(response.Header.Get("Content-Disposition"))
	}
	filename = safeConnectorFilename(filename)
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		return "", func() {}, fmt.Errorf("create connector file root: %w", err)
	}
	if err := os.Chmod(tempRoot, 0o700); err != nil {
		return "", func() {}, fmt.Errorf("secure connector file root: %w", err)
	}
	tempDir, err := os.MkdirTemp(tempRoot, "input-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create connector file directory: %w", err)
	}
	remove := func() { _ = os.RemoveAll(tempDir) }
	if err := os.Chmod(tempDir, 0o700); err != nil {
		remove()
		return "", func() {}, fmt.Errorf("secure connector file directory: %w", err)
	}

	path := filepath.Join(tempDir, filename)
	output, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		remove()
		return "", func() {}, fmt.Errorf("create connector file: %w", err)
	}
	written, copyErr := io.Copy(output, io.LimitReader(response.Body, artifactrelay.MaxCipherBytes+1))
	closeErr := output.Close()
	if copyErr != nil {
		remove()
		return "", func() {}, fmt.Errorf("store connector file: %w", copyErr)
	}
	if closeErr != nil {
		remove()
		return "", func() {}, fmt.Errorf("close connector file: %w", closeErr)
	}
	if written > artifactrelay.MaxCipherBytes {
		remove()
		return "", func() {}, fmt.Errorf("connector file exceeds %d bytes", artifactrelay.MaxCipherBytes)
	}
	return path, remove, nil
}

func validateConnectorDownloadURL(value *url.URL) error {
	if value == nil || !strings.EqualFold(value.Scheme, "https") || value.Hostname() == "" || value.User != nil {
		return toolError("ARTIFACT_FILE_REFERENCE_INVALID", "connector file download address must be HTTPS", "validation")
	}
	host := strings.ToLower(strings.TrimSuffix(value.Hostname(), "."))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return toolError("ARTIFACT_FILE_REFERENCE_INVALID", "connector file download host is not allowed", "validation")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return toolError("ARTIFACT_FILE_REFERENCE_INVALID", "connector file download host is not allowed", "validation")
	}
	return nil
}

func responseFilename(disposition string) string {
	_, params, err := mime.ParseMediaType(strings.TrimSpace(disposition))
	if err != nil {
		return ""
	}
	return params["filename"]
}

func safeConnectorFilename(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = filepath.Base(value)
	if value == "" || value == "." || value == ".." {
		return "connector-file.bin"
	}
	if len(value) > 240 {
		extension := filepath.Ext(value)
		base := strings.TrimSuffix(value, extension)
		maxBase := 240 - len(extension)
		if maxBase < 1 {
			return "connector-file.bin"
		}
		value = base[:maxBase] + extension
	}
	return value
}
