package publicartifacts

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open publish source: %w", err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create payload: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy payload: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close payload: %w", closeErr)
	}
	return nil
}

func writeTarGz(src, dst string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create archive payload: %w", err)
	}
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	walkErr := filepath.WalkDir(src, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		name, err := filepath.Rel(filepath.Dir(src), path)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if name == "." {
			return nil
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = name
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, in)
			closeErr := in.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})
	closeTar := tw.Close()
	closeGz := gz.Close()
	closeOut := out.Close()
	if walkErr != nil {
		return fmt.Errorf("archive source: %w", walkErr)
	}
	if closeTar != nil {
		return closeTar
	}
	if closeGz != nil {
		return closeGz
	}
	if closeOut != nil {
		return closeOut
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return streamSHA256(file)
}

func streamSHA256(reader io.Reader) (string, error) {
	hash := sha256.New()
	if _, err := io.Copy(hash, reader); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func imageDimensions(path, mimeType string) (int, int) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(mimeType)), "image/") {
		return 0, 0
	}
	file, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

func contentDisposition(mimeType string) string {
	value, _, err := mime.ParseMediaType(strings.ToLower(strings.TrimSpace(mimeType)))
	if err != nil {
		value = strings.ToLower(strings.TrimSpace(mimeType))
	}
	switch value {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/bmp", "text/plain":
		return "inline"
	default:
		// HTML、SVG、XML 等主动内容必须下载，不能在 AgentDock 同源下解释执行。
		return "attachment"
	}
}

func detectMime(path, filename string, archive bool) string {
	if archive {
		return "application/gzip"
	}
	if mt := mime.TypeByExtension(filepath.Ext(filename)); mt != "" {
		return mt
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return "application/octet-stream"
	}
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}

func safeDownloadName(value string) string {
	value = strings.ToValidUTF8(value, "_")
	value = strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f {
			return '_'
		}
		return char
	}, value)
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = filepath.Base(value)
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, "/\\") {
		return "artifact.bin"
	}
	if len(value) <= 240 {
		return value
	}

	// 下载名受 HTTP 头和文件系统共同约束。按字节限制时必须停在 rune 边界，
	// 否则长中文名会生成非法 UTF-8，并继续污染 metadata、URL 与响应头。
	ext := filepath.Ext(value)
	if len(ext) >= 240 {
		ext = ""
	}
	base := strings.TrimSuffix(value, ext)
	base = truncateUTF8Bytes(base, 240-len(ext))
	if base == "" {
		return "artifact.bin"
	}
	return base + ext
}

func truncateUTF8Bytes(value string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.ValidString(value[:maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
