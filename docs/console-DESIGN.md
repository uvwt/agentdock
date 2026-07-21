# Design System: AgentDock Console

## 1. Visual Theme & Atmosphere
An iOS Settings–inspired control surface for local agent infrastructure: calm, precise, and hardware-adjacent rather than “AI product.” Atmosphere is **daily-app balanced density (5)**, **predictable-but-offset structure (4)**, and **fluid restrained motion (5)**. Surfaces feel like frosted glass and grouped inset lists, not marketing cards. The mood is quiet utility — like configuring a device in a dim room with one warm copper indicator light.

## 2. Color Palette & Roles
### Light
- **Canvas Mist** (#F2F2F7) — page background (iOS grouped background)
- **Surface Porcelain** (#FFFFFF) — inset group / card fill
- **Ink Near-Black** (#1C1C1E) — primary labels
- **Secondary Label** (#8E8E93) — captions, hints, metadata
- **Separator Hairline** (rgba(60,60,67,0.12)) — row dividers
- **System Blue** (#0A84FF is dark; light uses **#007AFF**) — interactive links only sparingly
- **Copper Signal** (#C45C26) — single accent for primary CTAs and active toggles (sat < 80%)
- **Success Green** (#34C759) — healthy state pills
- **Warning Amber** (#FF9F0A) — caution
- **Danger Rose** (#FF3B30) — errors / stop

### Dark
- **Canvas Void** (#000000 avoided) use **#0B0B0F** — page background
- **Surface Elevated** (#1C1C1E) — groups
- **Surface Raised** (#2C2C2E) — nested chips / inputs
- **Ink Primary** (#F5F5F7) — primary text
- **Secondary Label** (#8E8E93) — secondary text
- **Separator** (rgba(84,84,88,0.45)) — hairlines
- **Copper Signal** (#D pen: use **#D97845**) — accent on dark
- Same semantic green/amber/rose as light for state recognition

**Rule:** One accent only (Copper). No purple neon, no multi-hue gradients on buttons.

## 3. Typography Rules
- **UI Sans:** `SF Pro Text` stack → `-apple-system, BlinkMacSystemFont, "Segoe UI", "Helvetica Neue", sans-serif`
- **Mono:** `SF Mono, ui-monospace, Menlo, monospace` for URLs, goal IDs, ports, paths
- **Display size:** large title ~34px / 700; section header ~20px / 600; body 15–17px / 400; caption 12–13px / 400
- **Tracking:** slightly tight on large titles (-0.02em); body normal
- **Banned:** Inter, Space Grotesk, generic serif in this console, emoji decoration

## 4. Component Stylings
### Navigation
- Sticky top bar with large title “Console” + compact status cluster (version, browser, auth)
- Right: theme segmented control (Light / Dark / System)

### Grouped Lists (iOS Settings pattern)
- Rounded continuous groups (radius 12–14px) on mist/void canvas
- Rows: 44–52px min height, label left, value/control right
- Hairline separators inset from leading edge (not full-bleed when icon present)

### Toggles
- iOS-style switch for boolean runtime flags:
  - Auto-wake
  - **Auto-approve tools** (primary new control)
- Track off: #E5E5EA (light) / #39393D (dark)
- Track on: Copper Signal
- Thumb: white circle with soft shadow; 31×18 track; 44px hit target
- Instant optimistic UI + toast on failure revert

### Buttons
- Primary: Copper fill, white label, radius 12, no glow
- Secondary: translucent fill / outline
- Destructive: Danger Rose text or fill for Stop
- Active: scale(0.98) / translateY(0.5px)

### Pills / Status
- Compact capsules with 6px dot + label; color encodes ok/warn/bad/idle

### Inputs
- Filled inset fields inside groups; label above; mono for URLs
- Focus ring: 3px copper at 25% opacity

### Loaders
- Skeleton shimmer matching row heights; no generic circular spinner as primary

## 5. Layout Principles
- Max width 980–1040px centered; mobile single column
- **Two-pane desktop:** main stack (Connection → ChatGPT/Worker → Goal loop → Workspace → Auth) left; sticky “Quick start + Live goals” rail right on ≥960px
- Avoid equal 3-card feature rows
- Generous vertical rhythm: 20–28px between groups
- Code blocks for MCP URLs full-width inside group with copy affordance

## 6. Motion & Interaction
- Theme crossfade 180ms ease
- Toggle thumb springy CSS transition (transform 200ms cubic-bezier(0.2, 0.8, 0.2, 1))
- Row press opacity 0.7 briefly
- Refresh updates without full-page flash
- Animate only opacity/transform

## 7. Anti-Patterns (Banned)
- No purple/AI neon gradients
- No Inter / Space Grotesk
- No pure black text on pure white without hierarchy
- No dual “開/關” button pairs for booleans — use **toggles**
- No emoji icons
- No marketing hero copy inside ops console
- No overlapping badges on content
- No 3 equal marketing cards

## 8. Product-Specific Content Mapping
1. **MCP 接入口** — tabs: 本機 / Cloudflare / LAN / Custom
2. **ChatGPT Worker** — open window, auto-wake toggle, **auto-approve-tools toggle**, bound URL, last error
3. **Goal loop** — goal id, phase, ticks/no-commit, bound thread, stop
4. **Workspace** — CWD set/reset
5. **Auth** — status + env help
6. **Live goals** — compact list

## 9. Implementation Notes for AgentDock
- Prefer CSS variables for light/dark via `data-theme="light|dark"` and `prefers-color-scheme` when system
- Persist theme in `localStorage.agentdock.console.theme`
- Persist nothing security-sensitive in localStorage
- Toggles call existing POST `/internal/runtime/chatgpt/worker` with `{auto_wake:bool}` / `{auto_approve_tools:bool}`
