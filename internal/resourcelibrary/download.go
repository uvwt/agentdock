package resourcelibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/uvwt/agentdock/internal/nexusclient"
)

// AuthorizeCloudDownload 确认候选包 URL 属于当前已配对的 Nexus 原点。
// 其他云、本机、链路本地和私网地址都拒绝。调用方随后只能用配对 endpoint 上的 Device Token 去下载。
func AuthorizeCloudDownload(endpoint, rawURL string) (string, error) {
	paired, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || paired.Scheme != "https" || paired.Hostname() == "" || paired.User != nil {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "paired Nexus endpoint is not an HTTPS origin")
	}
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download URL must be an HTTPS URL without user info")
	}
	if !strings.EqualFold(paired.Hostname(), target.Hostname()) || canonicalPort(paired) != canonicalPort(target) {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download URL is not on the paired Nexus origin")
	}
	if rejectedHost(target.Hostname()) {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download URL uses a local or non-public host")
	}
	addresses, err := net.LookupIP(target.Hostname())
	if err != nil || len(addresses) == 0 {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download host cannot be resolved to a public address")
	}
	for _, address := range addresses {
		if !publicIP(address) {
			return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download host resolves to a non-public address")
		}
	}
	if dotDotSegment(target.Path) || dotDotSegment(target.RawPath) || strings.Contains(target.Path, `\`) {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download path is not allowed")
	}
	clean := path.Clean(target.Path)
	if clean == "/" || clean == "." || !strings.HasPrefix(clean, "/") {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download path is not allowed")
	}
	requestURI := target.EscapedPath()
	if target.RawQuery != "" {
		requestURI += "?" + target.RawQuery
	}
	if requestURI == "" || strings.Contains(requestURI, "://") || strings.HasPrefix(requestURI, "//") {
		return "", failed("DOWNLOAD_URL_REJECTED", "validation", "package download path is not allowed")
	}
	return requestURI, nil
}

func canonicalPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if value.Scheme == "https" {
		return "443"
	}
	return ""
}

func rejectedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	switch host {
	case "localhost", "localhost.localdomain", "metadata.google.internal", "metadata.google":
		return true
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil && !publicIP(ip) {
		return true
	}
	return false
}

func publicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return true
	}
	if v4[0] == 0 || v4[0] >= 224 {
		return false
	}
	// 运营商级 NAT 和文档地址都不是可接受的包下载目标。
	if v4[0] == 100 && v4[1]&0xc0 == 64 {
		return false
	}
	if v4[0] == 192 && v4[1] == 0 && v4[2] == 2 {
		return false
	}
	if v4[0] == 198 && v4[1] == 51 && v4[2] == 100 {
		return false
	}
	if v4[0] == 203 && v4[1] == 0 && v4[2] == 113 {
		return false
	}
	if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
		return false
	}
	return true
}

func dotDotSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || segment == "." || strings.EqualFold(segment, "%2e%2e") || strings.EqualFold(segment, "%2e") {
			return true
		}
	}
	return false
}

// FetchPackage 通过现有 Nexus HTTP 客户端下载候选 ZIP。
// 重定向不跟随；Device Token 只发给配对 endpoint，不会被带到其他 Origin。
func FetchPackage(ctx context.Context, endpoint, token, rawURL, destination string, maxBytes int64) (string, error) {
	requestURI, err := AuthorizeCloudDownload(endpoint, rawURL)
	if err != nil {
		return "", err
	}
	return downloadAuthorizedPath(ctx, endpoint, token, requestURI, destination, maxBytes)
}

func downloadAuthorizedPath(ctx context.Context, endpoint, token, requestURI, destination string, maxBytes int64) (string, error) {
	if maxBytes <= 0 {
		return "", failed("PACKAGE_INVALID", "validation", "download limit must be positive")
	}
	if strings.TrimSpace(token) == "" {
		return "", failed("NEXUS_NOT_PAIRED", "validation", "Nexus device token is unavailable")
	}
	client := nexusclient.New(endpoint, token)
	response, err := client.Do(ctx, http.MethodGet, requestURI, nil)
	if err != nil {
		return "", failed("DOWNLOAD_FAILED", "validation", "package download failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", failed("DOWNLOAD_FAILED", "validation", "package download was not accepted")
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", failed("PACKAGE_INVALID", "validation", "package download cannot be stored")
	}
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(output, hasher), io.LimitReader(response.Body, maxBytes+1))
	closeErr := output.Close()
	if copyErr != nil || closeErr != nil || written > maxBytes {
		_ = os.Remove(destination)
		if written > maxBytes {
			return "", failedf("PACKAGE_INVALID", "validation", "package exceeds %d bytes", maxBytes)
		}
		return "", failed("DOWNLOAD_FAILED", "validation", "package download failed")
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}
