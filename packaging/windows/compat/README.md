# Windows Release compatibility assets

`manage-windows.ps1` is intentionally kept only as a Release ZIP compatibility asset.

AgentDock v0.8.2 and v0.8.3 updaters require that filename while extracting a Windows
desktop update. Current AgentDock no longer uses this script for runtime lifecycle
operations; after the one-time flat-to-generation migration succeeds, the installed
compatibility copy is removed.

Do not add new runtime behavior here. The file may be removed once releases that require
the legacy updater contract are outside the supported upgrade window.
