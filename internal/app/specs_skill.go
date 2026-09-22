package app

import (
	"context"

	toolskill "github.com/uvwt/agentdock/internal/tool/skill"
)

func skillToolSpecs() []ToolSpec {
	return []ToolSpec{{
		Name: "skill_manage", Contract: skillToolContract, Title: "Manage standalone Skills",
		Description: "Install or update current managed Skill content, remove it, and manage its isolated environment without exposing secret values.",
		Annotations: mutatingToolAnnotations(true, true),
		Handler: typedToolHandler("skill_manage", func(ctx context.Context, r *Runtime, request toolskill.ManageRequest) (Result, error) {
			return r.skills.Manage(ctx, request)
		}),
	}}
}
