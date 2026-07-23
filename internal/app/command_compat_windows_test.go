//go:build windows

package app

func (r *Runtime) resolveWSLWorkdir(args map[string]any, skill string) (string, error) {
	return r.command.ResolveWSLWorkdir(args, skill)
}
