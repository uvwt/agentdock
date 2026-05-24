package tools

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
)

type imageInfo struct {
	Width  int
	Height int
	MIME   string
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
