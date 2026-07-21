package httpx

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/uvwt/agentdock/internal/auth"
	"github.com/uvwt/agentdock/internal/config"
	"github.com/uvwt/agentdock/internal/mcp"
)

// registerConsole mounts the product settings Web UI (not a Goal task dashboard).
// This is the human control surface for MCP URL, tunnel, browser worker, and runtime status.
func registerConsole(mux *http.ServeMux, server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) {
	mux.HandleFunc("/console", consoleHandler(server, cfg, oauthStore))
	mux.HandleFunc("/console/", consoleHandler(server, cfg, oauthStore))
}

func consoleHandler(server *mcp.Server, cfg config.Config, oauthStore *auth.OAuthStore) http.HandlerFunc {
	authorizer := auth.Bearer{Token: cfg.AuthToken}
	authRequired := cfg.AuthRequired()
	return func(w http.ResponseWriter, r *http.Request) {
		if authRequired {
			staticOK := cfg.AuthToken != "" && authorizer.Authorized(r)
			oauthOK := authorizedOAuth(r, cfg, oauthStore)
			if !staticOK && !oauthOK {
				if tok := r.URL.Query().Get("token"); tok != "" && tok == cfg.AuthToken {
					// ok
				} else {
					setBearerChallenge(w, cfg, r, true)
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
			}
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		host := cfg.Host
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		mcpURL := fmt.Sprintf("http://%s:%d/mcp", host, cfg.Port)
		w.Header().Set("content-type", "text/html; charset=utf-8")
		w.Header().Set("cache-control", "no-store")
		page := strings.ReplaceAll(consoleHTML, "__MCP_URL__", mcpURL)
		page = strings.ReplaceAll(page, "__VERSION__", config.Version)
		page = strings.ReplaceAll(page, "__HOST__", host)
		page = strings.ReplaceAll(page, "__PORT__", fmt.Sprintf("%d", cfg.Port))
		_, _ = w.Write([]byte(page))
	}
}

// consoleHTML is the product control surface. Goal work still happens in ChatGPT web;
// this page only simplifies connection, tunnel, browser, and runtime settings.
const consoleHTML = `<!doctype html>
<html lang="zh-Hant" data-theme="system">
<head>
<meta charset="utf-8"/>
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover"/>
<title>AgentDock Console</title>
<style>
:root {
  --bg: #F2F2F7;
  --surface: #FFFFFF;
  --surface-2: #FFFFFF;
  --ink: #1C1C1E;
  --secondary: #8E8E93;
  --separator: rgba(60,60,67,0.12);
  --accent: #C45C26;
  --accent-contrast: #FFFFFF;
  --ok: #34C759;
  --warn: #FF9F0A;
  --bad: #FF3B30;
  --track-off: #E5E5EA;
  --shadow: 0 1px 0 rgba(0,0,0,0.02);
  --radius: 14px;
  --hit: 44px;
  --mono: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  --sans: -apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue", sans-serif;
  color-scheme: light;
}
html[data-theme="dark"] {
  --bg: #0B0B0F;
  --surface: #1C1C1E;
  --surface-2: #2C2C2E;
  --ink: #F5F5F7;
  --secondary: #8E8E93;
  --separator: rgba(84,84,88,0.45);
  --accent: #D97845;
  --accent-contrast: #1C1C1E;
  --ok: #30D158;
  --warn: #FFD60A;
  --bad: #FF453A;
  --track-off: #39393D;
  --shadow: 0 1px 0 rgba(255,255,255,0.04);
  color-scheme: dark;
}
@media (prefers-color-scheme: dark) {
  html[data-theme="system"] {
    --bg: #0B0B0F;
    --surface: #1C1C1E;
    --surface-2: #2C2C2E;
    --ink: #F5F5F7;
    --secondary: #8E8E93;
    --separator: rgba(84,84,88,0.45);
    --accent: #D97845;
    --accent-contrast: #1C1C1E;
    --ok: #30D158;
    --warn: #FFD60A;
    --bad: #FF453A;
    --track-off: #39393D;
    --shadow: 0 1px 0 rgba(255,255,255,0.04);
    color-scheme: dark;
  }
}
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; background: var(--bg); color: var(--ink); font-family: var(--sans); }
body { min-height: 100dvh; }
button, input, select { font: inherit; color: inherit; }
a { color: var(--accent); }
.topbar {
  position: sticky; top: 0; z-index: 20;
  backdrop-filter: saturate(180%) blur(16px);
  -webkit-backdrop-filter: saturate(180%) blur(16px);
  background: color-mix(in srgb, var(--bg) 82%, transparent);
  border-bottom: 1px solid var(--separator);
}
.topbar-inner {
  max-width: 1040px; margin: 0 auto; padding: 14px 16px 12px;
  display: flex; align-items: flex-end; justify-content: space-between; gap: 12px;
}
.large-title { font-size: 32px; font-weight: 700; letter-spacing: -0.02em; line-height: 1.1; margin: 0; }
.top-meta { display: flex; flex-wrap: wrap; gap: 8px; align-items: center; justify-content: flex-end; }
.seg {
  display: inline-flex; background: var(--surface-2); border-radius: 10px; padding: 2px; border: 1px solid var(--separator);
}
.seg button {
  border: 0; background: transparent; min-height: 32px; padding: 0 10px; border-radius: 8px; color: var(--secondary); cursor: pointer;
}
.seg button[aria-pressed="true"] { background: var(--surface); color: var(--ink); box-shadow: var(--shadow); }
.wrap { max-width: 1040px; margin: 0 auto; padding: 18px 16px 48px; }
.layout { display: grid; grid-template-columns: 1fr; gap: 18px; }
@media (min-width: 960px) {
  .layout { grid-template-columns: minmax(0, 1.4fr) minmax(280px, 0.8fr); align-items: start; }
  .rail { position: sticky; top: 86px; }
}
.section-label {
  margin: 18px 4px 8px; font-size: 13px; font-weight: 600; color: var(--secondary);
  text-transform: uppercase; letter-spacing: 0.04em;
}
.group {
  background: var(--surface); border-radius: var(--radius); overflow: hidden;
  box-shadow: var(--shadow); border: 1px solid var(--separator);
}
.row {
  display: flex; align-items: center; justify-content: space-between; gap: 12px;
  min-height: var(--hit); padding: 10px 14px; position: relative;
}
.row + .row::before {
  content: ""; position: absolute; left: 14px; right: 0; top: 0; height: 1px; background: var(--separator);
}
.row .label { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.row .label b { font-size: 16px; font-weight: 500; }
.row .label span { font-size: 12px; color: var(--secondary); line-height: 1.35; }
.row .value { color: var(--secondary); font-size: 15px; text-align: right; max-width: 58%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row .value.mono, .mono { font-family: var(--mono); font-size: 12px; }
.row-actions { display: flex; gap: 8px; flex-wrap: wrap; justify-content: flex-end; align-items: center; }
.hint { color: var(--secondary); font-size: 12px; line-height: 1.45; padding: 0 14px 12px; }
.footer-note { color: var(--secondary); font-size: 12px; padding: 10px 4px 0; }

/* iOS switch */
.switch { position: relative; width: 51px; height: 31px; flex: 0 0 auto; }
.switch input { opacity: 0; width: 0; height: 0; position: absolute; }
.switch .track {
  position: absolute; inset: 0; border-radius: 999px; background: var(--track-off);
  transition: background .2s cubic-bezier(.2,.8,.2,1); cursor: pointer;
}
.switch .track::after {
  content: ""; position: absolute; top: 2px; left: 2px; width: 27px; height: 27px; border-radius: 50%;
  background: #fff; box-shadow: 0 1px 2px rgba(0,0,0,.18), 0 2px 6px rgba(0,0,0,.12);
  transition: transform .2s cubic-bezier(.2,.8,.2,1);
}
.switch input:checked + .track { background: var(--accent); }
.switch input:checked + .track::after { transform: translateX(20px); }
.switch input:focus-visible + .track { outline: 3px solid color-mix(in srgb, var(--accent) 30%, transparent); outline-offset: 2px; }

.btn {
  appearance: none; border: 0; border-radius: 12px; min-height: 36px; padding: 0 12px; cursor: pointer;
  background: color-mix(in srgb, var(--ink) 6%, var(--surface)); color: var(--ink);
}
.btn:active { transform: scale(0.98); }
.btn.primary { background: var(--accent); color: var(--accent-contrast); font-weight: 600; }
.btn.danger { background: color-mix(in srgb, var(--bad) 14%, var(--surface)); color: var(--bad); font-weight: 600; }
.btn.ghost { background: transparent; color: var(--accent); }
.input, .select {
  width: 100%; min-height: 40px; border-radius: 10px; border: 1px solid var(--separator);
  background: color-mix(in srgb, var(--bg) 70%, var(--surface)); padding: 8px 10px; outline: none;
}
.input:focus, .select:focus { border-color: color-mix(in srgb, var(--accent) 50%, var(--separator)); box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 22%, transparent); }
.field-block { padding: 8px 14px 12px; display: grid; gap: 8px; }
.tabs { display: flex; gap: 6px; padding: 10px 12px 0; flex-wrap: wrap; }
.tab {
  border: 0; background: transparent; color: var(--secondary); min-height: 32px; padding: 0 10px;
  border-radius: 999px; cursor: pointer;
}
.tab[aria-selected="true"] { background: color-mix(in srgb, var(--accent) 14%, var(--surface)); color: var(--accent); font-weight: 600; }
.panel { display: none; padding-top: 4px; }
.panel.active { display: block; }
.pill {
  display: inline-flex; align-items: center; gap: 6px; min-height: 26px; padding: 0 10px; border-radius: 999px;
  background: color-mix(in srgb, var(--ink) 5%, var(--surface)); color: var(--secondary); font-size: 12px;
}
.pill .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--secondary); }
.pill.ok { color: var(--ok); background: color-mix(in srgb, var(--ok) 12%, var(--surface)); }
.pill.ok .dot { background: var(--ok); }
.pill.warn { color: var(--warn); background: color-mix(in srgb, var(--warn) 14%, var(--surface)); }
.pill.warn .dot { background: var(--warn); }
.pill.bad { color: var(--bad); background: color-mix(in srgb, var(--bad) 12%, var(--surface)); }
.pill.bad .dot { background: var(--bad); }
.codebox {
  font-family: var(--mono); font-size: 12px; word-break: break-all; background: color-mix(in srgb, var(--bg) 80%, var(--surface));
  border: 1px solid var(--separator); border-radius: 10px; padding: 10px; color: var(--ink);
}
.list { display: grid; gap: 0; }
.goal-item { padding: 12px 14px; border-top: 1px solid var(--separator); }
.goal-item:first-child { border-top: 0; }
.goal-item b { display: block; font-size: 14px; margin-bottom: 4px; }
.goal-item .meta { color: var(--secondary); font-size: 12px; font-family: var(--mono); }
#toast {
  position: fixed; left: 50%; bottom: 24px; transform: translateX(-50%) translateY(20px);
  background: color-mix(in srgb, var(--ink) 92%, #000); color: var(--bg); padding: 10px 14px; border-radius: 12px;
  opacity: 0; pointer-events: none; transition: opacity .18s ease, transform .18s ease; z-index: 50; font-size: 14px;
  max-width: min(92vw, 420px);
}
#toast.show { opacity: 1; transform: translateX(-50%) translateY(0); }
.stack { display: grid; gap: 18px; }
.kv-grid { display: grid; grid-template-columns: 110px 1fr; gap: 6px 10px; padding: 12px 14px; font-size: 13px; }
.kv-grid .k { color: var(--secondary); }
.kv-grid .v { min-width: 0; overflow: hidden; text-overflow: ellipsis; }
@media (max-width: 640px) {
  .large-title { font-size: 28px; }
  .row .value { max-width: 48%; }
}
</style>
</head>
<body>
  <header class="topbar">
    <div class="topbar-inner">
      <div>
        <div class="footer-note" style="padding:0 0 4px">AgentDock</div>
        <h1 class="large-title">Console</h1>
      </div>
      <div class="top-meta">
        <span class="pill" id="runtimePill"><span class="dot"></span><span>runtime</span></span>
        <span class="pill" id="authPill"><span class="dot"></span><span>auth</span></span>
        <div class="seg" role="group" aria-label="Theme">
          <button type="button" data-theme-set="system" aria-pressed="true">System</button>
          <button type="button" data-theme-set="light" aria-pressed="false">Light</button>
          <button type="button" data-theme-set="dark" aria-pressed="false">Dark</button>
        </div>
        <button class="btn ghost" id="btnRefresh" type="button">重新整理</button>
      </div>
    </div>
  </header>

  <main class="wrap">
    <div class="layout">
      <div class="stack">
        <div>
          <div class="section-label">狀態</div>
          <div class="group">
            <div class="kv-grid">
              <div class="k">Version</div><div class="v mono" id="kVersion">—</div>
              <div class="k">Browser</div><div class="v" id="kBrowser">—</div>
              <div class="k">Tools</div><div class="v" id="kTools">—</div>
              <div class="k">Auth</div><div class="v" id="kAuth">—</div>
              <div class="k">Home</div><div class="v mono" id="sysHome">—</div>
              <div class="k">CWD</div><div class="v mono" id="sysDir">—</div>
            </div>
          </div>
        </div>

        <div>
          <div class="section-label">01 · MCP 接入口</div>
          <div class="group">
            <div class="tabs" role="tablist">
              <button class="tab" role="tab" data-tab="local" aria-selected="true" type="button">本機</button>
              <button class="tab" role="tab" data-tab="cloudflare" aria-selected="false" type="button">Cloudflare</button>
              <button class="tab" role="tab" data-tab="lan" aria-selected="false" type="button">LAN</button>
              <button class="tab" role="tab" data-tab="custom" aria-selected="false" type="button">Custom</button>
            </div>

            <div class="panel active" id="panel-local" role="tabpanel">
              <div class="row"><div class="label"><b>本機 MCP</b><span>僅本機程序可連</span></div><span class="pill" id="localPill"><span class="dot"></span><span>ready</span></span></div>
              <div class="field-block">
                <div class="codebox" id="mcpLocal">__MCP_URL__</div>
                <div class="row-actions"><button class="btn" type="button" data-copy="mcpLocal">複製</button></div>
              </div>
            </div>

            <div class="panel" id="panel-cloudflare" role="tabpanel">
              <div class="row"><div class="label"><b>Cloudflare Quick Tunnel</b><span>臨時公開 HTTPS</span></div><span class="pill" id="cfPill"><span class="dot"></span><span>idle</span></span></div>
              <div class="field-block">
                <div class="row-actions">
                  <button class="btn primary" id="btnCfStart" type="button">啟動 Tunnel</button>
                  <button class="btn" id="btnCfStop" type="button">停止</button>
                </div>
                <div class="codebox" id="mcpCf">尚未啟動</div>
                <div class="row-actions"><button class="btn" type="button" data-copy="mcpCf">複製</button></div>
              </div>
            </div>

            <div class="panel" id="panel-lan" role="tabpanel">
              <div class="row"><div class="label"><b>LAN 入口</b><span>區網 HTTP，不適合雲端 ChatGPT</span></div><span class="pill" id="lanPill"><span class="dot"></span><span>idle</span></span></div>
              <div class="field-block">
                <div class="row-actions"><button class="btn primary" id="btnLanStart" type="button">啟用 LAN</button></div>
                <div class="codebox" id="mcpLan">尚未啟用</div>
                <div class="row-actions"><button class="btn" type="button" data-copy="mcpLan">複製</button></div>
              </div>
            </div>

            <div class="panel" id="panel-custom" role="tabpanel">
              <div class="row"><div class="label"><b>自訂域名</b><span>反代或 Named Tunnel</span></div><span class="pill" id="customPill"><span class="dot"></span><span>idle</span></span></div>
              <div class="field-block">
                <label class="footer-note" for="customUrlInput" style="padding:0">公開基址</label>
                <input class="input" id="customUrlInput" placeholder="https://agentdock.example.com"/>
                <label class="footer-note" for="customTokenInput" style="padding:0">Tunnel Token（可選）</label>
                <input class="input" id="customTokenInput" placeholder="eyJ…（可留空）" autocomplete="off"/>
                <div class="row-actions">
                  <button class="btn primary" id="btnCustomStart" type="button">儲存並啟用</button>
                  <button class="btn" id="btnCustomClear" type="button">清除</button>
                </div>
                <div class="codebox" id="mcpCustom">尚未設定</div>
                <div class="row-actions"><button class="btn" type="button" data-copy="mcpCustom">複製</button></div>
                <div class="footer-note" id="customHint" style="padding:0">反代到本機 127.0.0.1:__PORT__ 後，MCP 為 https://你的域名/mcp</div>
              </div>
            </div>
          </div>
        </div>

        <div>
          <div class="section-label">02 · ChatGPT Worker</div>
          <div class="group">
            <div class="row">
              <div class="label"><b>Worker</b><span id="workerProfile">profile</span></div>
              <span class="pill" id="workerPill"><span class="dot"></span><span>worker</span></span>
            </div>
            <div class="row">
              <div class="label"><b>打開 ChatGPT</b><span>專用 profile 視窗</span></div>
              <div class="row-actions"><button class="btn primary" id="btnOpenChatGPT2" type="button">打開</button><button class="btn" id="btnForceRotate" type="button">新對話</button></div>
            </div>
            <div class="row">
              <div class="label"><b>Auto-wake</b><span>awaiting_reasoning 時自動喚醒</span></div>
              <label class="switch" title="Auto-wake">
                <input type="checkbox" id="toggleAutoWake"/>
                <span class="track"></span>
              </label>
            </div>
            <div class="row">
              <div class="label"><b>自動允許工具</b><span>自動點擊 Allow / 允許（如 Svananda）</span></div>
              <label class="switch" title="Auto-approve tools">
                <input type="checkbox" id="toggleAutoApprove"/>
                <span class="track"></span>
              </label>
            </div>
            <div class="kv-grid">
              <div class="k">Bound</div><div class="v mono" id="workerBoundURL">—</div>
              <div class="k">Last wake</div><div class="v mono" id="workerLastWake">—</div>
              <div class="k">Last error</div><div class="v mono" id="workerLastError">—</div>
              <div class="k">自動允許</div><div class="v" id="workerAutoApprove">—</div>
            </div>
            <div class="hint" id="workerHint">第一次請在專用視窗登入 ChatGPT。預設不會自動允許工具。</div>
          </div>
        </div>

        <div>
          <div class="section-label">03 · Goal loop</div>
          <div class="group">
            <div class="row">
              <div class="label"><b>Orchestrator</b><span id="orchPhase">idle</span></div>
              <span class="pill" id="orchPill"><span class="dot"></span><span>orch</span></span>
            </div>
            <div class="field-block">
              <input class="input mono" id="orchGoalInput" placeholder="goal_…"/>
              <div class="row-actions">
                <button class="btn" id="btnOrchRefresh" type="button">重新整理</button>
                <button class="btn danger" id="btnOrchStop" type="button">停止</button>
              </div>
            </div>
            <div class="kv-grid">
              <div class="k">Goal</div><div class="v mono" id="orchGoalID">—</div>
              <div class="k">Status</div><div class="v" id="orchGoalStatus">—</div>
              <div class="k">Phase</div><div class="v" id="orchPhaseText">—</div>
              <div class="k">Ticks / no-commit</div><div class="v mono" id="orchTicks">—</div>
              <div class="k">Bound thread</div><div class="v mono" id="orchBound">—</div>
              <div class="k">Last message</div><div class="v" id="orchMsg">—</div>
              <div class="k">Last error</div><div class="v" id="orchErr">—</div>
            </div>
          </div>
        </div>

        <div>
          <div class="section-label">04 · 工作目錄</div>
          <div class="group">
            <div class="kv-grid">
              <div class="k">目前 CWD</div><div class="v mono" id="cwdAbs">—</div>
              <div class="k">顯示名</div><div class="v mono" id="cwdDisplay">—</div>
              <div class="k">安裝預設</div><div class="v mono" id="cwdInstallDefault">—</div>
            </div>
            <div class="field-block">
              <input class="input" id="cwdInput" placeholder="/Users/you/Projects/app 或 ~/Projects/app"/>
              <div class="row-actions">
                <button class="btn primary" id="btnSetCwd" type="button">套用</button>
                <button class="btn" id="btnResetCwd" type="button">重置</button>
              </div>
            </div>
            <div class="hint" id="cwdHint">相對路徑從這裡開始；絕對與 ~/ 仍可用。</div>
          </div>
        </div>

        <div>
          <div class="section-label">05 · 認證與安全</div>
          <div class="group">
            <div class="row"><div class="label"><b>Auth</b><span id="authRequired">—</span></div><span class="pill" id="oauthPill"><span class="dot"></span><span>oauth</span></span></div>
            <div class="kv-grid">
              <div class="k">Token 已設</div><div class="v" id="authTokenSet">—</div>
              <div class="k">OAuth</div><div class="v" id="oauthEnabled">—</div>
              <div class="k">OAuth URL</div><div class="v mono" id="oauthURL">—</div>
              <div class="k">Bind</div><div class="v mono" id="authBind">—</div>
            </div>
            <div class="hint" id="authHint">公網 Tunnel 建議啟用 Token 或 OAuth。</div>
            <div class="field-block">
              <div class="row-actions">
                <button class="btn" id="btnCopyAuthHelp" type="button">複製設定範例</button>
                <a class="btn ghost" id="oauthLink" href="/oauth/authorize" target="_blank" rel="noopener" style="text-decoration:none;display:inline-flex;align-items:center">OAuth 授權頁</a>
              </div>
            </div>
          </div>
        </div>
      </div>

      <aside class="rail stack">
        <div>
          <div class="section-label">Quick start</div>
          <div class="group">
            <div class="hint" style="padding-top:12px">1. 複製 MCP URL 到 ChatGPT Connectors<br/>2. 打開專用 ChatGPT 並登入<br/>3. 需要時開啟「自動允許工具」toggle<br/>4. 在對話建立 Goal 並 orchestrate_start</div>
            <div class="field-block">
              <div class="row-actions"><button class="btn primary" id="btnOpenChatGPT" type="button">打開 ChatGPT 視窗</button></div>
            </div>
          </div>
        </div>
        <div>
          <div class="section-label">Live goals</div>
          <div class="group">
            <div class="list" id="goalList"><div class="hint">載入中…</div></div>
          </div>
        </div>
        <div class="footer-note">v__VERSION__ · __HOST__:__PORT__ · design: iOS Settings</div>
      </aside>
    </div>
  </main>
  <div id="toast"></div>
<script>

const state = { tunnelMode: 'cloudflare', tab: 'local' };
const $ = (id) => document.getElementById(id);

function toast(msg){
  const el = $('toast');
  el.textContent = msg;
  el.classList.add('show');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => el.classList.remove('show'), 2200);
}

function setPill(el, kind, label){
  if (!el) return;
  el.className = 'pill' + (kind ? (' ' + kind) : '');
  const dot = el.querySelector('.dot');
  const textNodes = [...el.childNodes].filter(n => n.nodeType === 3 || (n.nodeType===1 && !n.classList.contains('dot')));
  // rebuild label span if present
  let span = el.querySelector('span:not(.dot)');
  if (!span) {
    span = document.createElement('span');
    el.appendChild(span);
  }
  if (!dot) {
    const d = document.createElement('span'); d.className='dot'; el.insertBefore(d, span);
  }
  span.textContent = label || '';
}

async function api(path, opts={}){
  const res = await fetch(path, {
    headers: { 'content-type': 'application/json', ...(opts.headers||{}) },
    ...opts,
  });
  const text = await res.text();
  let data = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = { raw: text }; }
  if (!res.ok) {
    const msg = (data && (data.error || data.message)) || res.statusText || ('HTTP ' + res.status);
    throw new Error(msg);
  }
  return data;
}

function pickTunnel(tunnelPayload){
  return (tunnelPayload && (tunnelPayload.tunnel || tunnelPayload)) || {};
}

function setTheme(mode){
  const m = mode || localStorage.getItem('agentdock.console.theme') || 'system';
  document.documentElement.setAttribute('data-theme', m);
  localStorage.setItem('agentdock.console.theme', m);
  document.querySelectorAll('[data-theme-set]').forEach(btn => {
    btn.setAttribute('aria-pressed', btn.getAttribute('data-theme-set') === m ? 'true' : 'false');
  });
}

function selectTab(tab){
  state.tab = tab;
  document.querySelectorAll('.tab').forEach(btn => {
    btn.setAttribute('aria-selected', btn.getAttribute('data-tab') === tab ? 'true' : 'false');
  });
  document.querySelectorAll('.panel').forEach(p => p.classList.remove('active'));
  const panel = document.getElementById('panel-' + tab);
  if (panel) panel.classList.add('active');
}

function applyWorkerToggles(w){
  const aw = !!w.auto_wake;
  const aa = !!w.auto_approve_tools;
  const tAw = $('toggleAutoWake');
  const tAa = $('toggleAutoApprove');
  if (tAw) tAw.checked = aw;
  if (tAa) tAa.checked = aa;
  if ($('workerAutoApprove')) $('workerAutoApprove').textContent = aa ? '開（會自動點允許）' : '關（需手動允許）';
}

async function refresh(){
  try {
    const [status, tunnel, worker, goals] = await Promise.all([
      api('/internal/runtime/status'),
      api('/internal/runtime/tunnel'),
      api('/internal/runtime/chatgpt/worker'),
      api('/internal/runtime/goals?limit=12').catch(() => ({ goals: [] })),
    ]);

    $('kVersion').textContent = status.version || '—';
    $('kBrowser').textContent = status.browser_enabled ? 'On' : 'Off';
    $('kTools').textContent = status.tool_count ?? '—';
    $('kAuth').textContent = status.auth_enabled ? 'On' : 'Off';
    $('sysHome').textContent = status.agentdock_home || '—';
    $('sysDir').textContent = status.default_cwd || status.agentdock_default_dir || '—';
    setPill($('runtimePill'), 'ok', 'online');

    $('cwdAbs').textContent = status.default_cwd || status.agentdock_default_dir || '—';
    $('cwdDisplay').textContent = status.default_cwd_display || '.';
    $('cwdInstallDefault').textContent = status.agentdock_default_dir || '—';

    const authOn = !!status.auth_enabled;
    const tokenSet = !!status.auth_token_configured;
    const oauthOn = !!status.oauth_enabled;
    setPill($('authPill'), authOn ? 'ok' : 'warn', authOn ? 'auth on' : 'open');
    setPill($('oauthPill'), oauthOn ? 'ok' : '', oauthOn ? 'oauth on' : 'oauth off');
    $('authRequired').textContent = authOn ? '需要 Bearer 或 OAuth' : '回環可匿名';
    $('authTokenSet').textContent = tokenSet ? '是' : '否';
    $('oauthEnabled').textContent = oauthOn ? '已啟用' : '未啟用';
    $('oauthURL').textContent = status.oauth_server_url || '（未設定）';
    $('authBind').textContent = (status.host || '127.0.0.1') + ':' + (status.port || '8765');
    if (!authOn && status.host && status.host !== '127.0.0.1' && status.host !== 'localhost') {
      $('authHint').textContent = '目前綁定非回環且未強制認證，公網暴露風險高。';
    } else if (!authOn) {
      $('authHint').textContent = '本機回環可匿名。公開 MCP 時請啟用 Token 或 OAuth。';
    } else {
      $('authHint').textContent = '認證已啟用。客戶端需 Bearer token 或 OAuth。';
    }
    $('oauthLink').style.display = oauthOn ? 'inline-flex' : 'none';

    const t = pickTunnel(tunnel);
    const tState = (t.state || t.mode || 'disabled');
    const mcpURL = t.mcp_url || t.public_url || '';
    // local always
    if (!$('mcpLocal').dataset.locked) {
      // keep server provided replacement
    }
    // cloudflare / lan / custom display
    const mode = (t.mode || (tunnel && tunnel.config && tunnel.config.mode) || '').toLowerCase();
    if (mode === 'cloudflare' || mode === 'quick' || t.provider === 'cloudflare') {
      $('mcpCf').textContent = mcpURL || (t.public_url ? (t.public_url.replace(/\/$/, '') + '/mcp') : '啟動中…');
      setPill($('cfPill'), t.running || t.state==='running' ? 'ok' : '', t.running || t.state==='running' ? 'running' : (t.state || 'idle'));
    }
    if (mode === 'lan') {
      $('mcpLan').textContent = mcpURL || '尚未啟用';
      setPill($('lanPill'), mcpURL ? 'ok' : '', mcpURL ? 'ready' : 'idle');
    }
    if (mode === 'custom') {
      $('mcpCustom').textContent = mcpURL || '尚未設定';
      setPill($('customPill'), mcpURL ? 'ok' : '', mcpURL ? 'ready' : 'idle');
      if (tunnel && tunnel.config && tunnel.config.custom_url && !$('customUrlInput').value) {
        $('customUrlInput').value = tunnel.config.custom_url;
      }
    }
    // generic fallback fill
    if (mcpURL) {
      if (mode.includes('cloud') || !mode) $('mcpCf').textContent = mcpURL;
      if (mode === 'lan') $('mcpLan').textContent = mcpURL;
      if (mode === 'custom') $('mcpCustom').textContent = mcpURL;
    }

    const w = worker.worker || worker;
    const wKind = w.waking ? 'warn' : (w.browser_enabled ? 'ok' : 'warn');
    setPill($('workerPill'), wKind, w.waking ? 'waking' : (w.browser_enabled ? 'ready' : 'browser off'));
    $('workerProfile').textContent = w.profile_id ? ('profile: ' + w.profile_id) : 'profile';
    applyWorkerToggles(w);
    const last = w.last || {};
    $('workerBoundURL').textContent = last.conversation_id ? ('c/' + last.conversation_id) : '—';
    $('workerLastWake').textContent = last.goal_id ? (last.goal_id + (last.rotated ? ' · rotated' : ' · same thread')) : '—';
    $('workerLastError').textContent = w.last_error || '—';
    if (w.last_error) $('workerHint').textContent = w.last_error;
    else $('workerHint').textContent = '第一次請在專用視窗登入 ChatGPT。預設不自動允許工具。';

    const items = goals.goals || [];
    const list = $('goalList');
    if (!items.length) {
      list.innerHTML = '<div class="hint">尚無 Goal</div>';
    } else {
      list.innerHTML = items.map(g => {
        const id = g.goal_id || g.ID || '';
        const st = g.status || '';
        const title = g.title || id;
        return '<div class="goal-item"><b>${title}</b><div class="meta">${id} · ${st}</div></div>';
      }).join('');
    }

    let focusID = ($('orchGoalInput').value || '').trim();
    if (!focusID && items.length) focusID = items[0].goal_id || items[0].ID || '';
    if (focusID && !$('orchGoalInput').value) $('orchGoalInput').value = focusID;
    if (focusID) {
      try {
        const ost = await api('/internal/runtime/goals/' + encodeURIComponent(focusID) + '/orchestrate_status', { method:'POST', body:'{}' });
        const o = (ost && ost.orchestrator) || {};
        const g0 = items.find(x => (x.goal_id||x.ID) === focusID) || {};
        $('orchGoalID').textContent = focusID;
        $('orchGoalStatus').textContent = o.goal_status || g0.status || '—';
        $('orchPhaseText').textContent = o.phase || 'idle';
        $('orchPhase').textContent = o.phase || 'idle';
        $('orchTicks').textContent = (o.ticks||0) + ' / ' + (o.no_commit_streak||0);
        $('orchBound').textContent = g0.worker_conversation_url || last.conversation_id || '—';
        $('orchMsg').textContent = o.last_message || '—';
        $('orchErr').textContent = o.last_error || '—';
        const ok = !!o.running;
        setPill($('orchPill'), ok ? 'ok' : (o.phase==='blocked'||o.phase==='error' ? 'bad' : ''), ok ? 'running' : (o.phase||'idle'));
      } catch (e) {
        $('orchMsg').textContent = String(e.message || e);
      }
    }
  } catch (e) {
    setPill($('runtimePill'), 'bad', 'offline');
    toast(String(e.message || e));
  }
}

// Tabs
document.querySelectorAll('.tab').forEach(btn => {
  btn.addEventListener('click', () => selectTab(btn.getAttribute('data-tab')));
});
document.querySelectorAll('[data-theme-set]').forEach(btn => {
  btn.addEventListener('click', () => setTheme(btn.getAttribute('data-theme-set')));
});
document.querySelectorAll('[data-copy]').forEach(btn => {
  btn.addEventListener('click', async () => {
    const id = btn.getAttribute('data-copy');
    const el = $(id);
    const text = el ? el.textContent : '';
    try { await navigator.clipboard.writeText(text); toast('已複製'); }
    catch { toast('複製失敗'); }
  });
});

async function setToggle(pathBody, checkbox, failureLabel){
  const next = checkbox.checked;
  try {
    await api('/internal/runtime/chatgpt/worker', { method:'POST', body: JSON.stringify(pathBody(next)) });
    refresh();
  } catch (e) {
    checkbox.checked = !next;
    toast((failureLabel||'更新失敗') + ': ' + (e.message||e));
  }
}

$('toggleAutoWake').addEventListener('change', () => setToggle(v => ({ auto_wake: v }), $('toggleAutoWake'), 'Auto-wake'));
$('toggleAutoApprove').addEventListener('change', () => setToggle(v => ({ auto_approve_tools: v }), $('toggleAutoApprove'), '自動允許工具'));

async function openChatGPT(){
  try {
    await api('/internal/runtime/chatgpt/worker', { method:'POST', body: JSON.stringify({ action: 'open' }) });
    toast('已請求打開 ChatGPT');
    refresh();
  } catch (e) { toast(String(e.message||e)); }
}
$('btnOpenChatGPT2').addEventListener('click', openChatGPT);
$('btnOpenChatGPT').addEventListener('click', openChatGPT);
$('btnForceRotate').addEventListener('click', async () => {
  try {
    await api('/internal/runtime/chatgpt/worker', { method:'POST', body: JSON.stringify({ action: 'force_rotate' }) });
    toast('已設定：下次 wake 開新對話');
    refresh();
  } catch (e) { toast(String(e.message||e)); }
});

$('btnCfStart').addEventListener('click', async () => {
  try {
    setPill($('cfPill'), 'warn', 'starting');
    const res = await api('/internal/runtime/tunnel/start', { method:'POST', body: JSON.stringify({ mode: 'cloudflare' }) });
    toast(res.ok === false ? (res.error || '啟動失敗') : 'Cloudflare tunnel 已啟動');
    refresh();
  } catch (e) { toast(String(e.message||e)); refresh(); }
});
$('btnCfStop').addEventListener('click', async () => {
  try { await api('/internal/runtime/tunnel/stop', { method:'POST', body: '{}' }); toast('已停止 tunnel'); refresh(); }
  catch (e) { toast(String(e.message||e)); }
});
$('btnLanStart').addEventListener('click', async () => {
  try {
    await api('/internal/runtime/tunnel/start', { method:'POST', body: JSON.stringify({ mode: 'lan' }) });
    toast('LAN 入口已啟用'); refresh();
  } catch (e) { toast(String(e.message||e)); }
});
$('btnCustomStart').addEventListener('click', async () => {
  try {
    const custom_url = ($('customUrlInput').value || '').trim();
    const tunnel_token = ($('customTokenInput').value || '').trim();
    await api('/internal/runtime/tunnel/start', { method:'POST', body: JSON.stringify({ mode: 'custom', custom_url, tunnel_token }) });
    toast('自訂域名已儲存並啟用'); refresh();
  } catch (e) { toast(String(e.message||e)); }
});
$('btnCustomClear').addEventListener('click', async () => {
  try {
    await api('/internal/runtime/tunnel/start', { method:'POST', body: JSON.stringify({ mode: 'loopback', clear_custom_url: true }) });
    $('customUrlInput').value = '';
    $('customTokenInput').value = '';
    toast('已清除自訂域名'); refresh();
  } catch (e) { toast(String(e.message||e)); }
});

$('btnSetCwd').addEventListener('click', async () => {
  try {
    await api('/internal/runtime/workspace', { method:'POST', body: JSON.stringify({ path: ($('cwdInput').value||'').trim() }) });
    toast('已更新 CWD'); refresh();
  } catch (e) { toast(String(e.message||e)); }
});
$('btnResetCwd').addEventListener('click', async () => {
  try {
    await api('/internal/runtime/workspace', { method:'POST', body: JSON.stringify({ reset: true }) });
    toast('已重置 CWD'); refresh();
  } catch (e) { toast(String(e.message||e)); }
});

$('btnCopyAuthHelp').addEventListener('click', async () => {
  const sample = [
    'export AGENTDOCK_AUTH_TOKEN=...',
    'export AGENTDOCK_OAUTH_ENABLED=true',
    'export AGENTDOCK_SERVER_URL=https://your.example.com',
    'export AGENTDOCK_OAUTH_PASSWORD=...',
    'export AGENTDOCK_CHATGPT_AUTO_APPROVE_TOOLS=false'
  ].join('\n');
  try { await navigator.clipboard.writeText(sample); toast('已複製設定範例'); }
  catch { toast('複製失敗'); }
});

$('btnOrchRefresh').addEventListener('click', () => refresh());
$('btnOrchStop').addEventListener('click', async () => {
  const id = ($('orchGoalInput').value || '').trim();
  if (!id) { toast('請先填 goal id'); return; }
  try {
    await api('/internal/runtime/goals/' + encodeURIComponent(id) + '/orchestrate_stop', { method:'POST', body:'{}' });
    toast('orchestrator stopped');
    refresh();
  } catch (e) { toast(String(e.message||e)); }
});

$('btnRefresh').addEventListener('click', refresh);
setTheme(localStorage.getItem('agentdock.console.theme') || 'system');
selectTab('local');
refresh();
setInterval(refresh, 5000);

</script>
</body>
</html>
`
