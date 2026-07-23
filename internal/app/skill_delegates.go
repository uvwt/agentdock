package app

import "context"

func (r *Runtime) skillPackage(ctx context.Context, args map[string]any) (Result, error) {
	return r.skills.Package(ctx, args)
}

func (r *Runtime) RuntimeSkillFiles(skill string) (Result, error) {
	return r.skills.RuntimeSkillFiles(skill)
}

func (r *Runtime) RuntimeSkillFile(skill, relativePath string) (Result, error) {
	return r.skills.RuntimeSkillFile(skill, relativePath)
}
