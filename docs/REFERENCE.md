# Reference: settings, commands, folders

<sub>Part of the [Athanor Hub README](../README.md) · [Français](REFERENCE.fr.md)</sub>

## macOS in detail

**What *Install everything* puts on the Mac**, through the official installers only: [Homebrew](https://brew.sh) if it is missing (its own installer asks for your password and Enter); seven command-line tools through Homebrew: `git`, `jq`, `ripgrep`, `node`, `imagemagick`, `ffmpeg`, `gh`; Claude Code (Anthropic's installer, into `~/.local/bin`, with one line in `~/.zshrc`); RTK, which shortens command output to save tokens, into Athanor's folder. Nothing with a window, nothing that starts by itself. The sandbox is macOS's own (Seatbelt): only checked. Everything is written to `logs/install-mac.log` in Athanor's folder.

**Its window is its own**, the Mac's web engine (WebKit) inside Athanor: no browser opens. **While it runs**, Athanor has its icon in the Dock and a flame near the clock (the menu bar); both show the pilots at work and open the window again. The red button hides the window: Athanor goes on. **Quit** (⌘Q, or the Dock icon's menu) with pilots running offers to keep Athanor in the menu bar (the Dock icon goes, the pilots go on, the flame stays) or to stop them and quit; *Quit* from the flame quits, the pilots going on or not, as you choose. The Mac stays awake while a pilot runs (`caffeinate`; a closed MacBook still sleeps, unless it is plugged in with an external screen).

Claude works in Seatbelt, the sandbox built into macOS (the same one Claude Code uses): it only writes in the project. The MCP servers have a Seatbelt cage of their own. Unreal, Blender, Roblox Studio, Xcode and the Android SDK are found in their usual places and Claude runs them directly; Windows programs and Docker through `tool` are Windows and Linux only. On the Mac, the programs Claude opens show on your screen (there is no separate desktop as on Windows).


## What an update keeps

Your token, settings, projects, `logs/` and `tools/` are never touched by an update. An installation made before 2.26 (a folder with `Athanor.exe`, `template/` and `athanor-config.json` side by side) stays where it is: put the new executable in it, or use the button. An update also cleans such a folder up: the files of the older versions that no version uses any more (`commands/`, the bash and Python scripts of `system/`, `docs/`, the README…) go when they are exactly as shipped; one you changed is moved into `logs/ancien-kit/<date>/`, and `logs/nettoyage-<date>.txt` lists what went. Nothing else of the folder is touched.

## Configuration

Everything is editable in the app (*Settings*), or in `.env` ([`.env.example`](../.env.example) documents every key). In *Settings → Claude*, an "i" next to a setting explains what a lower or a higher value changes. The framing time box (`FRAMING_MAX_MIN`) can also be set project by project: *Manage → Working mode → Longest framing* (25, 45, 60 or 90 minutes, or a time of your own from 10 to 240 minutes in the "Other" box; "Like Settings" takes the shared value).

