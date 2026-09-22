package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/uvwt/agentdock/internal/agentinstructions"
)

type workspaceContextRequest struct {
	Workdir string `json:"workdir,omitempty"`
}

type workspaceContextResult struct {
	Workdir         string                   `json:"workdir"`
	WorkspaceRoot   string                   `json:"workspace_root"`
	Instructions    []agentinstructions.File `json:"instructions"`
	WorkspaceSkills []workspaceSkillItem     `json:"workspace_skills"`
	Warnings        []capabilityWarning      `json:"warnings"`
}

type workspaceSkillItem struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	File          string `json:"file"`
	SkillRef      string `json:"skill_ref"`
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id"`
	ContentDigest string `json:"content_digest,omitempty"`
}

// workspaceContext 每次调用都从磁盘重新读取当前工作区规则与本地 Skill 索引。
// workdir 只用于本次选择，不修改 Workspace 默认 cwd，也不保存为 Runtime 状态。
func (r *Runtime) workspaceContext(ctx context.Context, workdir string) (Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := r.ws.ResolveExisting(workdir)
	if err != nil {
		return nil, toolErrorDetails("INVALID_ARGUMENT", "workspace workdir must resolve to an existing host directory", "validation", map[string]any{"workdir": workdir})
	}
	info, err := os.Stat(resolved.Abs)
	if err != nil || !info.IsDir() {
		return nil, toolErrorDetails("INVALID_ARGUMENT", "workspace workdir must be a directory", "validation", map[string]any{"workdir": workdir})
	}

	// 全局 AGENTS.md 只有一个固定位置；它不受 AGENTDOCK_HOME 等运行目录配置影响。
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	instructions, err := agentinstructions.Load(ctx, agentinstructions.Options{
		Home:       filepath.Join(home, ".agentdock"),
		DefaultDir: r.ws.Root(),
		Workdir:    resolved.Abs,
	})
	if err != nil {
		return nil, err
	}

	warnings := []capabilityWarning{}
	workspaceSkills := []workspaceSkillItem{}
	skillIndex, skillErr := scanWorkspaceFilesystemSkills(instructions.WorkspaceRoot)
	if skillErr != nil {
		warnings = append(warnings, capabilityWarning{Source: "workspace_skills", Message: "工作区 Skill 索引暂不可用。"})
	} else {
		workspaceSkills = make([]workspaceSkillItem, 0, len(skillIndex.Items))
		for _, item := range skillIndex.Items {
			skillRef, sourceID, refErr := r.skills.WorkspaceSkillRef(instructions.WorkspaceRoot, item.Name)
			if refErr != nil {
				warnings = append(warnings, capabilityWarning{Source: "workspace_skills", Message: "工作区 Skill 引用生成失败：" + item.Name})
				continue
			}
			workspaceSkills = append(workspaceSkills, workspaceSkillItem{
				Name: item.Name, Description: item.Description,
				File: skillRef + "/SKILL.md", SkillRef: skillRef,
				SourceType: "workspace", SourceID: sourceID,
			})
		}
		if skillIndex.Truncated {
			warnings = append(warnings, capabilityWarning{Source: "workspace_skills", Message: "工作区 Skill 数量超过索引上限，仅返回稳定排序后的前 50 项。"})
		}
	}

	value := workspaceContextResult{
		Workdir:         instructions.Workdir,
		WorkspaceRoot:   instructions.WorkspaceRoot,
		Instructions:    instructions.Files,
		WorkspaceSkills: workspaceSkills,
		Warnings:        warnings,
	}
	var result Result
	if err := remarshal(value, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *Runtime) workspaceContextTool(ctx context.Context, args map[string]any) (Result, error) {
	var request workspaceContextRequest
	if err := decodeToolInput("workspace_context", args, &request); err != nil {
		return nil, err
	}
	return r.workspaceContext(ctx, request.Workdir)
}
