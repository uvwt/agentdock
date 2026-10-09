package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	pluginruntime "github.com/uvwt/agentdock/internal/plugin"
	"github.com/uvwt/agentdock/internal/resourcelibrary"
)

// RuntimeResourceLibrary 是设备端资源库控制面。
// 它不接收压缩包正文。导出 ZIP 由 export_upload 流式 PUT 到已配对 Nexus；安装包从同一 HTTPS 原点下载，再交给现有 Skill/Plugin 安装器。
func (r *Runtime) RuntimeResourceLibrary(ctx context.Context, method string, body []byte) (Result, error) {
	service, err := r.resourceLibraryService()
	if err != nil {
		return nil, err
	}
	result, err := service.Handle(ctx, method, body)
	if err == nil {
		return Result(result), nil
	}
	var libraryErr *resourcelibrary.Error
	if errors.As(err, &libraryErr) {
		return nil, &ToolError{Code: libraryErr.Code, Message: libraryErr.Message, Category: libraryErr.Category}
	}
	return nil, err
}

func (r *Runtime) resourceLibraryService() (*resourcelibrary.Service, error) {
	// 暂存目录失败不应该阻止 Core 启动。第一次调用资源库时再失败关闭。
	r.resourceOnce.Do(func() {
		r.resourceSvc, r.resourceErr = resourcelibrary.New(filepath.Join(r.cfg.AgentDockHome, "tmp", "resource-library"), resourcelibrary.Deps{
			Identity: r.resourceIdentity,
			Lookup:   r.lookupManagedPackage,
			SkillValidate: func(ctx context.Context, archive, digest string) (resourcelibrary.SkillPreview, error) {
				result, err := r.skills.ValidateLocalArchive(ctx, archive, digest)
				if err != nil {
					return resourcelibrary.SkillPreview{}, err
				}
				issues := make([]string, 0, len(result.Issues))
				for _, issue := range result.Issues {
					issues = append(issues, issue.Message)
				}
				return resourcelibrary.SkillPreview{
					Valid: result.Valid, Name: result.Document.Name,
					SourceDigest: result.SourceDigest, ContentDigest: result.ContentDigest, Issues: issues,
				}, nil
			},
			SkillInstall: func(ctx context.Context, archive, digest string) (resourcelibrary.SkillCommit, error) {
				result, err := r.skills.InstallLocalArchive(ctx, archive, digest)
				if err != nil {
					return resourcelibrary.SkillCommit{}, err
				}
				return resourcelibrary.SkillCommit{Name: result.Skill, ContentDigest: result.ContentDigest, Changed: result.Changed}, nil
			},
			PluginValidate: func(ctx context.Context, archive string) (resourcelibrary.PluginPreview, error) {
				review, err := r.plugins.ValidateLocalArchive(ctx, archive)
				if err != nil {
					return resourcelibrary.PluginPreview{}, err
				}
				return pluginPreview(review), nil
			},
			PluginInstall: func(ctx context.Context, archive, reviewToken string) (resourcelibrary.PluginCommit, error) {
				result, err := r.plugins.InstallReviewedArchive(ctx, archive, true, reviewToken)
				if err != nil {
					return resourcelibrary.PluginCommit{}, err
				}
				return pluginCommit(result), nil
			},
			PluginUpdate: func(ctx context.Context, archive, reviewToken string) (resourcelibrary.PluginCommit, error) {
				result, err := r.plugins.UpdateReviewedArchive(ctx, archive, reviewToken)
				if err != nil {
					return resourcelibrary.PluginCommit{}, err
				}
				return pluginCommit(result), nil
			},
			PluginInstalled: func(name string) (bool, error) {
				_, err := r.plugins.InstalledPackage(name)
				if err == nil {
					return true, nil
				}
				var pluginErr *pluginruntime.Error
				if errors.As(err, &pluginErr) && pluginErr.Code == "PLUGIN_NOT_FOUND" {
					return false, nil
				}
				return false, err
			},
		})
	})
	return r.resourceSvc, r.resourceErr
}

// resourceIdentity 读取 Nexus 配对文件。nexusbridge 已经依赖 app，这里不能反向调用它。
// 文件格式与 internal/nexusbridge/identity.go 的 Identity 保持一致；缺文件按未配对失败关闭。
func (r *Runtime) resourceIdentity() (resourcelibrary.DeviceIdentity, error) {
	path := filepath.Join(r.cfg.AgentDockHome, "nexus", "device.json")
	info, err := os.Lstat(path)
	if err != nil {
		return resourcelibrary.DeviceIdentity{}, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return resourcelibrary.DeviceIdentity{}, errors.New("Nexus device identity is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return resourcelibrary.DeviceIdentity{}, err
	}
	var identity struct {
		Version     int    `json:"version"`
		Endpoint    string `json:"endpoint"`
		NodeID      string `json:"node_id"`
		DeviceToken string `json:"device_token"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return resourcelibrary.DeviceIdentity{}, err
	}
	if identity.Version != 1 || identity.Endpoint == "" || identity.NodeID == "" || identity.DeviceToken == "" {
		return resourcelibrary.DeviceIdentity{}, errors.New("Nexus device identity is invalid")
	}
	return resourcelibrary.DeviceIdentity{Endpoint: identity.Endpoint, NodeID: identity.NodeID, DeviceToken: identity.DeviceToken}, nil
}

func (r *Runtime) lookupManagedPackage(kind, name string) (string, error) {
	switch kind {
	case resourcelibrary.KindSkill:
		return r.skills.ManagedPackageRoot(name)
	case resourcelibrary.KindPlugin:
		installed, err := r.plugins.InstalledPackage(name)
		if err != nil {
			return "", err
		}
		return installed.Root, nil
	default:
		return "", &ToolError{Code: "PACKAGE_KIND_INVALID", Message: "package kind must be skill or plugin", Category: "validation"}
	}
}

func pluginPreview(review pluginruntime.Review) resourcelibrary.PluginPreview {
	return resourcelibrary.PluginPreview{
		Valid: review.Valid, Name: review.Name, Version: review.Version,
		PackageDigest: review.PackageDigest, ReviewToken: review.ReviewToken,
		Warnings: review.Warnings, Issues: review.Issues, Format: review.Format,
	}
}

func pluginCommit(result Result) resourcelibrary.PluginCommit {
	name, _ := result["name"].(string)
	version, _ := result["version"].(string)
	digest, _ := result["package_digest"].(string)
	changed, _ := result["changed"].(bool)
	return resourcelibrary.PluginCommit{Name: name, Version: version, PackageDigest: digest, Changed: changed}
}
