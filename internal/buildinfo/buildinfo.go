package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/uvwt/agentdock/internal/config"
)

var (
	Commit    string
	BuildDate string
)

type Info struct {
	Version   string
	Commit    string
	BuildDate string
	GoVersion string
	Platform  string
}

func Current() Info {
	info := Info{
		Version:   config.Version,
		Commit:    strings.TrimSpace(Commit),
		BuildDate: strings.TrimSpace(BuildDate),
		GoVersion: runtime.Version(),
		Platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
	build, ok := debug.ReadBuildInfo()
	if ok {
		for _, setting := range build.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = strings.TrimSpace(setting.Value)
				}
			case "vcs.time":
				if info.BuildDate == "" {
					info.BuildDate = strings.TrimSpace(setting.Value)
				}
			}
		}
	}
	if len(info.Commit) > 12 {
		info.Commit = info.Commit[:12]
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.BuildDate == "" {
		info.BuildDate = "unknown"
	}
	return info
}
