<p align="center">
  <img src="docs/images/logo.svg" width="112" height="112" alt="Athanor logo">
</p>

<h1 align="center">Athanor</h1>

<h3 align="center">Let Claude Code build whole projects while you do something else.</h3>

<p align="center">
  <i>The athanor was the alchemist's furnace, built to burn for days without going out and without anyone tending it.</i>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-3fc6a5?style=flat-square" alt="MIT license"></a>
  <img src="https://img.shields.io/badge/platform-Windows%2010%2F11%20%C2%B7%20Linux-1b2430?style=flat-square" alt="Windows 10/11 and Linux">
  <a href="https://claude.com/claude-code"><img src="https://img.shields.io/badge/runs-Claude%20Code-d97757?style=flat-square" alt="Runs Claude Code"></a>
  <img src="https://img.shields.io/badge/app-Go%20%C2%B7%20one%20file-00add8?style=flat-square" alt="Go app, one file">
  <img src="https://img.shields.io/badge/UI-6%20languages-7c8da1?style=flat-square" alt="6 languages">
</p>

<p align="center">
  <a href="#quick-start"><b>Quick start</b></a> ·
  <a href="#how-it-works"><b>How it works</b></a> ·
  <a href="#parallel-mode"><b>Parallel mode</b></a> ·
  <a href="#the-app"><b>The app</b></a> ·
  <a href="#security"><b>Security</b></a> ·
  <a href="#faq"><b>FAQ</b></a> ·
  <a href="#troubleshooting"><b>Troubleshooting</b></a> ·
  <a href="README.fr.md"><b>Français</b></a>
</p>

<p align="center">
  <a href="https://github.com/Aprilox/Athanor/releases/latest"><img src="https://img.shields.io/badge/Download-Windows%20%C2%B7%20Linux-3fc6a5?style=for-the-badge" alt="Download for Windows and Linux"></a>
</p>

<p align="center">
  <img src="docs/images/overview.png" alt="Athanor overview: three workers building milestones in parallel, the project plan organized in waves, and usage statistics" width="100%">
</p>

