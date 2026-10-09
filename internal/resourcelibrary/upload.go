package resourcelibrary

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/uvwt/agentdock/internal/nexusclient"
)

// UploadPackage 用已配对 Device Token 把暂存 ZIP 流式 PUT 到资源库传输路由。
// 正文来自文件，不先读入内存；Content-Length 使用调用方核对过的大小。3xx 不跟随。
func UploadPackage(ctx context.Context, endpoint, token, rawURL, archivePath string, size int64) error {
	requestURI, err := AuthorizeCloudUpload(endpoint, rawURL)
	if err != nil {
		return err
	}
	return uploadAuthorizedPath(ctx, endpoint, token, requestURI, archivePath, size)
}

func uploadAuthorizedPath(ctx context.Context, endpoint, token, requestURI, archivePath string, size int64) error {
	if size <= 0 {
		return failed("PACKAGE_INVALID", "validation", "upload size must be positive")
	}
	if strings.TrimSpace(token) == "" {
		return failed("NEXUS_NOT_PAIRED", "validation", "Nexus device token is unavailable")
	}
	info, err := os.Lstat(archivePath)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != size {
		return failed("ARCHIVE_CHANGED", "validation", "export archive changed before upload")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return failed("UPLOAD_FAILED", "validation", "package upload failed")
	}
	defer file.Close()
	if err := ctx.Err(); err != nil {
		return uploadContextError(err)
	}
	client := nexusclient.New(endpoint, token)
	response, err := client.DoStream(ctx, http.MethodPut, requestURI, "application/zip", file, size)
	if err != nil {
		if ctx.Err() != nil {
			return uploadContextError(ctx.Err())
		}
		return failed("UPLOAD_FAILED", "validation", "package upload failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return failed("UPLOAD_REDIRECT_REJECTED", "validation", "package upload redirect was not followed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return failed("UPLOAD_FAILED", "validation", "package upload was not accepted")
	}
	return nil
}

func uploadContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return failed("UPLOAD_TIMEOUT", "validation", "package upload timed out")
	}
	return failed("UPLOAD_FAILED", "validation", "package upload failed")
}
