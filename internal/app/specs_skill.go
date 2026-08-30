package app

import (
	"context"

	toolskill "github.com/uvwt/agentdock/internal/tool/skill"
)

func skillToolSpecs() []ToolSpec {
	return []ToolSpec{{Name: "skill_package", Contract: skillToolContract, Title: "Manage Skill packages", Description: "Validate, install, activate, or roll back AgentDock Skill packages and manage each Skill's isolated environment without returning secret values.", Annotations: mutatingToolAnnotations(true, true), Handler: typedToolHandler("skill_package", func(ctx context.Context, r *Runtime, request toolskill.PackageRequest) (Result, error) {
		return r.skills.Package(ctx, request)
	})}}
}
