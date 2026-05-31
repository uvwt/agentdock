package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"strings"
)

type imageInfo struct {
	Width  int
	Height int
	MIME   string
}

type imageCrop struct {
	X      int
	Y      int
	Width  int
	Height int
}

func prepareImageBytes(data []byte, crop *imageCrop, maxBytes, maxWidth, maxHeight int, format string, quality int) ([]byte, imageInfo, map[string]any, []string, bool) {
	warnings := []string{}
	if maxBytes <= 0 {
		maxBytes = 750000
	}
	if maxWidth <= 0 {
		maxWidth = 1280
	}
	if maxHeight <= 0 {
		maxHeight = 1280
	}
	if quality <= 0 {
		quality = 72
	}
	if quality > 95 {
		quality = 95
	}
	if quality < 35 {
		quality = 35
	}

	img, inputFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, imageInfo{}, nil, append(warnings, "image decode failed"), false
	}
	bounds := img.Bounds()
	original := map[string]any{"bytes": len(data), "width": bounds.Dx(), "height": bounds.Dy(), "format": inputFormat}

	if crop != nil && crop.Width > 0 && crop.Height > 0 {
		cropRect := image.Rect(bounds.Min.X+crop.X, bounds.Min.Y+crop.Y, bounds.Min.X+crop.X+crop.Width, bounds.Min.Y+crop.Y+crop.Height).Intersect(bounds)
		if cropRect.Empty() {
			warnings = append(warnings, "crop rectangle is outside image bounds; using full image")
		} else {
			dst := image.NewRGBA(image.Rect(0, 0, cropRect.Dx(), cropRect.Dy()))
			draw.Draw(dst, dst.Bounds(), img, cropRect.Min, draw.Src)
			img = dst
			bounds = img.Bounds()
			original["crop"] = map[string]any{"x": crop.X, "y": crop.Y, "width": crop.Width, "height": crop.Height}
		}
	}

	if maxWidth > 0 && maxHeight > 0 && (bounds.Dx() > maxWidth || bounds.Dy() > maxHeight) {
		scale := math.Min(float64(maxWidth)/float64(bounds.Dx()), float64(maxHeight)/float64(bounds.Dy()))
		if scale > 0 && scale < 1 {
			newW := int(math.Max(1, math.Floor(float64(bounds.Dx())*scale)))
			newH := int(math.Max(1, math.Floor(float64(bounds.Dy())*scale)))
			img = resizeBilinear(img, newW, newH)
			bounds = img.Bounds()
		}
	}

	outFormat := strings.ToLower(strings.TrimSpace(format))
	if outFormat == "" || outFormat == "jpg" {
		outFormat = "jpeg"
	}
	if outFormat != "png" && outFormat != "jpeg" {
		warnings = append(warnings, "unsupported output format; using jpeg")
		outFormat = "jpeg"
	}

	for attempt := 0; attempt < 8; attempt++ {
		encoded, info, ok := encodeImageBytes(img, outFormat, quality)
		if !ok {
			return nil, imageInfo{}, original, append(warnings, "image encode failed"), false
		}
		if len(encoded) <= maxBytes || attempt == 7 {
			if len(encoded) > maxBytes {
				warnings = append(warnings, "encoded image still exceeds max_bytes after downscaling")
			}
			info.Width = img.Bounds().Dx()
			info.Height = img.Bounds().Dy()
			return encoded, info, original, warnings, len(encoded) <= maxBytes
		}
		if outFormat == "jpeg" && quality > 45 {
			quality -= 10
			if quality < 45 {
				quality = 45
			}
			continue
		}
		b := img.Bounds()
		newW := int(math.Max(1, math.Floor(float64(b.Dx())*0.82)))
		newH := int(math.Max(1, math.Floor(float64(b.Dy())*0.82)))
		if newW == b.Dx() && newH == b.Dy() {
			break
		}
		img = resizeBilinear(img, newW, newH)
	}
	return nil, imageInfo{}, original, warnings, false
}

func encodeImageBytes(img image.Image, format string, quality int) ([]byte, imageInfo, bool) {
	var out bytes.Buffer
	bounds := img.Bounds()
	switch format {
	case "png":
		if err := png.Encode(&out, img); err != nil && err != io.ErrClosedPipe {
			return nil, imageInfo{}, false
		}
		return out.Bytes(), imageInfo{Width: bounds.Dx(), Height: bounds.Dy(), MIME: "image/png"}, true
	default:
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, imageInfo{}, false
		}
		return out.Bytes(), imageInfo{Width: bounds.Dx(), Height: bounds.Dy(), MIME: "image/jpeg"}, true
	}
}

