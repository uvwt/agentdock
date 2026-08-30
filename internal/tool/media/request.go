package media

// CropRequest 是 view_image 的可选裁剪矩形。
type CropRequest struct {
	X      int `json:"x,omitempty"`
	Y      int `json:"y,omitempty"`
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// ViewImageRequest 是 view_image 进入媒体 capability 后的稳定输入契约。
type ViewImageRequest struct {
	ArtifactID      string       `json:"artifact_id,omitempty"`
	Path            string       `json:"path,omitempty"`
	URL             string       `json:"url,omitempty"`
	MaxSourceBytes  *int         `json:"max_source_bytes,omitempty"`
	SourceTimeoutMS *int         `json:"source_timeout_ms,omitempty"`
	MaxBytes        *int         `json:"max_bytes,omitempty"`
	MaxWidth        *int         `json:"max_width,omitempty"`
	MaxHeight       *int         `json:"max_height,omitempty"`
	AutoResize      *bool        `json:"auto_resize,omitempty"`
	Format          string       `json:"format,omitempty"`
	Quality         *int         `json:"quality,omitempty"`
	Crop            *CropRequest `json:"crop,omitempty"`
}

// FilePublishRequest.File 是 connector 文件重写产生的动态叶子；Path 是普通本地路径入口。
type FilePublishRequest struct {
	File             any    `json:"file,omitempty"`
	Path             string `json:"path,omitempty"`
	RetentionSeconds *int   `json:"retention_seconds,omitempty"`
}

func intValue(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}