You describe what you want: a game, a website, an app, a tool. Athanor runs [Claude Code](https://claude.com/claude-code) **cycle after cycle** on your own PC, with your own Claude subscription. Every cycle finishes **one milestone of the plan**: Claude does the work, runs the project's checks, ticks the milestone and commits it. Then a fresh session takes the next one, until the project is delivered. You can close the app, lock it or go to bed. The furnace keeps burning.

> [!NOTE]
> Athanor is an independent open-source project. It is not affiliated with or endorsed by Anthropic. It drives the official Claude Code CLI with your own subscription.

---

## Highlights

| | |
|---|---|
| **An autonomous loop** | One milestone per cycle, a fresh context each time, so turns stay cheap and the project never drifts. Usage limit reached → pause until it reopens, or switch to a fallback model. Error → retry with a growing delay. Silent or over-long cycle → cut and resumed. PC restarted → resumes where it stopped. All state lives in files. |
| **Parallel mode** | One switch per project. A fine-grained plan with dependencies; up to 10 workers build independent milestones at once, each in its own git worktree; the pilot merges each one as soon as it finishes and starts the next, and the lead verifies once at the end. |
| **A real team** | A project lead plus sub-agents: `architect`, `creator`, `developer`, `tester`, `critic`, and a cheap `executor` (Haiku by default) for mechanical work. The plan, independent tests and the final critique are always delegated. |
| **Skills & MCP** | Give Claude expertise and tools: a verified catalog (Anthropic skills, superpowers, Context7, Serena, Playwright, GitHub…), a search over thousands of community skills (SkillsMP) and over the official MCP Registry, any skill from a GitHub link, skills you write yourself, any MCP server pasted from its README. Switched on per project, for the lead and the parallel workers. Local MCP servers run in a cage, away from your files and the Claude token. |
| **A sandbox** | Claude's commands run under bubblewrap + seccomp. They can write only in the project, never read the rest of your machine, and Internet stays open. |
| **Your PC's software** | Unreal Engine, Blender, the Android SDK, Docker and winget are reached through a controlled bridge, command by command. |
| **No UAC prompts** | Opt-in. One consent, and from then on Claude installs and runs what the project needs with administrator rights, silently. |
| **A desktop app** | One file. It shows the live team, the plan, usage, the readable log and the request queue, plus health checks with one-click repair. It also runs several projects at once and keeps Claude's windows on their own virtual desktop. Themes, 6 languages. |
| **Clean projects** | A project is any folder on your PC. Athanor only adds a `.athanor/` workshop (excluded from git); delete it at the end and nothing is left behind. |
| **Frugal by design** | Targeted reading rules, a quick check during the milestone and the full check once, screenshots assembled into contact sheets, a project map regenerated before each cycle, automatic archives, command output compression. The Overview page shows live how much of each session is re-read from the prompt cache. |

---

## Quick start

**You need:** Windows 10/11 (WSL2 is installed for you) or Linux, and a Claude subscription (Pro, Max or Team).

### 1. Get Athanor

<table>
<tr>
<th width="50%">Option A · Download a release <i>(recommended)</i></th>
<th width="50%">Option B · Build it yourself</th>
</tr>
<tr>
<td valign="top">

From the [**latest release**](https://github.com/Aprilox/Athanor/releases/latest):

- **Windows**: `Athanor-X.Y.Z-Windows.zip`, unzip it anywhere (the Desktop, for example).
- **Linux**: `Athanor-X.Y.Z-Linux.tar.gz`, then `tar xzf Athanor-X.Y.Z-Linux.tar.gz`.

Each download is the complete tool, ready to use.

</td>
<td valign="top">

From the source code, with [Go](https://go.dev/dl/) 1.24 or later.

**Windows** (PowerShell):

```powershell
git clone https://github.com/Aprilox/Athanor.git
cd Athanor
.\build.cmd
```

No git? *Code → Download ZIP*, unzip, double-click `build.cmd`. If Go is missing, `build.cmd` offers to install it with winget.

**Linux**:

```bash
git clone https://github.com/Aprilox/Athanor.git
cd Athanor
./build.sh
```

</td>
</tr>
</table>

Both options give the same folder: `Athanor.exe` (or `athanor`) next to `commands/`, `system/` and `template/`. The app needs them, so always keep the whole folder together.

> [!TIP]
> On a downloaded release, Windows SmartScreen may show *"Windows protected your PC"* the first time, because the app is not code-signed. Click *More info → Run anyway*. An `Athanor.exe` you built yourself does not trigger it.

### 2. First launch

**Windows**

1. Double-click **`Athanor.exe`**. Pick a language, a theme and an accent: the first-launch guide takes you through the rest.
2. **Install**: one window installs WSL2, Claude Code and the sandbox. Linux asks for a password once. The very first time may need a restart: then open "Ubuntu" once to create your user, and run the installation again.
3. **Connect**: *Setup & health* → *Check everything*. On the *Token and model access* line, click *Repair*: a window signs you in to your Claude account. Paste the token in *Settings*.
4. **Optional, recommended**: *Setup & health* → *Administrator rights for installations* → *Repair*. Windows asks for your consent **once**; Claude never needs a UAC click again.
5. **Add a project**: a name, its folder (existing or new, on any drive) and what you want. Technologies are free unless you impose some. ✨ *Improve with Claude* turns a few words into a complete request; you can go back to yours.
6. **Start.** Add more projects the same way and run them side by side.

**Linux** (the scripts run natively, without WSL)

```bash
cd athanor                   # or Athanor, if you built it from source
commands/install.sh          # Claude Code, sandbox, tools (asks for sudo once)
commands/configure.sh        # token, models, options
./athanor                    # the app, opens in your browser
```

Then add a project and start it, as on Windows (steps 5 and 6).

### 3. Updating

| You installed | To update |
|---|---|
| a release | close the app, download the new release and extract it in the same place, so that its `athanor` folder merges into yours |
| from source | close the app, then `git pull` and run `build.cmd` (or `./build.sh`) again |

Your `.env`, settings, `logs/` and `tools/` are never touched by an update. The Windows and Linux apps keep separate settings files (`athanor-config.json`, `athanor-config-linux.json`), so a tool folder shared between both systems (dual boot, for example) works with both.

The app only uses Go's standard library plus three vendored packages for the WebView2 window, so building needs no network access once the source is on your disk.

---

## How it works

<p align="center">
  <img src="docs/images/how-it-works.svg" alt="How Athanor works: you describe the project, the pilot loop runs a Claude Code session per cycle, the lead delegates to sub-agents or to parallel workers, every cycle ends with merge, check, tick and commit" width="100%">
</p>

**A cycle.** The pilot (`system/pilot.sh`) syncs the request, archives old milestones, rebuilds the project map (`.athanor/MAP.md`), opens the Unreal editor if the project has one, then starts a Claude Code session with [`template/CLAUDE.md`](template/CLAUDE.md) as instructions, in your language. Claude takes the first unchecked milestone of `.athanor/PLAN.md`, does it, runs `check.sh`, ticks it, logs it in `LOG.md` and commits. The pilot closes the cycle shortly after the commit and starts the next one.

**A project's life.**

| Phase | What happens |
|---|---|
| Framing | `docs/REQUIREMENTS.md`, `docs/DESIGN.md`, `docs/ARCHITECTURE.md`, and a plan of 8 to 25 milestones, each with verifiable acceptance criteria |
| Skeleton | an executable skeleton and the project's own `check.sh` (build, tests, screenshots) |
| Complete slice | one feature working end to end, checked by the `tester` |
| Production | the remaining milestones, one per cycle, or several at once in parallel mode |
| Polish | the `critic` uses the result like a demanding user and files bugs in `BUGS.md` |
| Delivery | `deliverable/`, `TEST_GUIDE.md`, then `FINAL_REPORT.md` with `STATUS: DONE`. The pilot takes the next queued request, or stops |

---

## Parallel mode

*Overview → Working mode → Parallel mode*, per project, with 2 to 10 workers at once.

1. The lead writes a **fine-grained plan with explicit dependencies**: many small, self-contained milestones, each with the files it touches, `Depends on:` the milestones it needs, and acceptance criteria. M1 is always the foundation (skeleton, `check.sh`, shared contracts).
2. The pilot runs a **scheduler**: every milestone whose dependencies are merged gets a **worker** (its own Claude session, its own git worktree `.athanor/work/M5` and branch `auto/M5`), up to the chosen number at once.
3. As soon as a worker finishes, the pilot **merges its branch immediately** when it applies cleanly, ticks the milestone, and starts the next ready milestones. Nobody waits for the slowest worker.
4. At the end of the cycle, **one** session of the lead verifies everything: it merges the branches that conflicted, runs the full check, fixes what the merges broke, and files the rest in `BUGS.md`.
5. **Queued requests don't wait.** At the start of each cycle they join the current request: the lead adds their milestones to the plan, with their dependencies.

Each worker only carries its own milestone in context, so the work is split into small, cheap sessions instead of one huge one. A worker may hand its long test runs and searches to `executor` (cheap model) and read only the failures; it calls no other sub-agent. The Team page shows one card per worker with the sub-agents it called, and the Log tags their lines `[M5]`. Parallel mode uses more quota per hour (one session per worker) but far less wall-clock time.

---

---

## The team

| Agent | Role | Model |
|---|---|---|
| **Project lead** | reads the plan, codes ordinary milestones, delegates, reviews every report, commits | `MODEL` |
| `architect` | requirements, architecture and the milestone plan; revises the plan when needed | `SUBAGENT_MODEL` |
| `creator` | content: story, world, dialogue, interface texts, programmatic 2D/3D/sound assets | `SUBAGENT_MODEL` |
| `developer` | one precise development task with its tests | `SUBAGENT_MODEL` |
| `tester` | automated tests, screenshots, bug reports (never fixes the code itself) | `SUBAGENT_MODEL` |
| `critic` | uses the result like a demanding end user, without complacency (read only) | `SUBAGENT_MODEL` |
| `executor` | mechanical work with no design decision: long command outputs, searches, bulk edits, file copies; the only sub-agent parallel workers may call | `CHEAP_MODEL` (Haiku) |

The agents live in [`template/.claude/agents/`](template/.claude/agents): edit them to change how your team works.

---

## The app

| | |
|:---:|:---:|
| <img src="docs/images/team.png" alt="Team page: the project lead with the sub-agents it called, and three workers, each with its own executor"><br>**Team**: who called whom, what each one does, live | <img src="docs/images/log.png" alt="Log page in the light theme: pilot events, Claude's words, tools and sub-agents, worker lines tagged [M5]"><br>**Log**: readable or raw, filtered, workers tagged |
| <img src="docs/images/requests.png" alt="Requests page: the current request with its plan in waves, and two queued requests that can be reordered"><br>**Requests**: a queue you can reorder, notes along the way | <img src="docs/images/options.png" alt="Options page: language, five palettes and five accents, startup options, tool folder"><br>**Options**: 6 languages, 5 palettes × 5 accents |

The screenshots show the dark "Slate" and the "Light" palettes.

| Page | What you do there |
|---|---|
| **Overview** | the state in one sentence, current milestone, last commit, ticked plan, usage (cycles, time, list-price cost, cache); *Start*, *Stop after cycle*, *Stop now*, *Working mode* |
| **Team** | a tree: the lead and each worker with the sub-agents it called; click a sub-agent for the brief it was given and the report it handed back; the model each one runs on; who can be called |
| **Requests** | the current request and its plan, the queued requests (time-stamped, reorderable), notes along the way; ✨ *Improve with Claude* rewrites a request clearer and more complete, and you can go back to yours |
| **Log** | the readable log (Claude speaks, tools, results, sub-agents, pilot) or the raw JSON, filterable |
| **Skills & MCP** | the library (verified catalog, search in SkillsMP and the MCP Registry, GitHub link, your own skills, pasted MCP configurations), a switch per project, the API keys of the servers (stored apart, never shown), *Test* starts a server in its cage and lists its tools |
| **Setup & health** | *Check everything*, *Install everything*, *Test the sandbox* for real, *Repair* line by line, *Ask Claude* to repair or install something |
| **Settings** | the `.env` (token masked, models, effort, watchdog…), *Save and restart after cycle* |
| **Options** | language, palette and accent, project list (removing a project never touches its folder), restart the pilots on opening, start with Windows, "Claude" virtual desktop, password, tool folder |

**Skills & MCP, safely**: skills are loaded as a session plugin (`--plugin-dir`), so Claude only reads one when it needs it; it can read them but never change them. MCP servers are added to the session's own MCP configuration; a local one starts in a bubblewrap cage (`system/mcp-jail.sh`): system read-only, only the project and the tool caches writable, no home folder, no Windows drives, no Athanor folder, no Claude token. Requires Claude Code with `--plugin-dir` for skills.

**A plan you can shape**: click a milestone to see its files, dependencies and acceptance criteria; move it up or down, or remove it (the other milestones stop depending on it), or edit `PLAN.md` as a whole.

**Lock**: with a password (*Options*), the 🔒 button locks the app. Claude keeps working, but nothing can be seen or changed (token included) without the password, and the app starts locked.

**"Claude" virtual desktop**: while a pilot runs, the windows it opens (Unreal, Blender, a game under test) go to a dedicated Windows desktop, so they never pop up over your work or your game. A window you bring back is never moved again. *View Claude's desktop* / *Come back*, or Ctrl + Win + ← / →.

The interface is translated into English, French, Spanish, German, Italian and Portuguese. The language you choose is also the one Claude writes the plan, log and documents in.

---

## What Claude can and cannot do

| | |
|---|---|
| Write | only in the project folder (and in the tool's `tools/` for what it installs) |
| Read | only the project, `tools/` and the Linux system files; not your documents, not the Windows drives, not `.env` |
| Internet | yes, freely |
| Install tools | yes: in `tools/` (Linux tools, portable Windows software in `tools/windows/`), or in a disposable Docker container; every installation is recorded in `tools/INSTALLS.md` |
| Install Windows software | yes, with winget or an installer: for your user, or with administrator rights if you allowed it (one Windows consent) |
| Windows firewall | set automatically with the same permission: the project's programs can accept connections **from the local network only** |
| Your Windows software | yes, through the gateways below. **This is an intended exit from the sandbox**: those programs run with your Windows rights |
| `cmd.exe`, `powershell.exe` | no, blocked from the sandbox (the gateways remain available) |
| Change the pilot, the sandbox or its settings | no (`system/` is outside its folders) |

---

## Your PC's tools

Claude works in Linux (WSL2 on Windows) and reaches your Windows software through gateways. The bridge runs each command **outside** the sandbox, with your rights:

| Gateway | What it does |
|---|---|
| `unreal-editor`, `unreal-cmd`, `unreal-uat` | opens the Unreal editor (optionally driven through Unreal MCP), runs commandlets and tests, packages Windows and Android builds |
| `blender` | the PC's Blender, typically in the background with a Python script (`blender -b -P scene.py`) |
| `adb` | the PC's Android tool: install and test on a phone plugged in over USB |
| `tool IMAGE CMD…` | runs a command in a disposable Docker container, with only the project mounted |
| `install-windows ID` / `--file setup.exe` | installs Windows software (winget or an installer), elevated when allowed |
| `pc-exec [--admin] program.exe` | runs a Windows program the project built or downloaded |
| `windows-var NAME PATH` | sets a Windows environment variable for the development tools (`ANDROID_HOME`, `JAVA_HOME`…) |

Installed software is detected on **every drive** (`C:`, `D:`…), and the newest version wins. The detected list is written before each cycle to `.athanor/PC_TOOLS.md`. Paths can be forced in *Settings* when detection misses them (`UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`).

**Unreal Engine (MCP)**: set `UNREAL_MCP=1` in *Settings* and run `commands\unreal-network.cmd` once. Claude enables the MCP plugins in the Unreal project itself, and the pilot opens the editor before each cycle. Claude drives it (actors, lights, materials, tests), runs tests and screenshots with `unreal-cmd`, and packages the Windows game with `unreal-uat` into `deliverable/`. The Unreal MCP is experimental: when it does not answer, Claude carries on without it.

**Missing software**: Claude sorts it out on its own (a portable version in `tools/windows/`, else winget, else the administrator task if allowed). If an installation fails, it writes it to `.athanor/TODO_FOR_YOU.md` and finds another way.

**Mobile games (Unreal)**: Android first (portable SDK and Java, `.apk` installed on a phone plugged in over USB), iOS kept portable (`docs/IOS_PORT.md`).

**Docker (optional)**: needs Docker Desktop with *Settings > Resources > WSL integration*.

---

## Configuration

Everything is editable in the app (*Settings*), or in `.env` ([`.env.example`](.env.example) documents every key).

| Key | Role |
|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | token of your subscription (`claude setup-token`), never shared |
| `MODEL` / `FALLBACK_MODEL` / `SUBAGENT_MODEL` | lead model, model used while the lead's quota is exhausted, sub-agents model |
| `CHEAP_MODEL` | model of the `executor` sub-agent (default `haiku`) |
| `EFFORT` | reasoning effort, `low` to `max` |
| `LANGUAGE` | language Claude works and documents in (set by the app) |
| `MAX_HOURS` | safety stop (default 168, one week) |
| `QUOTA_WAIT_MIN` | wait when the quota reset time cannot be read |
| `IDLE_MAX_MIN`, `CYCLE_MAX_MIN`, `CLOSE_AFTER_MILESTONE` | cycle watchdog, one milestone per cycle |
| `RTK`, `TZ` | command output compression, time zone of the logs (empty = the system's) |
| `UNREAL_MCP`, `UNREAL_MCP_URL` | let Claude drive the open Unreal editor |
| `UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`, `ANDROID_JDK_DIR` | tool paths when auto-detection misses them (optional, absent by default) |

When the quota is exhausted for every model, the pilot waits for it to reopen and resumes on its own.

---

## Security

What the sandbox guarantees, and what it deliberately does not:

- **Claude's commands** run in the Claude Code sandbox (bubblewrap + seccomp). They can write only in the project folder and `tools/`. They cannot read the rest of your Linux home or the Windows drives, and cannot launch `cmd.exe` or `powershell.exe` directly. *Setup & health → Test the sandbox* proves it with a real session. Claude's file tools (Read, Edit, Write…) are limited the same way by a hook ([`system/guard.py`](system/guard.py)).
- **Windows software is an intended exit.** The gateways run outside the sandbox, with your user rights, and `pc-exec` runs any Windows program Claude places in the project or in `tools/windows`. Treat a project like code you run yourself.
- **Administrator rights are opt-in.** *Administrator rights → Repair* asks for your consent once and registers an elevated scheduled task (`Athanor - installations`). Its script is copied to `C:\ProgramData\Athanor`, which only administrators can modify. The task only runs winget packages and programs located inside your project folders or `tools\windows`. It also lets the project's programs accept local-network connections without the firewall dialog. Remove it any time with `commands\allow-admin.cmd remove`.
- **The pilot never trusts the project folder.** It keeps its own state (pid, lock, sandbox settings, instructions, counters) in `logs/`, out of Claude's reach, and never writes through a symbolic link placed in `.athanor/`. It validates every value it reads back, and runs git with hooks, filters, fsmonitor and external diff disabled. It starts Claude Code with only its own settings and MCP servers (`--setting-sources user`, `--strict-mcp-config` when the CLI supports them), and `tools/` is never in its `PATH`.
- **The app** listens on `127.0.0.1` only and rejects any other `Host` header (DNS rebinding). It requires a per-session secret header on every API call and stores the password salted and hashed. It never sends `.env` secrets unless asked, and while locked it shows only "running / phase / cycle".
- **"Ask Claude" repair sessions** run outside the sandbox, in the tool folder, without `sudo`: on purpose, to repair the tooling itself. Anything that needs `sudo` or Windows is written to `logs/TODO_FOR_YOU.md` for you instead.
- **Nothing personal is ever committed**: `.env`, `logs/`, `tools/` and the app's settings are git-ignored, and projects live outside the tool folder.

Found a vulnerability? Please open a [private security advisory](https://github.com/Aprilox/Athanor/security/advisories/new) rather than a public issue.

---

## Running for days

- The pilots run in WSL without a window: you can close the app.
- Disable sleep mode (*Settings > System > Power > Sleep: Never*).
- *Options*: "Open the app when Windows starts (without a window)" and "Restart the pilots that were running, when the app opens".
- Safety stop after `MAX_HOURS` (counted from the project's first start); raise it in *Settings* to go on.

## Finished project, follow-ups, new project

- **Follow-up requests**: *Requests* page. As soon as the current request is done, the pilot takes the first queued one, appends it to `REQUEST.md` and reopens the project with a resume milestone.
- **Note along the way**: *Requests* page, read at the start of the next cycle, without a restart.
- **Reopen by hand**: replace `STATUS: DONE` with `STATUS: REOPENED` in `.athanor/FINAL_REPORT.md`, then *Start*.
- **New project**: selector → *+ New project*. **Clean a finished project**: delete its `.athanor/` folder (and `.claude/` if present).

---

## Commands without the app

| Windows | Linux / WSL | |
|---|---|---|
| `commands\install.cmd` | `commands/install.sh` | install everything |
| `commands\configure.cmd` | `commands/configure.sh` | token, models, options (the language is set in the app) |
| `commands\start.cmd D:\Dev\game` | `commands/start.sh --background DIR` | start the pilot on a project |
| `commands\follow.cmd D:\Dev\game` | `commands/follow.sh DIR` | follow it live |
| `commands\stop-after-cycle.cmd D:\Dev\game` | `commands/stop-after-cycle.sh DIR` | stop cleanly after the current milestone |
| `commands\stop.cmd D:\Dev\game` | `commands/stop.sh DIR` | stop now |
| `commands\test-sandbox.cmd` | `commands/test-sandbox.sh` | prove the sandbox with a real session |
| `commands\allow-admin.cmd [remove]` | | allow or remove administrator installations |
| `commands\unreal-network.cmd` | | WSL network for the Unreal MCP (once) |

Without a folder, the commands use `<tool>/projet`. The request is read and written in `DIR/.athanor/REQUEST.md`, the next ones in `DIR/.athanor/queue/` (one file per request, taken in order).

## Where to look

- `.athanor/STATE.json`: phase, cycle, model, current milestone, planned resume; `stats.tsv`: one line per cycle.
- `.athanor/PLAN.md`, `LOG.md`; `pilot.log` (cycles, quotas, fallbacks); `logs/` (raw log of each cycle).
- At the end: `.athanor/FINAL_REPORT.md`, `TEST_GUIDE.md`, and the deliverable in `deliverable/`.
- On the tool side: `logs/` (repairs, sandbox test, `TODO_FOR_YOU.md`).

---

## Repository layout

```
Athanor.exe, athanor   the app for Windows / Linux (in the release, or built with build.cmd / build.sh)
app/                   the app's Go sources, static UI (HTML, CSS, JS without dependencies), Windows resources
commands/              install, configure, start, stop, stop-after-cycle, follow, test-sandbox, repair,
                       allow-admin, unreal-network (.cmd for Windows, .sh for Linux/WSL)
system/                pilot.sh (the loop), parallel.sh (scheduler, workers, merges), extensions.py + mcp-jail.sh (Skills & MCP), safety.sh, bridge.py, settings.py
                       (sandbox), guard.py, detect-tools.sh, athanor.py (map, archives, screenshots),
                       bin/ (gateways), windows/ (PowerShell helpers)
template/              CLAUDE.md (Claude's instructions), PC_TOOLS_GUIDE.md, .claude/agents/ (the team)
docs/images/           README images
.env.example           every setting, documented
logs/, tools/          created at run time, never committed
```

Inside each project, `.athanor/` holds REQUEST.md, PLAN.md, LOG.md, queue/, done/, check.sh, STATE.json, stats.tsv and logs/. It is the only thing Athanor adds.

---

## FAQ

<details>
<summary><b>What does it cost?</b></summary>

Only your Claude subscription: Athanor chains Claude Code sessions, nothing else. The most capable models use quota fastest; Sonnet lasts longer. `FALLBACK_MODEL` keeps working while the main model's quota is exhausted, and `CHEAP_MODEL` handles the mechanical work. The Overview page shows the cost of every cycle at list price, which is what your subscription quota pays.
</details>

<details>
<summary><b>What can it build?</b></summary>

Anything Claude Code can build and check by itself in a folder: websites and web apps, desktop and command-line tools, games (Unreal Engine, Godot, web), Android apps, libraries. Projects that need your Windows software use the gateways. The checks are written by Claude for each project (`check.sh`), with screenshots for anything visual.
</details>

<details>
<summary><b>Can I steer it while it works?</b></summary>

Yes. *Requests → Note along the way* is read at the start of the next cycle. New requests are queued and can be reordered. *Stop after cycle* finishes the current milestone, saves it, then stops, so nothing is lost.
</details>

<details>
<summary><b>Will it get in my way while I use the PC?</b></summary>

The pilots run in WSL without a window. The windows they open (an editor, a game under test) go to the "Claude" virtual desktop while a pilot runs. Remember to disable sleep mode for long runs.
</details>

<details>
<summary><b>Does the project stay clean?</b></summary>

Yes: delete `.athanor/` when you are done, plus `.claude/` if your Claude Code version did not support `--agents`. Both are excluded from git through `.git/info/exclude`.
</details>

<details>
<summary><b>Can I use it on several PCs?</b></summary>

Yes. Nothing is tied to a user name or a drive letter: paths come from your profile, the tool folder can live anywhere, and software is detected on every drive. Copy the folder or clone the repository, then run the setup on each PC.
</details>

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| "Run install.cmd first" / WSL failing in the health checks | *Install everything* (or `install.cmd`) has not chosen the WSL distribution yet |
| The pilot stops at once, with a message about the sandbox | *Repair* on the *System sandbox* line: bubblewrap, socat or the seccomp filter is missing |
| Sandbox test: "CMD.EXE LAUNCHED FROM THE SANDBOX" | the seccomp filter is missing: *Repair* on the *System sandbox* line |
| Unreal: "MCP unavailable for this cycle" | editor 5.8 with the Unreal MCP plugins, *Auto Start Server* checked, WSL network "mirrored" (`unreal-network.cmd`) |
| `tool`: "Docker not found" | start Docker Desktop, enable *WSL integration* |
| "Usage limit reached, Claude is waiting" | expected; with `FALLBACK_MODEL`, Claude goes on with the fallback model |
| "Connection problem" | missing or wrong token: *Settings*, or *Repair* on the *Token and model access* line |
| An installation needs administrator rights | *Setup & health* → *Administrator rights for installations* → *Repair* (once) |
| A `.sh` script fails with `$'\r'` | Windows line endings: save it with "LF" (the repository enforces it through `.gitattributes`) |

---

## Releasing

Push a version tag and GitHub builds both apps and publishes the release ([`.github/workflows/release.yml`](.github/workflows/release.yml)):

```bash
git tag v2.0.1 && git push origin v2.0.1
```

Keep the version in [`app/winres/athanor.rc`](app/winres/athanor.rc) in step with the tag.

## Contributing

Issues and pull requests are welcome. Before opening one:

- `cd app && go vet . && GOOS=windows go vet .` must pass, as well as `go test ./...`.
- Shell scripts must pass `bash -n` and `shellcheck -S warning`.
- Comments and user-facing strings are in English. Some internal identifiers are in French, because the project started in French; keep the existing names when you touch them. New interface strings go through `t()` and get their translations in [`app/static/i18n.js`](app/static/i18n.js).

## Acknowledgements

- [Claude Code](https://claude.com/claude-code), which does the actual work.
- [VirtualDesktop](https://github.com/MScholtes/VirtualDesktop) by Markus Scholtes (MIT), compiled on first use for the "Claude" desktop.
- [go-webview2](https://github.com/jchv/go-webview2) (MIT) for the Windows window, and [bubblewrap](https://github.com/containers/bubblewrap) for the sandbox.

## License

[MIT](LICENSE) © Aprilox
