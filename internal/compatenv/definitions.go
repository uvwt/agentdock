package compatenv

type Definition struct {
	Skill  string
	Name   string
	Kind   string
	Source string
}

const (
	KindPlain  = "plain"
	KindSecret = "secret"
	Source     = "compat"
)

var definitions = []Definition{
	{Skill: "baidu-netdisk", Name: "BDPAN_BIN", Kind: KindPlain, Source: Source},
	{Skill: "baidu-netdisk", Name: "BDPAN_CONFIG_FILE", Kind: KindPlain, Source: Source},
	{Skill: "baidu-netdisk", Name: "BDPAN_HOME", Kind: KindPlain, Source: Source},
	{Skill: "bark", Name: "BARK_SERVER_URL", Kind: KindPlain, Source: Source},
	{Skill: "bark", Name: "BARK_BASE_URL", Kind: KindPlain, Source: Source},
	{Skill: "bark", Name: "BARK_URL", Kind: KindPlain, Source: Source},
	{Skill: "bark", Name: "BARK_ENV_FILE", Kind: KindPlain, Source: Source},
	{Skill: "bark", Name: "BARK_INSECURE_TLS", Kind: KindPlain, Source: Source},
	{Skill: "cloudsaver", Name: "CLOUDSAVER_BASE_URL", Kind: KindPlain, Source: Source},
	{Skill: "cloudsaver", Name: "CLOUDSAVER_ENV_FILE", Kind: KindPlain, Source: Source},
	{Skill: "cloudsaver", Name: "CLOUDSAVER_PASSWORD", Kind: KindSecret, Source: Source},
	{Skill: "cloudsaver", Name: "CLOUDSAVER_TOKEN", Kind: KindSecret, Source: Source},
	{Skill: "cloudsaver", Name: "CLOUDSAVER_TOKEN_FILE", Kind: KindPlain, Source: Source},
	{Skill: "cloudsaver", Name: "CLOUDSAVER_USERNAME", Kind: KindPlain, Source: Source},
	{Skill: "dida365-open-api", Name: "DIDA365_ACCESS_TOKEN", Kind: KindSecret, Source: Source},
	{Skill: "dida365-open-api", Name: "DIDA365_CLIENT_ID", Kind: KindPlain, Source: Source},
	{Skill: "dida365-open-api", Name: "DIDA365_CLIENT_SECRET", Kind: KindSecret, Source: Source},
	{Skill: "dida365-open-api", Name: "DIDA365_REDIRECT_URI", Kind: KindPlain, Source: Source},
	{Skill: "dida365-open-api", Name: "DIDA365_REGION", Kind: KindPlain, Source: Source},
	{Skill: "spotify-web-api", Name: "SPOTIFY_CLIENT_ID", Kind: KindPlain, Source: Source},
	{Skill: "spotify-web-api", Name: "SPOTIFY_REDIRECT_URI", Kind: KindPlain, Source: Source},
	{Skill: "spotify-web-api", Name: "SPOTIFY_SCOPES", Kind: KindPlain, Source: Source},
	{Skill: "douban-marks", Name: "DOUBAN_ENV_FILE", Kind: KindPlain, Source: Source},
	{Skill: "douban-marks", Name: "DOUBAN_UID", Kind: KindPlain, Source: Source},
	{Skill: "douban-marks", Name: "DOUBAN_USER_ID", Kind: KindPlain, Source: Source},
	{Skill: "weread-skills", Name: "WEREAD_API_KEY", Kind: KindSecret, Source: Source},
	{Skill: "openlist", Name: "OPENLIST_URL", Kind: KindPlain, Source: Source},
	{Skill: "openlist", Name: "OPENLIST_TOKEN", Kind: KindSecret, Source: Source},
	{Skill: "openlist", Name: "OPENLIST_SESSION_FILE", Kind: KindPlain, Source: Source},
	{Skill: "openlist", Name: "OPENLIST_INSECURE_TLS", Kind: KindPlain, Source: Source},
	{Skill: "rsshub", Name: "RSSHUB_BASE_URL", Kind: KindPlain, Source: Source},
	{Skill: "telegram-official", Name: "DOCK_DEVICE", Kind: KindPlain, Source: Source},
	{Skill: "telegram-official", Name: "TELEGRAM_CHAT_ID", Kind: KindPlain, Source: Source},
	{Skill: "telegram-official", Name: "TELEGRAM_ENV_FILE", Kind: KindPlain, Source: Source},
	{Skill: "telegram-official", Name: "TG_CHAT_ID", Kind: KindPlain, Source: Source},
	{Skill: "xiaohongshu-mcp", Name: "XIAOHONGSHU_CHROME_BIN", Kind: KindPlain, Source: Source},
	{Skill: "xiaohongshu-mcp", Name: "XIAOHONGSHU_COOKIE_FILE", Kind: KindPlain, Source: Source},
	{Skill: "xiaohongshu-mcp", Name: "XIAOHONGSHU_LAUNCH_AGENT", Kind: KindPlain, Source: Source},
	{Skill: "xiaohongshu-mcp", Name: "XIAOHONGSHU_MCP_URL", Kind: KindPlain, Source: Source},
}

func All() []Definition {
	return append([]Definition(nil), definitions...)
}

func ForSkill(skill string) []Definition {
	items := make([]Definition, 0)
	for _, def := range definitions {
		if def.Skill == skill {
			items = append(items, def)
		}
	}
	return items
}
