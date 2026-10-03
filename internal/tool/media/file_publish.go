package media

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/uvwt/agentdock/internal/httpx/requestmeta"
	"github.com/uvwt/agentdock/internal/publicartifacts"
)

const maxConnectorFileBytes int64 = 128 << 20

type connectorFileInput struct {
	DownloadURL string
	FileID      string
	FileName    string
	MimeType    string
}

func (s *Service) FilePublish(ctx context.Context, request FilePublishRequest) (Result, error) {
	pathValue, cleanup, err := s.filePublishSourcePath(ctx, request)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	store := publicartifacts.New(s.cfg.AgentDockHome, s.cfg.OAuthServerURL, s.cfg.Port)
	published, err := store.Publish(publicartifacts.PublishRequest{Path: pathValue, RetentionSeconds: intValue(request.RetentionSeconds, 0), BaseURL: requestmeta.BaseURL(ctx)})
	if err != nil {
		return nil, fmt.Errorf("publish file: %w", err)
	}
	result := Result{}
	for key, value := range artifactResult(published) {
		result[key] = value
	}
	return result, nil
}

func (s *Service) filePublishSourcePath(ctx context.Context, request FilePublishRequest) (string, func(), error) {
	if request.File != nil {
		if pathValue := connectorLocalPath(request.File); pathValue != "" {
			resolved, err := s.ws.ResolveExisting(pathValue)
			if err != nil {
				return "", func() {}, err
			}
			return resolved.Abs, func() {}, nil
		}
		if input, ok := connectorFile(request.File); ok {
			return s.downloadConnectorFile(ctx, input, newConnectorDownloadClient())
		}
	}
	pathValue := strings.TrimSpace(request.Path)
	if pathValue == "" {
		return "", func() {}, toolError("FILE_PUBLISH_SOURCE_REQUIRED", "file or path is required", "validation")
	}
	resolved, err := s.ws.ResolveExisting(pathValue)
	if err != nil {
		return "", func() {}, err
	}
	return resolved.Abs, func() {}, nil
}

func connectorLocalPath(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		for _, key := range []string{"local_path", "file_path", "mount_path", "path"} {
			if raw, ok := v[key].(string); ok && strings.TrimSpace(raw) != "" {
				return strings.TrimSpace(raw)
			}
		}
		if raw, ok := v["filename"].(string); ok && strings.TrimSpace(raw) != "" && filepath.IsAbs(raw) {
			return strings.TrimSpace(raw)
		}
	}
	return ""
}

func connectorFile(value any) (connectorFileInput, bool) {
	v, ok := value.(map[string]any)
	if !ok {
		return connectorFileInput{}, false
	}
	input := connectorFileInput{
		DownloadURL: stringValue(v["download_url"]),
		FileID:      stringValue(v["file_id"]),
		FileName:    stringValue(v["file_name"]),
		MimeType:    stringValue(v["mime_type"]),
	}
	if input.DownloadURL == "" && input.FileID == "" {
		return connectorFileInput{}, false
	}
	return input, true
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func (s *Service) downloadConnectorFile(ctx context.Context, input connectorFileInput, client *http.Client) (string, func(), error) {
	if input.DownloadURL == "" || input.FileID == "" {
		return "", func() {}, toolError("FILE_PUBLISH_FILE_INVALID", "file.download_url and file.file_id are required", "validation")
	}
	parsed, err := validateConnectorDownloadURL(input.DownloadURL)
	if err != nil {
		return "", func() {}, err
	}
	tmpRoot := filepath.Join(s.cfg.AgentDockHome, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o700); err != nil {
		return "", func() {}, fmt.Errorf("create file publish temp root: %w", err)
	}
	tmpDir, err := os.MkdirTemp(tmpRoot, "file-publish-")
	if err != nil {
		return "", func() {}, fmt.Errorf("create file publish temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	filename := connectorDownloadName(input, parsed)
	target := filepath.Join(tmpDir, filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.DownloadURL, nil)
	if err != nil {
		cleanup()
		return "", func() {}, toolError("FILE_PUBLISH_DOWNLOAD_URL_INVALID", "cannot create file download request", "validation")
	}
	resp, err := client.Do(req)
	if err != nil {
		cleanup()
		return "", func() {}, toolError("FILE_PUBLISH_DOWNLOAD_FAILED", "cannot download connector file", "runtime")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		cleanup()
		return "", func() {}, toolError("FILE_PUBLISH_DOWNLOAD_HTTP_ERROR", fmt.Sprintf("connector file download returned HTTP %d", resp.StatusCode), "runtime")
	}
	if resp.ContentLength > maxConnectorFileBytes {
		cleanup()
		return "", func() {}, toolError("FILE_PUBLISH_FILE_TOO_LARGE", "connector file exceeds maximum supported size", "validation")
	}

	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("create connector temp file: %w", err)
	}
	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, maxConnectorFileBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		cleanup()
		return "", func() {}, toolError("FILE_PUBLISH_DOWNLOAD_FAILED", "cannot read connector file download", "runtime")
	}
	if closeErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("close connector temp file: %w", closeErr)
	}
	if written > maxConnectorFileBytes {
		cleanup()
		return "", func() {}, toolError("FILE_PUBLISH_FILE_TOO_LARGE", "connector file exceeds maximum supported size", "validation")
	}
	return target, cleanup, nil
}

func validateConnectorDownloadURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return nil, toolError("FILE_PUBLISH_DOWNLOAD_URL_INVALID", "file.download_url must be an absolute HTTP(S) URL", "validation")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, toolError("FILE_PUBLISH_DOWNLOAD_URL_INVALID", "file.download_url must use http or https", "validation")
	}
	return parsed, nil
}

func connectorDownloadName(input connectorFileInput, parsed *url.URL) string {
	name := strings.TrimSpace(input.FileName)
	if name == "" {
		name = path.Base(parsed.Path)
	}
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if name == "" || name == "." || name == "/" {
		name = "uploaded-file"
	}
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	if name == "" || name == "." || name == ".." {
		return "uploaded-file"
	}
	return name
}

func newConnectorDownloadClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = safeConnectorDialContext
	return &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			scheme := strings.ToLower(req.URL.Scheme)
			if scheme != "http" && scheme != "https" {
				return fmt.Errorf("redirect uses unsupported scheme %q", req.URL.Scheme)
			}
			return nil
		},
	}
}

func safeConnectorDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("connector download host resolved to no addresses")
	}
	for _, resolved := range addresses {
		if blockedConnectorDownloadIP(resolved.IP) {
			return nil, fmt.Errorf("connector download host resolves to a non-public address")
		}
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, resolved := range addresses {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func blockedConnectorDownloadIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4[0] == 100 && ip4[1]&0xc0 == 0x40
	}
	return false
}
