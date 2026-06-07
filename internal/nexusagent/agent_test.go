package nexusagent

import (
	"testing"

	"github.com/uvwt/agentdock/internal/envregistry"
)

func TestEnvDefinitionsIncludeLocalCompatVariables(t *testing.T) {
	definitions := envDefinitions(nil)
	byKey := map[string]envregistry.Definition{}
	for _, def := range definitions {
		byKey[def.Skill+"\x00"+def.Name] = def
	}
	for _, expected := range []envregistry.Definition{
		{Skill: "baidu-netdisk", Name: "BDPAN_CONFIG_FILE", Kind: envregistry.KindPlain},
		{Skill: "cloudsaver", Name: "CLOUDSAVER_PASSWORD", Kind: envregistry.KindSecret},
		{Skill: "cloudsaver", Name: "CLOUDSAVER_USERNAME", Kind: envregistry.KindPlain},
		{Skill: "dida365-open-api", Name: "DIDA365_ACCESS_TOKEN", Kind: envregistry.KindSecret},
		{Skill: "dida365-open-api", Name: "DIDA365_REGION", Kind: envregistry.KindPlain},
		{Skill: "douban-marks", Name: "DOUBAN_USER_ID", Kind: envregistry.KindPlain},
		{Skill: "openlist", Name: "OPENLIST_SESSION_FILE", Kind: envregistry.KindPlain},
		{Skill: "spotify-web-api", Name: "SPOTIFY_CLIENT_ID", Kind: envregistry.KindPlain},
		{Skill: "spotify-web-api", Name: "SPOTIFY_SCOPES", Kind: envregistry.KindPlain},
		{Skill: "telegram-official", Name: "TELEGRAM_CHAT_ID", Kind: envregistry.KindPlain},
		{Skill: "telegram-official", Name: "DOCK_DEVICE", Kind: envregistry.KindPlain},
		{Skill: "xiaohongshu-mcp", Name: "XIAOHONGSHU_MCP_URL", Kind: envregistry.KindPlain},
	} {
		got, ok := byKey[expected.Skill+"\x00"+expected.Name]
		if !ok {
			t.Fatalf("missing compat definition %s/%s", expected.Skill, expected.Name)
		}
		if got.Kind != expected.Kind {
			t.Fatalf("%s/%s kind = %s, want %s", expected.Skill, expected.Name, got.Kind, expected.Kind)
		}
	}
}