| Key | Role |
|---|---|
| `CLAUDE_AUTH` | how Athanor connects to Claude: `subscription` (default: the token below, your plan's quota) or `api` (an API key, each run billed to the API account) |
| `CLAUDE_CODE_OAUTH_TOKEN` | token of your subscription (`claude setup-token`), never shared |
| `ANTHROPIC_API_KEY` | API key (`api` mode), created on console.anthropic.com › API keys, never shared |
| `API_BUDGET_USD` | `api` mode: the most to spend, in dollars (the credit left on the API account); empty = no limit |
| `API_SPENT_USD` | `api` mode: what is spent already when you save it (the count goes on from it) |
| `MODEL` / `FALLBACK_MODEL` / `SUBAGENT_MODEL` | lead model, model used while the lead's quota is exhausted, sub-agents model (`auto`: chosen by the lead for each call) |
| `CHEAP_MODEL` | model of the `executor` sub-agent (default `haiku`) |
| `FRAMING_MODEL` / `WORKER_MODEL` | model of the framing cycle (empty = `MODEL`), model of the parallel workers (default `sonnet`, empty = `MODEL`) |
| `FRAMING_MAX_MIN` | time box of the framing, in minutes (default 25) |
| `RELAY_CONTEXT_K` | context, in thousands of tokens, past which a fresh session takes over the milestone (default 110, 0 = never) |
| `EFFORT` | reasoning effort, `low` to `max` |
| `LANGUAGE` | language Claude works and documents in (set by the app) |
| `MAX_HOURS` | work time limit, in hours: only the time the pilot works counts (not its waits, nor the time it is stopped); 0 = unlimited, the default, the pilot stopping by itself at the delivery; a project can have its own: *Manage → Working mode* |
| `QUOTA_WAIT_MIN` | wait when the quota reset time cannot be read |
| `IDLE_MAX_MIN`, `CYCLE_MAX_MIN`, `CLOSE_AFTER_MILESTONE` | cycle watchdog, one milestone per cycle |
| `KEEP_AWAKE` | the PC stays awake while a pilot runs: `1` (default; the screen too for Roblox Studio and Unreal), `screen` (the screen always), `0` (off) |
| `RTK`, `TZ` | command output compression, time zone of the logs (empty = the system's) |
| `PONYTAIL` | Ponytail's rules for the cycles that build: `1` (default), `0` (off) |
| `UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`, `ANDROID_JDK_DIR` | tool paths when auto-detection misses them (optional, absent by default) |

When the quota is exhausted for every model, the pilot waits for it to reopen and resumes on its own.

**Permissions.** *Settings → Permissions* sets the default mode of two uses: the *SSH and SFTP assistant* (“The assistant beside a server's terminal and files, on every server without a mode of its own.”) and *Projects* (“Every project without a mode of its own, on its servers.”); a server and a project can still choose their own. The four modes are those of [a project's deployment](SERVERS.md#a-projects-deployment); *Manual* by default, and *Ignore permissions* only from Athanor's window, with its password.

**Subscription or API key.** *Settings → Claude → Connection → Connect with*: your Claude subscription (its token) or an API key. Only the chosen one reaches Claude Code (with both, Claude Code would bill the key). With an API key, the plugins and connectors of a claude.ai account (Gmail, Drive… added in the Claude app) do not work; the Skills, the MCP servers and every extension of Athanor work the same. *Setup & health* checks the key, and *Overview* shows the **API spend** in place of the subscription quota: what Claude Code reported for each run (list price) since the count started, against `API_BUDGET_USD` when set (spent, left). *Already spent* (Settings) starts the count from what the account has spent so far. An **automatic stop** in dollars (*Overview → API spend → Auto stop*, say at 80 $) stops the pilot after the cycle that reaches it: the cost of a cycle is known at its end, so it stops around that amount, never in the middle of a cycle. Once the budget is spent, or when the API says the account has no credit left, the pilot waits instead of failing cycle after cycle. *Count again from now* (credit added), a higher budget or another key, and it resumes at once. Saving a new key, or a first budget, starts the count again; a budget raised later keeps it. In a terminal: `athanor depense [zero]`.

**A new connection, at once.** The pilot reads the connection again at each cycle and every few seconds of its waits: a token or a key saved in *Settings* while it waits for a quota, a connection problem, the budget or the credit (another account, a key with credit left) is used at once, the wait cut short. The fallback model and the workers' pause of the old account are let go.

---

## Commands without the app

Everything the app does is a command of the executable (`Athanor.exe` on Windows, the downloaded `./Athanor-Linux-x64`, `Athanor.app/Contents/MacOS/athanor` on the Mac, or `athanor` in Athanor's folder; `Athanor.exe wsl COMMAND …` runs it in WSL):

| Command | |
|---|---|
| `installer` | install everything (Linux, macOS, the WSL side); Windows: `installer-windows` (WSL) or `installer-natif` |
| `configurer` | connection (subscription token or API key, budget), models, options (the language is set in the app) |
| `depense [zero \| arret DOLLARS \| arret off]` | the connection, and with an API key the spend against the budget; `zero` counts it again from now, `arret` sets the automatic stop after the cycle |
| `demarrer [--background] DIR` | start the pilot on a project |
| `suivre DIR` | follow it live |
| `arreter --apres-cycle DIR` | stop cleanly after the current milestone |
| `arreter DIR` | stop now |
| `tester-cage` | prove the sandbox with a real session |
| `autoriser-admin [--retirer]` | allow or remove administrator installations (Windows) |
| `reseau-unreal` | WSL network for the Unreal MCP (Windows, once) |
| `arret-urgence` | the emergency stop of the running Athanor (pilots stopped, hub tokens revoked, approvals refused, vault locked) |
| `mcp-client appairer NAME [--sorte desktop\|code]` | pair Claude Desktop or Claude Code with Athanor's MCP (see [docs/MCP.md](MCP.md)) |
| `module servers [--nom NAME]` / `module appairer` | run Athanor without a window as its Servers module, on a VPS, and pair it with a hub (see [docs/REMOTE.md](REMOTE.md)) |
| `tui` / `gui` | the app in the terminal / in its window (Linux, macOS; see [In a terminal](HUB.md#in-a-terminal)) |
| `aide` | every command |

Without a folder, the commands use `projet` in Athanor's folder. The request is read and written in `DIR/.athanor/REQUEST.md`, the next ones in `DIR/.athanor/queue/` (one file per request, taken in order).

## Where to look

- `.athanor/STATE.json`: phase, cycle, model, current milestone, planned resume; `stats.tsv`: one line per cycle.
- `.athanor/PLAN.md`, `LOG.md`; `pilot.log` (cycles, quotas, fallbacks); `logs/` (raw log of each cycle).
- At the end: `.athanor/FINAL_REPORT.md`, `TEST_GUIDE.md`, and the deliverable in `deliverable/`.
- In Athanor's folder: `logs/` (repairs, sandbox test, `TODO_FOR_YOU.md`).

---

## Athanor's folder

```
Athanor.exe, athanor   the app (Windows; athanor alone on Linux and macOS, and on Windows the Linux one of the WSL mode);
                       everything else is a command of it: the pilot, its gateways (system/bin links), installer…
template/              the texts for Claude, put back by every update
.env, .env.example     your settings and token, and the model of every setting
athanor-config*.json   the app's settings (projects, options)
logs/, tools/          the pilots' logs and private folders; the tools Claude installs (tools/INSTALLS.md)
extensions/, system/   the Skills & MCP library; what the PC has and Claude's commands
local/                 repairs made on this PC (a file here replaces the same one of template/ or app/catalogue.json,
                       never overwritten by an update)
modules/, hub/         each module's data (one folder per module, made when it needs one); the hub's (milestones done today)
app/                   the Go sources, written only when a repair needs them (Rebuild Athanor, a repair by Claude)
```

## Repository layout

```
app/                   the app's Go sources, static UI (HTML, CSS, JS without dependencies), Windows resources;
                       go run . construire builds Athanor.exe and athanor, each carrying template/, .env.example,
                       LICENSE and these sources (app/embarque.go)
template/              CLAUDE.md (Claude's instructions), guides/ (the parts only some cycles need), PC_TOOLS_GUIDE.md, .claude/agents/ (the team)
docs/images/           README images
docs/*.md              guides of the modules: deployments, Cloudflare, MCP, remote Athanors, a test server, a new machine, old missions
docs/releases/         what is new in each version (shown on the release page)
.env.example           every setting, documented
```

Inside each project, `.athanor/` holds REQUEST.md, PLAN.md, LOG.md, queue/, done/, check.sh, STATE.json, stats.tsv and logs/. It is the only thing Athanor adds.

---

## Releasing

Push a version tag: GitHub first runs every CI check on the tagged commit (tests on Windows, Linux and macOS, the single executable, every build target), then builds the apps and publishes the release ([`.github/workflows/release.yml`](../.github/workflows/release.yml)). If a single check fails (or anything after it), nothing is published: the tag is removed, and the release too if it was already created, so Athanor's updates never offer it. Fix, commit, then push the same tag again (`git tag -f v2.42.0 && git push origin v2.42.0`):

```bash
git tag v3.0.0 && git push origin v3.0.0
```

Keep the version in [`app/winres/athanor.rc`](../app/winres/athanor.rc) and `versionAthanor` in [`app/commandes.go`](../app/commandes.go) in step with the tag (a test checks they match), and write what is new in `docs/releases/VERSION.md` (e.g. [`docs/releases/2.31.0.md`](releases/2.31.0.md)): it opens the release page.

Every push is checked on Windows, Linux and macOS ([`.github/workflows/ci.yml`](../.github/workflows/ci.yml)), the single executable included (nothing written before the consent, Athanor's folder installed, nothing next to the file); `cd app && go run . construire --all` builds every target into `dist/`. A release publishes `Athanor-Windows-x64.exe`, `Athanor-Windows-ARM64.exe`, `Athanor-macOS.zip` (Athanor.app, signed ad hoc on a Mac runner), `Athanor-Linux-x64`, `Athanor-Linux-ARM64` and `SHA256SUMS.txt`, under the same names at every version; Athanor's own updates download the same files.

After a change of `app/` on a PC (a repair by Claude, or yours: `athanor reparer --diff` first writes the sources into Athanor's folder when they are not there), `./athanor reparer --recompiler` (`.\Athanor.exe reparer --recompiler` on Windows) rebuilds the kit's executables there: it shows the change against the running executable's own sources, installs a portable Go outside the kit when none is found, runs `go vet`, builds, runs the new executable's autotest and only then puts it in place, keeping the previous one (`reparer --annuler` puts it back).
