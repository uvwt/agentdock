package media

import (
	"context"
	"strings"
)

func (s *Service) ViewImage(ctx context.Context, request ViewImageRequest) (Result, error) {
	loaded, err := s.loadImageSource(ctx, request)
	if err != nil {
		return nil, err
	}
	info, err := identifyImage(loaded.Data)
	if err != nil {
		return nil, toolError("BINARY_FILE", "source is not a supported image", "validation")
	}

	maxBytes := intValue(request.MaxBytes, defaultViewImageBytes)
	if maxBytes <= 0 {
		maxBytes = defaultViewImageBytes
	}
	if maxBytes > hardViewImageBytes {
		maxBytes = hardViewImageBytes
	}
	maxWidth := intValue(request.MaxWidth, 1280)
	maxHeight := intValue(request.MaxHeight, 1280)
	format := request.Format
	if strings.TrimSpace(format) == "" {
		format = "jpeg"
	}
	quality := intValue(request.Quality, 72)
	crop := imageCropFromRequest(request.Crop)
	autoResize := boolValue(request.AutoResize, true)

	original := map[string]any{"size_bytes": len(loaded.Data), "width": info.Width, "height": info.Height, "mime_type": info.MIME}
	prepared := loaded.Data
	preparedInfo := info
	warnings := []string{}
	resized := false
	if crop != nil || autoResize || strings.TrimSpace(format) != "" || quality != 72 {
		var ok bool
		var prepOriginal map[string]any
		prepared, preparedInfo, prepOriginal, warnings, ok = prepareImageBytes(loaded.Data, crop, maxBytes, maxWidth, maxHeight, format, quality)
		if prepOriginal != nil {
			if bytes, ok := prepOriginal["bytes"]; ok {
				prepOriginal["size_bytes"] = bytes
				delete(prepOriginal, "bytes")
			}
			original = prepOriginal
		}
		if !ok {
			return nil, toolErrorDetails("IMAGE_TOO_LARGE", "image exceeds max_bytes after processing", "validation", map[string]any{"bytes": len(prepared), "max_bytes": maxBytes, "auto_resize": autoResize, "warnings": warnings})
		}
		resized = preparedInfo.Width != info.Width || preparedInfo.Height != info.Height || len(prepared) != len(loaded.Data)
	}
	if len(prepared) > maxBytes {
		return nil, toolErrorDetails("IMAGE_TOO_LARGE", "image exceeds max_bytes", "validation", map[string]any{"bytes": len(prepared), "max_bytes": maxBytes, "auto_resize": autoResize, "warnings": warnings})
	}

	result := Result{
		"source":   loaded.Source,
		"image":    imageMetadata(loaded.Name, preparedInfo, len(prepared)),
		"original": original,
		"resized":  resized,
		"warnings": warnings,
	}
	attachMCPImage(result, prepared, preparedInfo.MIME)
	return result, nil
}