func imageDiffPercent(a, b []byte) (float64, bool) {
	imgA, _, errA := image.Decode(bytes.NewReader(a))
	imgB, _, errB := image.Decode(bytes.NewReader(b))
	if errA != nil || errB != nil {
		return 0, false
	}
	ba := imgA.Bounds()
	bb := imgB.Bounds()
	w := minInt(ba.Dx(), bb.Dx())
	h := minInt(ba.Dy(), bb.Dy())
	if w <= 0 || h <= 0 {
		return 0, false
	}
	stepX := int(math.Max(1, math.Floor(float64(w)/160)))
	stepY := int(math.Max(1, math.Floor(float64(h)/90)))
	changed := 0
	total := 0
	for y := 0; y < h; y += stepY {
		for x := 0; x < w; x += stepX {
			r1, g1, b1, _ := imgA.At(ba.Min.X+x, ba.Min.Y+y).RGBA()
			r2, g2, b2, _ := imgB.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			delta := absInt(int(r1>>8)-int(r2>>8)) + absInt(int(g1>>8)-int(g2>>8)) + absInt(int(b1>>8)-int(b2>>8))
			if delta > 45 {
				changed++
			}
			total++
		}
	}
	if total == 0 {
		return 0, false
	}
	return float64(changed) / float64(total), true
}

func identifyImage(data []byte) (imageInfo, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return imageInfo{}, err
	}
	mimeType := "image/" + format
	if format == "jpg" {
		mimeType = "image/jpeg"
	}
	return imageInfo{Width: cfg.Width, Height: cfg.Height, MIME: mimeType}, nil
}

func shouldResizeImage(size, width, height, maxBytes, maxWidth, maxHeight int) bool {
	return size > maxBytes || width > maxWidth || height > maxHeight
}

func resizeImageBytes(data []byte, maxBytes, maxWidth, maxHeight int) ([]byte, imageInfo, bool) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, imageInfo{}, false
	}
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	scale := math.Min(float64(maxWidth)/float64(w), float64(maxHeight)/float64(h))
	if scale <= 0 || scale > 1 {
		scale = 1
	}
	newW := int(math.Max(1, math.Floor(float64(w)*scale)))
	newH := int(math.Max(1, math.Floor(float64(h)*scale)))
	resized := resizeBilinear(img, newW, newH)

	var out bytes.Buffer
	mimeType := "image/png"
	switch format {
	case "jpeg", "jpg":
		mimeType = "image/jpeg"
		for _, quality := range []int{85, 75, 65, 55} {
			out.Reset()
			if err := jpeg.Encode(&out, resized, &jpeg.Options{Quality: quality}); err != nil {
				return nil, imageInfo{}, false
			}
			if out.Len() <= maxBytes || quality == 55 {
				break
			}
		}
	case "gif":
		mimeType = "image/gif"
		if err := gif.Encode(&out, resized, nil); err != nil {
			return nil, imageInfo{}, false
		}
	default:
		if err := png.Encode(&out, resized); err != nil && err != io.ErrClosedPipe {
			return nil, imageInfo{}, false
		}
	}
	return out.Bytes(), imageInfo{Width: newW, Height: newH, MIME: mimeType}, true
}

func resizeBilinear(src image.Image, width, height int) *image.RGBA {
	bounds := src.Bounds()
	srcW, srcH := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		fy := float64(y) * float64(srcH-1) / math.Max(1, float64(height-1))
		y0 := int(math.Floor(fy))
		y1 := minInt(y0+1, srcH-1)
		wy := fy - float64(y0)
		for x := 0; x < width; x++ {
			fx := float64(x) * float64(srcW-1) / math.Max(1, float64(width-1))
			x0 := int(math.Floor(fx))
			x1 := minInt(x0+1, srcW-1)
			wx := fx - float64(x0)
			dst.Set(x, y, mixColors(src.At(bounds.Min.X+x0, bounds.Min.Y+y0), src.At(bounds.Min.X+x1, bounds.Min.Y+y0), src.At(bounds.Min.X+x0, bounds.Min.Y+y1), src.At(bounds.Min.X+x1, bounds.Min.Y+y1), wx, wy))
		}
	}
	return dst
}

func mixColors(c00, c10, c01, c11 color.Color, wx, wy float64) color.Color {
	r00, g00, b00, a00 := c00.RGBA()
	r10, g10, b10, a10 := c10.RGBA()
	r01, g01, b01, a01 := c01.RGBA()
	r11, g11, b11, a11 := c11.RGBA()
	return color.RGBA64{R: uint16(mixChannel(r00, r10, r01, r11, wx, wy)), G: uint16(mixChannel(g00, g10, g01, g11, wx, wy)), B: uint16(mixChannel(b00, b10, b01, b11, wx, wy)), A: uint16(mixChannel(a00, a10, a01, a11, wx, wy))}
}

func mixChannel(c00, c10, c01, c11 uint32, wx, wy float64) uint32 {
	top := float64(c00)*(1-wx) + float64(c10)*wx
	bottom := float64(c01)*(1-wx) + float64(c11)*wx
	return uint32(top*(1-wy) + bottom*wy)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
