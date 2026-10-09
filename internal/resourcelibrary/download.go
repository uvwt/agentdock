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
	return authorizePairedHTTPS(endpoint, rawURL, "DOWNLOAD_URL_REJECTED", "package download")
}

// AuthorizeCloudUpload 确认上传 URL 是同一配对原点上的资源库传输路由。
// ticket 由 Cloud 绑定 node、kind 和预期摘要；设备只校验原点、公网地址和路径形状。
func AuthorizeCloudUpload(endpoint, rawURL string) (string, error) {
	requestURI, err := authorizePairedHTTPS(endpoint, rawURL, "UPLOAD_URL_REJECTED", "package upload")
	if err != nil {
		return "", err
	}
	if err := libraryTransferPath(requestURI); err != nil {
		return "", err
	}
	return requestURI, nil
}

func authorizePairedHTTPS(endpoint, rawURL, code, label string) (string, error) {
	paired, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || paired.Scheme != "https" || paired.Hostname() == "" || paired.User != nil {
		return "", failed(code, "validation", "paired Nexus endpoint is not an HTTPS origin")
	}
	target, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil || target.Fragment != "" {
		return "", failed(code, "validation", label+" URL must be an HTTPS URL without user info")
	}
	if !strings.EqualFold(paired.Hostname(), target.Hostname()) || canonicalPort(paired) != canonicalPort(target) {
		return "", failed(code, "validation", label+" URL is not on the paired Nexus origin")
	}
	if rejectedHost(target.Hostname()) {
		return "", failed(code, "validation", label+" URL uses a local or non-public host")
	}
	// 这里的解析只提前拒绝明显的私网目标。真正拨号时 packageClient 会再查一次，
	// 并只连接当时仍全部通过 publicIP 的 IP。授权结果不能单独当成拨号锁定。
	addresses, err := net.LookupIP(target.Hostname())
	if err != nil || len(addresses) == 0 {
		return "", failed(code, "validation", label+" host cannot be resolved to a public address")
	}
	for _, address := range addresses {
		if !publicIP(address) {
			return "", failed(code, "validation", label+" host resolves to a non-public address")
		}
	}
	if dotDotSegment(target.Path) || dotDotSegment(target.RawPath) || strings.Contains(target.Path, `\`) {
		return "", failed(code, "validation", label+" path is not allowed")
	}
	clean := path.Clean(target.Path)
	if clean == "/" || clean == "." || !strings.HasPrefix(clean, "/") {
		return "", failed(code, "validation", label+" path is not allowed")
	}
	requestURI := target.EscapedPath()
	if target.RawQuery != "" {
		requestURI += "?" + target.RawQuery
	}
	if requestURI == "" || strings.Contains(requestURI, "://") || strings.HasPrefix(requestURI, "//") {
		return "", failed(code, "validation", label+" path is not allowed")
	}
	return requestURI, nil
}

func libraryTransferPath(requestURI string) error {
	const prefix = "/v1/nodes/library/transfer/"
	if strings.Contains(requestURI, "?") || !strings.HasPrefix(requestURI, prefix) {
		return failed("UPLOAD_URL_REJECTED", "validation", "package upload URL is not the library transfer route")
	}
	ticket := strings.TrimPrefix(requestURI, prefix)
	if len(ticket) < 8 || len(ticket) > 256 || strings.Contains(ticket, "/") {
		return failed("UPLOAD_URL_REJECTED", "validation", "package upload ticket is invalid")
	}
	for _, r := range ticket {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return failed("UPLOAD_URL_REJECTED", "validation", "package upload ticket is invalid")
		}
	}
	return nil
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
		return publicIPv6(ip)
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

func publicIPv6(ip net.IP) bool {
	ip = ip.To16()
	if ip == nil {
		return false
	}
	// 文档地址、已废弃站点本地、丢弃前缀和 NAT64 翻译前缀都不是可直接固定的公网目标。
	if ipv6Documentation.Contains(ip) || ipv6Discard.Contains(ip) || nat64WellKnown.Contains(ip) || nat64Local.Contains(ip) {
		return false
	}
	if ip[0] == 0xfe && ip[1]&0xc0 == 0xc0 {
		return false
	}
	// 6to4 把 IPv4 嵌在地址里。嵌入的是私网地址时，不能把它当成公网 IPv6。
	if ip[0] == 0x20 && ip[1] == 0x02 {
		return publicIP(net.IPv4(ip[2], ip[3], ip[4], ip[5]))
	}
	return true
}

var (
	ipv6Documentation = mustCIDR("2001:db8::/32")
	ipv6Discard       = mustCIDR("100::/64")
	nat64WellKnown    = mustCIDR("64:ff9b::/96")
	nat64Local        = mustCIDR("64:ff9b:1::/48")
)

func mustCIDR(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}

func dotDotSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." || segment == "." || strings.EqualFold(segment, "%2e%2e") || strings.EqualFold(segment, "%2e") {
			return true
		}
	}
	return false
}

// FetchPackage 用资源包专用客户端下载候选 ZIP。
// 重定向不跟随。Device Token 只出现在发往配对域名的请求里，连接地址是拨号时重新校验过的公网 IP。
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
	client, err := packageClient(endpoint, token)
	if err != nil {
		return "", err
	}
	return downloadWith(ctx, client, requestURI, destination, maxBytes)
}

func downloadWith(ctx context.Context, client nexusclient.Client, requestURI, destination string, maxBytes int64) (string, error) {
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
