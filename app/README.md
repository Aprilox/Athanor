# The app (Go)

One executable, standard library only (plus three vendored packages for the WebView2 window): it serves the interface on `127.0.0.1` (random port, anti-CSRF token per session) and opens it in a real application window (WebView2; Edge/Chrome `--app` window as fallback). Everything else in the tool (`commands/`, `system/`, `template/`) is driven through it, and every project is a folder of your own with a `.athanor/` workshop inside.

| File | Role |
|---|---|
| `main.go` | HTTP server, static UI, CSP, window |
| `config.go` | `athanor-config.json` next to the exe: tool folder, projects, language, theme, options, password hash |
| `serveur.go` | routes, session lock, CSRF, JSON helpers, per-project state, auto-restart of pilots |
| `api_projets.go` | state, log, team, pilot start/stop, add/choose/remove project, folder browser, preferences (language, theme, accent, first-launch guide) |
| `api_demandes.go` | current request, queue (reorder, remove), notes along the way |
| `api_outillage.go` | `.env` (secrets masked), health checks, repairs, "Ask Claude", options, virtual desktop, open files |
| `kit.go` | tool folder and project workshop (`.athanor/`): `.env`, requests, `STATE.json`, `stats.tsv`, plan |
| `journal.go` | incremental reading of Claude Code `stream-json`, readable translation, team tracking (lead + sub-agents) |
| `taches.go` | background tasks with live output |
| `sante.go` | checks, repairs, Claude repair sessions (`commands/repair.sh`), real sandbox test |
| `textes.go` | known languages, themes and accents; per-language labels written into project files |
| `systeme_windows.go` | WSL without a window, WebView2 window, start with Windows, "Claude" virtual desktop ([VirtualDesktop](https://github.com/MScholtes/VirtualDesktop) by Markus Scholtes, MIT, compiled at first run with Windows' `csc.exe`) |
| `systeme_autre.go` | Linux/macOS equivalents (native kit, tests) |
| `static/` | interface: HTML, CSS, JavaScript without dependencies; `logo.svg` is the app logo (favicon and header) |
| `winres/` | Windows icon (`athanor.ico`, rendered from `static/logo.svg`) and version info (`athanor.rc`), compiled into `rsrc_windows_amd64.syso` by `build.sh` and linked into the exe (`go build` picks up any `.syso`); the window uses it through `IconId: 1` |

Interface conventions (`static/`):
- `app.css`: every color is a token on `:root`; palettes (`data-theme`: ardoise, minuit, ocean, clair, sable) override the surfaces and text, accents (`data-accent`: sarcelle, bleu, violet, ambre, rose) the highlight color. Semantic colors (`--ok`, `--attention`, `--erreur`, `--info`) stay stable. No inline styles except computed values (progress width, palette previews).
- `i18n.js`: translations keyed by the English source string (`t(key, args)`, `{0}` substitution, English fallback, prefix translation for log lines), the language, theme and accent tables, and the theme bootstrap from `localStorage` so the page never flashes the wrong palette.
- `app.js`: one state object, one `api()` helper (adds the project and the CSRF header), one renderer per page, event delegation on `#page`, a 1.5 s refresh loop. The first-launch wizard (`#assistant`) and the lock screen (`#verrou`) are overlays.

Build: `build.cmd` (Windows, needs Go) or `./build.sh` (Linux/macOS/WSL, cross-compiles `Athanor.exe` and recompiles the icon resource when `llvm-windres` or `x86_64-w64-mingw32-windres` is installed; the committed `.syso` is used otherwise). `go vet` must pass for both `GOOS=linux` and `GOOS=windows`.

Security model: listens on loopback only; every state-changing request needs the `X-Athanor` header; with a password set, the app starts locked and refuses everything but the unlock endpoint (400 ms delay per attempt); `.env` secrets are only returned with `?secrets=1` while unlocked; the config file is written with mode 0600.
