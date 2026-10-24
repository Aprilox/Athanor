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
| **A real team** | A project lead plus sub-agents: `scout`, `architect`, `creator`, `developer`, `tester`, `critic`, and a cheap `executor` (Haiku by default) for mechanical work. Scouts read for the framing, independent tests and the final critique are always delegated. |
| **Skills & MCP** | Give Claude expertise and tools: a verified catalog (Anthropic skills, superpowers, Context7, Serena, Playwright, GitHub…), a search over thousands of community skills (SkillsMP) and over the official MCP Registry, any skill from a GitHub link, skills you write yourself, any MCP server pasted from its README, and Claude Code plugins from the official marketplace (and any other you add), like Shopify's or Stripe's. Switched on per project, for the lead and the parallel workers. Local MCP servers run in a cage, away from your files and the Claude token. |
| **Tasks for you** | What Claude cannot do itself (create an account, pay, receive a code by SMS, generate a key) becomes a task on the *For you* page, with the exact steps; meanwhile Claude carries on with the rest, and the pilot pauses if nothing else can be done. You answer *done* (with a text, files, keys in a protected field) or *skip: find another way*; Claude checks it at once and tells you whether it works or what is still missing. |
| **A sandbox** | Claude's commands run under bubblewrap + seccomp. They can write only in the project, never read the rest of your machine, and Internet stays open. |
| **Your PC's software** | Unreal Engine, Blender, the Android SDK, Docker and winget are reached through a controlled bridge, command by command. |
| **No UAC prompts** | Opt-in. One consent, and from then on Claude installs and runs what the project needs with administrator rights, silently. |
| **A desktop app** | One file. It shows the live team, the plan, usage, the readable log and the request queue, plus health checks with one-click repair. It also runs several projects at once and keeps the programs Claude starts on a desktop of their own, out of your way. Themes, 6 languages. |
| **GitHub, one repository at a time** | Link a project to a new private repository or one of yours, and its commits go there after each milestone. The key Athanor pushes with only works for that repository and stays out of Claude's reach; every push is checked for secrets and never forced. |
| **Clean projects** | A project is any folder on your PC. Athanor only adds a `.athanor/` workshop, with its working documents (requirements, design, architecture), kept out of git on your computer alone, never through your `.gitignore`; delete it at the end and nothing is left behind. |
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
2. **Install**: a fresh install starts in **native Windows** (nothing else to install: *Install everything* puts Git for Windows, jq, Python and Claude Code for Windows). For Claude in a sandbox, open the **WSL** tab of *Setup & health*: *Install everything* there installs WSL2, Claude Code and the sandbox (Linux asks for a password once; the very first time may need a restart, then open "Ubuntu" once to create your user and run the installation again). A short tour then shows the mode, *Setup & health*, a new project and *Start*.
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

**What every agent knows from the start.** `.athanor/PC_TOOLS.md` lists the toolchain already installed and its versions (Node, Python, PHP, Composer, Git…), checked by the pilot, so nobody spends turns probing it. Two short files collect the problems already solved, one line each: `.athanor/SOLUTIONS.md` for the project and `tools/SOLUTIONS.md` for the PC, shared by every project. Every agent reads them first and adds a line when it solves something; the pilot removes duplicates and keeps the latest 60 lines, and merges what parallel workers learned.

**No thread lost when a context gets full.** Every session keeps `.athanor/HANDOFF.md` (where it is, 10 lines). Past `RELAY_CONTEXT_K` thousand tokens of context (150 by default), the pilot stops it between two steps and starts a fresh session on the same milestone, with that file, its last actions and its last messages: cheaper turns, and no automatic compaction that would blur the thread. Workers are relayed the same way, on their own branch.

**Work done inside a program is saved.** An editor or an engine driven through an MCP server keeps its work in memory: before ticking a milestone, the agent saves it into the project. If a milestone used an MCP server and its commit changed no file, the pilot has that checked first on the next cycle. The catalogue's Roblox Studio and Unreal servers come with how to save in them.

**A project's life.**

| Phase | What happens |
|---|---|
| Framing | one time-boxed cycle (`FRAMING_MAX_MIN`, 25 min by default) that only writes the plan: up to 3 `scout`s read the attachments and the existing code at once, the lead writes `.athanor/docs/REQUIREMENTS.md`, `DESIGN.md`, `ARCHITECTURE.md` and a plan of 8 to 25 milestones, each line a self-contained sheet (files, dependencies, acceptance criteria, what to read), and the `critic` checks the plan once. No code, no installation, no demo content; past the time box, the plan as it stands goes ahead |
| Skeleton | an executable skeleton and the project's own `check.sh` (build, tests, screenshots) |
| Complete slice | one feature working end to end, checked by the `tester` |
| Production | the remaining milestones, one per cycle, or several at once in parallel mode |
| Polish | the `critic` uses the result like a demanding user and files bugs in `BUGS.md` |
| Delivery | `deliverable/`, `TEST_GUIDE.md`, then `FINAL_REPORT.md` with `STATUS: DONE`. The pilot takes the next queued request, or stops |

---

## Parallel mode

*Overview → Working mode → Parallel mode*, per project, with 2 to 10 workers at most. Workers run on `WORKER_MODEL` (Sonnet by default): a well-written milestone sheet is execution work, the lead keeps its own model for the plan and the merges.

1. The lead writes a **fine-grained plan with explicit dependencies**: many small, self-contained milestones, each with the files it touches, `Depends on:` the milestones it needs, and acceptance criteria. M1 is always the foundation (skeleton, `check.sh`, shared contracts).
2. The pilot runs a **scheduler**: every milestone whose dependencies are merged gets a **worker** (its own Claude session, its own git worktree `.athanor/work/M5` and branch `auto/M5`), starting with 2 and adding one each time a worker delivers, up to the chosen number. As soon as a worker meets the quota, no other one starts; if they all met it, the lead's verification is skipped and the pilot waits, then runs one milestone with the lead alone before starting workers again. The pilot also lets the lead work alone, for that cycle, when workers would not pay off: fewer than 3 milestones ready at once, only documentation milestones (they link to each other and conflict), or the 5-hour quota at 75% or the weekly one at 85%. Two milestones whose sheets name the same files never run at the same time. The workers share a board, `.athanor/BOARD.md`, written by the pilot every few seconds: what each one is doing and the files it holds. A worker that needs a file outside its sheet reserves it first (`.athanor/RESERVE`); the pilot grants it unless another worker holds it, and no new worker starts on a reserved file.
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
| `scout` | reads what it is pointed at (code, attachments, documentation) and answers one question in a short summary, or writes one document; for the framing | `SUBAGENT_MODEL`, Sonnet when empty |
| `architect` | requirements, architecture and the milestone plan for a large re-planning | `SUBAGENT_MODEL` |
| `creator` | content: story, world, dialogue, interface texts, programmatic 2D/3D/sound assets | `SUBAGENT_MODEL` |
| `developer` | one precise development task with its tests | `SUBAGENT_MODEL` |
| `tester` | automated tests, screenshots, bug reports (never fixes the code itself) | `SUBAGENT_MODEL` |
| `critic` | uses the result like a demanding end user, without complacency (read only) | `SUBAGENT_MODEL` |
| `executor` | mechanical work with no design decision: long command outputs, searches, bulk edits, file copies; the only sub-agent parallel workers may call | `CHEAP_MODEL` (Haiku) |

With `SUBAGENT_MODEL=auto` (*Settings*, "chosen by the lead"), the lead picks the model of each call itself: Haiku for mechanical work, Sonnet for most of the work, Opus only for judgement (architecture, a bug not understood, the final critique). A call that sets none keeps the model of the table.

The agents live in [`template/.claude/agents/`](template/.claude/agents): edit them to change how your team works.

**Your own sub-agents.** On the Team page, **+ New sub-agent** adds a specialist of your own (a translator, a researcher for a domain, a level designer…): a name, when the lead should call it, its instructions (**✨ Improve with Claude** turns a few lines into a structured brief), a model (like the others, cheap or most capable), read only or not, this project or all your projects. The lead can also propose one when a need keeps coming back: it writes `.athanor/agents/<name>.md`, the pilot checks it and adds it to the team at the next cycle, labelled *proposed by the lead*; you keep it, edit it or remove it (a removed proposal is never made again). These sub-agents work like the others: same sandbox, skills, MCP tools and attached files, callable by the lead and by parallel workers; the lead finds them in `.athanor/TEAM.md`, and a request can name one ("have `translator` do the texts"). They are stored in `extensions/agents.json` (12 per project at most).

---

## The app

| | |
|:---:|:---:|
| <img src="docs/images/team.png" alt="Team page: the project lead with the sub-agents it called, and three workers, each with its own executor"><br>**Team**: who called whom, what each one does, live | <img src="docs/images/log.png" alt="Log page in the light theme: pilot events, Claude's words, tools and sub-agents, worker lines tagged [M5]"><br>**Log**: readable or raw, filtered, workers tagged |
| <img src="docs/images/requests.png" alt="Requests page: the current request with its plan in waves, and two queued requests that can be reordered"><br>**Requests**: a queue you can reorder, notes along the way | <img src="docs/images/options.png" alt="Options page: language, five palettes and five accents, startup options, tool folder"><br>**Options**: 6 languages, 5 palettes × 5 accents |
| <img src="docs/images/skills.png" alt="Skills &amp; MCP page, Skills tab: four skills installed from the verified catalog, three of them switched on for this project, and the community search below"><br>**Skills**: installed once, switched on per project | <img src="docs/images/mcp.png" alt="Skills &amp; MCP page, MCP tab: Context7 and Godot switched on, an optional API key field, a successful test, and the MCP Registry search"><br>**MCP**: remote or local in a cage, tested in one click |

The screenshots show the dark "Slate" and the "Light" palettes.

| Page | What you do there |
|---|---|
| **Overview** | the state in one sentence, current milestone, last commit, ticked plan, usage (cycles, time, list-price cost, cache); *Start*, *Stop after cycle*, *Stop now*, *Working mode* |
| **Team** | a tree: the lead and each worker with the sub-agents it called; click a sub-agent for the brief it was given and the report it handed back; the model each one runs on; who can be called, and your own sub-agents (create, edit, remove) |
| **For you** | what Claude needs from you: each task with its steps and links, a protected field for each key it asks for, files; *It's done* or *Skip: find another way*; Claude's verdict (verified, or what is still missing) and the history. A badge in the menu, a banner on Overview and a system notification announce a new task |
| **Requests** | the current request and its plan, the queued requests (time-stamped, reorderable), notes along the way; ✨ *Improve with Claude* rewrites a request clearer and more complete, and you can go back to yours; 📎 attach files to a request (specification, mock-ups, photos, PDF, data): they stay in the workshop and Claude reads them with the request |
| **Chat** | talk with Claude about the project: questions at any time (it reads the code, the plan, the log); while the pilot is stopped it can also change files (no commands) and Athanor commits them; while the pilot runs, a change becomes a note for the pilot, sent with one click; model to choose |
| **Log** | the readable log (Claude speaks, tools, results, sub-agents, pilot) or the raw JSON, filterable |
| **Manage** (in the project picker) | rename the project; move or rename its folder; point it to another folder (the plan and requests come along, the milestones are checked against what it contains); check the milestones; archive it; delete it (from the list, its workshop or the whole folder, to the recycle bin); link it to GitHub |
| **Skills & MCP** | the library (verified catalog, search in SkillsMP and the MCP Registry, GitHub link, your own skills, pasted MCP configurations), a switch per project, the API keys of the servers (stored apart, never shown) and their settings (an address, a port, a path: detected in the configuration, shown in clear with their default), *View / edit* a server's configuration, settings and program to open as JSON (pasted ones included), *Test* starts a server in its cage and lists its tools; *Sign in* once to a remote server that asks for it (OAuth), for every project; **Plugins**: the whole catalog of Claude Code plugins (official marketplace plus yours), searchable, installed in one click and switched on per project |
| **Setup & health** | One tab per mode (native Windows, WSL; Linux), the current one marked: *Install everything* for it, or switch to it. *Check everything*, *Test the sandbox* for real, *Repair* line by line, *Ask Claude* to repair or install something |
| **Settings → Claude** | the pilot's settings, the kit's `.env` (token masked, models, effort, watchdog…, rarely used ones under *Advanced*), *Save and restart after cycle* |
| **Settings → Options** | language, palette and accent, project list (removing a project never touches its folder), restart the pilots on opening, start with the computer, "Claude" desktop, password, tool folder |

**Skills & MCP, safely**: skills are loaded as a session plugin (`--plugin-dir`), so Claude only reads one when it needs it; it can read them but never change them. MCP servers are added to the session's own MCP configuration; a local one starts in a bubblewrap cage (`system/mcp-jail.sh`): system read-only, only the project and the tool caches writable, no home folder, no Windows drives, no Athanor folder, no Claude token. A *Windows program* (Roblox Studio's server, a `.exe` or a `.bat`, recognized when pasting and confirmed with the *Windows program* option) cannot be caged from WSL: it starts on Windows through WSL interop (`system/windows-mcp.sh`), with your rights, and only receives its own variables; Claude only sees its tools, and its whole process tree is stopped with the session. Requires Claude Code with `--plugin-dir` for skills.

**Plugins, taken apart**: a plugin is downloaded once into the tool folder (from GitHub, pinned to the commit its marketplace gives) and switched on per project. Its skills, commands and sub-agents are loaded with `--plugin-dir`, read-only for Claude; its MCP servers join the MCP library (local ones in the cage, keys stored apart, a remote one signed in to once for every project); its hooks, LSP servers and monitors are left out, because they would run outside the sandbox. Plugins built by a command or published only on npm are listed but not installed.

**Projects that follow their folder**: when a project points to another folder, Athanor compares the ticked milestones with what is there. In an empty folder nothing is done any more; a milestone whose listed files are missing is unticked; when the files come back, it is ticked again by itself. At the next start, Claude checks every milestone more thoroughly (`.athanor/REVERIFY`) and unticks what is really missing. Archived projects leave the list and are never restarted: *Archived projects*, at the bottom of the project list, brings them back.

**GitHub, automatically**: when you create a project, or later under *Manage*, choose *Create a private repository* or *Use one of my repositories*. A window opens once to sign in to GitHub; Athanor then gives that repository its own key (a GitHub *deploy key*, accepted for that repository and nothing else) and signs out: no GitHub sign-in is kept on your PC (you can also revoke “GitHub CLI” in your GitHub settings, the key does not need it). From then on, the pilot pushes after each milestone and at the delivery (switch it off to push only with *Push now*). Pushes are never forced: if GitHub has commits the project does not, Athanor stops and says so. Before each push, the new commits are checked for secrets (keys, tokens, `.env` files, private keys); if one is found nothing is pushed, and Claude removes it from the unpushed commits on its next cycle. A change to the repository's GitHub Actions workflows (code GitHub runs, with the repository's secrets) waits for you: check it, then *Push anyway*. The same block sets the **author of the commits** (your name, and your GitHub email or your private GitHub *noreply* address, proposed from your account) and whether Claude appears as co-author; Claude writes each commit subject as `M3: what the milestone delivers`.

**A plan you can shape**: click a milestone to see its files, dependencies and acceptance criteria; move it up or down, or remove it (the other milestones stop depending on it), or edit `PLAN.md` as a whole.

**Start with the computer**: *Open Athanor when the computer starts* opens it with its window, on Windows as in a Linux desktop session. With *Restart the pilots that were running*, the projects you started and neither stopped nor saw delivered start again. Launching Athanor while it already runs only brings its window back: there is never a second copy.

**Lock**: with a password (*Options*), the 🔒 button locks the app. Claude keeps working, but nothing can be seen or changed (token included) without the password, and the app starts locked.

**"Claude" desktop**: the programs Claude starts never open on your screen, so they never interrupt your work or a full-screen game.
- **Windows**: Unreal, Blender, a game under test and everything they start open on a Windows desktop of their own, the way the UAC prompt has its own (the gateways start them with `Athanor.exe --isole`). They cannot take the focus. *View Claude's desktop* switches the screen to it; the bar at the top, or Ctrl + Alt + B, brings you back.
- **Linux**, and the Linux programs under WSL that WSLg would show on Windows: each pilot runs a private X server (Xvfb). Claude, its tools and its MCP servers only see that screen (`system/ecran.sh`), and the user's display, X11 or Wayland, is out of their reach. *View Claude's screen* shows it live in the app.
- With the option off, new programs open on your screen again. Those already running stay where they are until they close: the app still lets you go and close them.

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

This table is the WSL mode, the default. In the native Windows mode below, there is no sandbox.

### Native Windows mode (experimental)

*Options → Execution mode → Native Windows*. The pilot runs in **Git Bash** (Git for Windows, the shell Claude Code uses on Windows) and Claude Code is the Windows program: no WSL, the project's Windows folder and Windows programs are reached directly, without the gateways, and file access is faster.

**There is no sandbox in this mode** (Claude Code has none on native Windows): Claude can read and change any file of your PC and run any program, without asking. The file guard still limits its file tools to the project and `tools/`, and its instructions tell it to stay there, but nothing enforces it for its commands. Use it for projects you trust. That includes Athanor's own files: in this mode Claude's commands could read the tool's `.env` (your token), the keys of the MCP servers and the GitHub key, and change the pilot itself.

- Requirements: Git for Windows, jq, Python, GitHub CLI and Claude Code for Windows; *Setup & health → Install everything* installs them (winget, then Claude Code's installer).
- The project must be in a folder of a Windows drive (`C:\...`, `D:\...`), not in the WSL file system. Its history (cycles, statistics) carries over between the two modes.
- The mode applies to the pilots started afterwards; stop the running ones first. When you close Athanor while native pilots run, it asks whether they go on in the background or stop.
- Claude's Linux screen does not exist in this mode; Claude's Windows desktop still keeps the programs it opens away from yours.

---

## Your PC's tools

Claude works in Linux (WSL2 on Windows) and reaches your Windows software through gateways. The bridge runs each command **outside** the sandbox, with your rights:

| Gateway | What it does |
|---|---|
| `unreal-editor`, `unreal-cmd`, `unreal-uat` | opens the Unreal editor (optionally driven through Unreal MCP), runs commandlets and tests, packages Windows and Android builds |
| `roblox-studio` | opens Roblox Studio on a place of the project (nothing if a Studio is already open) |
| `blender` | the PC's Blender, typically in the background with a Python script (`blender -b -P scene.py`); `blender --open FILE.blend` opens its window (nothing if a Blender is already open) |
| `adb` | the PC's Android tool: install and test on a phone plugged in over USB |
| `tool IMAGE CMD…` | runs a command in a disposable Docker container, with only the project mounted |
| `install-windows ID` / `--file setup.exe` | installs Windows software (winget or an installer), elevated when allowed |
| `pc-exec [--admin] program.exe` | runs a Windows program the project built or downloaded |
| `windows-var NAME PATH` | sets a Windows environment variable for the development tools (`ANDROID_HOME`, `JAVA_HOME`…) |

Installed software is detected on **every drive** (`C:`, `D:`…), and the newest version wins. The detected list is written before each cycle to `.athanor/PC_TOOLS.md`. Paths can be forced in *Settings* when detection misses them (`UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`).

**Unreal Engine (MCP)**: install **Unreal Engine** from the verified catalog of *Skills & MCP*, switch it on for the project (its address is a setting of the server) and run `commands\unreal-network.cmd` once. Claude enables the MCP plugins in the Unreal project itself, and the pilot opens the editor before each cycle. Claude drives it (actors, lights, materials, tests), runs tests and screenshots with `unreal-cmd`, and packages the Windows game with `unreal-uat` into `deliverable/`. The Unreal MCP is experimental: when it does not answer, Claude carries on without it.

**Roblox Studio (MCP)**: install **Roblox Studio** from the verified catalog of *Skills & MCP* and switch it on for the project. In Studio, *Assistant > … > Manage MCP Servers*, turn on *Enable Studio as MCP server*. Before each cycle, the pilot opens the project's first `.rbxl`/`.rbxlx` place in Studio (on Claude's desktop when that option is on), unless a Studio is already open: then Claude works in yours. Claude edits scripts and instances, runs play tests and inserts assets through Studio's built-in MCP server. It is a Windows program: it runs on Windows, outside the cage (see *Skills & MCP, safely* above). Experimental.

**Blender (MCP)**: install **Blender** from the verified catalog of *Skills & MCP* (Blender Lab's official server; needs Blender 5.1+, uv and Git for Windows) and switch it on for the project. In Blender, install the MCP add-on from [blender.org/lab/mcp-server](https://www.blender.org/lab/mcp-server/), turn on its *Auto Start*, and allow *Online Access* (*Preferences > System*). Before each cycle, the pilot opens the project's first `.blend` (or an empty Blender), unless a Blender is already open: then Claude works in yours. Claude runs Python in Blender, inspects the scene, takes screenshots and searches Blender's documentation; it saves the scene into the project as a `.blend`. The server runs the code Claude writes inside Blender with your rights, outside the cage. Experimental.

**Any MCP server that drives a program**: the program to open before each cycle is part of the server's description, in the catalog or in your library (*View / edit*, key `ouvrir`): the program (a gateway such as `blender`, or a Windows program `C:\…\name.exe`), its arguments (`{FICHIER}`: the project's file found), the project files to look for (`*.blend`), the process name or the port that says it is already open, and how long to wait for that port. Settings (`${PORT}`…) are filled in from the fields below the server. A new server of this kind needs no change to Athanor. Blender's opening is described this way; Roblox Studio and Unreal keep their own (Rojo build, editor's MCP plugin).

**Missing software**: Claude sorts it out on its own (a portable version in `tools/windows/`, else winget, else the administrator task if allowed). If an installation fails, it writes it to `.athanor/TODO_FOR_YOU.md` and finds another way.

**Mobile games (Unreal)**: Android first (portable SDK and Java, `.apk` installed on a phone plugged in over USB), iOS kept portable (`docs/IOS_PORT.md`).

**Docker (optional)**: needs Docker Desktop with *Settings > Resources > WSL integration*.

---

## Configuration

Everything is editable in the app (*Settings*), or in `.env` ([`.env.example`](.env.example) documents every key).

| Key | Role |
|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | token of your subscription (`claude setup-token`), never shared |
| `MODEL` / `FALLBACK_MODEL` / `SUBAGENT_MODEL` | lead model, model used while the lead's quota is exhausted, sub-agents model (`auto`: chosen by the lead for each call) |
| `CHEAP_MODEL` | model of the `executor` sub-agent (default `haiku`) |
| `FRAMING_MODEL` / `WORKER_MODEL` | model of the framing cycle (empty = `MODEL`), model of the parallel workers (default `sonnet`, empty = `MODEL`) |
| `FRAMING_MAX_MIN` | time box of the framing, in minutes (default 25) |
| `RELAY_CONTEXT_K` | context, in thousands of tokens, past which a fresh session takes over the milestone (default 150, 0 = never) |
| `EFFORT` | reasoning effort, `low` to `max` |
| `LANGUAGE` | language Claude works and documents in (set by the app) |
| `MAX_HOURS` | safety stop (default 168, one week) |
| `QUOTA_WAIT_MIN` | wait when the quota reset time cannot be read |
| `IDLE_MAX_MIN`, `CYCLE_MAX_MIN`, `CLOSE_AFTER_MILESTONE` | cycle watchdog, one milestone per cycle |
| `KEEP_AWAKE` | the PC stays awake while a pilot runs: `1` (default; the screen too for Roblox Studio and Unreal), `screen` (the screen always), `0` (off) |
| `PILOT_ENGINE` | `bash` (default) or `go`: the pilot in Go, parallel workers included |
| `RTK`, `TZ` | command output compression, time zone of the logs (empty = the system's) |
| `UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`, `ANDROID_JDK_DIR` | tool paths when auto-detection misses them (optional, absent by default) |

When the quota is exhausted for every model, the pilot waits for it to reopen and resumes on its own.

---

## Security

What the sandbox guarantees, and what it deliberately does not:

- **Claude's commands** run in the Claude Code sandbox (bubblewrap + seccomp). They can write only in the project folder and `tools/`. They cannot read the rest of your Linux home or the Windows drives, and cannot launch `cmd.exe` or `powershell.exe` directly. *Setup & health → Test the sandbox* proves it with a real session. Claude's file tools (Read, Edit, Write…) are limited the same way by a hook ([`system/guard.py`](system/guard.py)).
- **Windows software is an intended exit.** The gateways run outside the sandbox, with your user rights, and `pc-exec` runs any Windows program Claude places in the project or in `tools/windows`. Treat a project like code you run yourself.
- **Administrator rights are opt-in.** *Administrator rights → Repair* asks for your consent once and registers an elevated scheduled task (`Athanor - installations`). Its script is copied to `C:\ProgramData\Athanor`, which only administrators can modify. The task only runs winget packages and programs located inside your project folders or `tools\windows`. It also lets the project's programs accept local-network connections without the firewall dialog. Remove it any time with `commands\allow-admin.cmd remove`.
- **The pilot never trusts the project folder.** It keeps its own state (pid, lock, sandbox settings, instructions, counters) in `logs/`, out of Claude's reach, and never writes through a symbolic link placed in `.athanor/`. It validates every value it reads back, and runs git with hooks, filters, fsmonitor and external diff disabled. It starts Claude Code with only its own settings and MCP servers (`--setting-sources user`, `--strict-mcp-config` when the CLI supports them), and `tools/` is never in its `PATH`.
- **The app** listens on `127.0.0.1` only and rejects any other `Host` header (DNS rebinding). Only the windows it opens itself can use it: each one is opened on a single-use address that gives it a cookie, so a program of the computer that finds the port (a command Claude runs, for instance) gets neither the page nor the API. It also requires a per-session secret header on every API call and stores the password salted and hashed. It never sends `.env` secrets unless asked, and while locked it shows only "running / phase / cycle".
- **"Ask Claude" repair sessions** run outside the sandbox, in the tool folder, without `sudo`: on purpose, to repair the tooling itself. Anything that needs `sudo` or Windows is written to `logs/TODO_FOR_YOU.md` for you instead.
- **GitHub**: the sign-in used to link a project lives in a temporary folder, deleted when the link is made. Pushes use a key that GitHub accepts for the project's repository only, kept in your Linux home (`~/.local/share/athanor/github`), which the sandbox cannot read; even a leaked key cannot touch your other repositories. The pilot pushes from a private git folder: nothing in the project's `.git` (which Claude can write) is used, GitHub's server identity is checked against the keys GitHub publishes, and `GH_TOKEN`/`GITHUB_TOKEN` are removed from Claude's environment.
- **Nothing personal is ever committed**: `.env`, `logs/`, `tools/` and the app's settings are git-ignored, and projects live outside the tool folder.

Found a vulnerability? Please open a [private security advisory](https://github.com/Aprilox/Athanor/security/advisories/new) rather than a public issue.

---

## Running for days

- The pilots run in WSL without a window: you can close the app.
- The PC stays awake while a pilot runs (`KEEP_AWAKE`), even with the app closed; for Roblox Studio and Unreal the screen stays on too, because a locked Windows session draws nothing (no captures, no play tests). Do not lock the session yourself (Win + L) before a night of Roblox or Unreal, and check that the screen saver does not ask for a sign-in on resume.
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
system/                pilot.sh (the loop), parallel.sh (scheduler, workers, merges), equipe.py (the team), taches.py (tasks for you), safety.sh, bridge.py,
                       extensions.py + mcp-jail.sh (Skills & MCP), ecran.sh (Claude's screen), github.sh (GitHub),
                       settings.py (sandbox), guard.py, detect-tools.sh, athanor.py (map, archives, screenshots),
                       bin/ (gateways), windows/ (PowerShell helpers)
template/              CLAUDE.md (Claude's instructions), guides/ (the parts only some cycles need), PC_TOOLS_GUIDE.md, .claude/agents/ (the team)
docs/images/           README images
docs/releases/         what is new in each version (shown on the release page)
.env.example           every setting, documented
local/                 repairs made on this PC: a file here replaces the same one of template/ or app/catalogue.json,
                       never overwritten by an update (`athanor autotest` lists them)
logs/, tools/,         created at run time, never committed
extensions/
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

The pilots run in WSL without a window. The programs they open (an editor, a game under test) go to the "Claude" desktop, never on your screen. The PC stays awake on its own while a pilot runs.
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
| "Usage limit reached, Claude is waiting" | expected; with `FALLBACK_MODEL`, Claude goes on with the fallback model, unless the limit hit is the account's own (5 hours, the week), which the fallback model shares |
| "Connection problem" | missing or wrong token: *Settings*, or *Repair* on the *Token and model access* line |
| An installation needs administrator rights | *Setup & health* → *Administrator rights for installations* → *Repair* (once) |
| A `.sh` script fails with `$'\r'` | Windows line endings: save it with "LF" (the repository enforces it through `.gitattributes`) |

---

## Releasing

Push a version tag and GitHub builds both apps and publishes the release ([`.github/workflows/release.yml`](.github/workflows/release.yml)):

```bash
git tag v2.24.28 && git push origin v2.24.28
```

Keep the version in [`app/winres/athanor.rc`](app/winres/athanor.rc) and `versionAthanor` in [`app/commandes.go`](app/commandes.go) in step with the tag (a test checks they match), and write what is new in `docs/releases/VERSION.md` (e.g. [`docs/releases/2.24.24.md`](docs/releases/2.24.24.md)): it opens the release page.

Every push is checked on Windows, Linux and macOS ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)); `./build.sh --all` builds every target into `dist/`.

After a change of `app/` on a PC (a repair by Claude, or yours), `./athanor reparer --recompiler` (`.\Athanor.exe reparer --recompiler` on Windows) rebuilds the kit's executables there: it shows the change against the running executable's own sources, installs a portable Go outside the kit when none is found, runs `go vet`, builds, runs the new executable's autotest and only then puts it in place, keeping the previous one (`reparer --annuler` puts it back).

## Contributing

Issues and pull requests are welcome. Before opening one:

- `cd app && go vet . && GOOS=windows go vet .` must pass, as well as `go test ./...`.
- Shell scripts must pass `bash -n` and `shellcheck -S warning`.
- Comments and user-facing strings are in English. Some internal identifiers are in French, because the project started in French; keep the existing names when you touch them. New interface strings go through `t()` and get their translations in [`app/static/i18n.js`](app/static/i18n.js).

## Acknowledgements

- [Claude Code](https://claude.com/claude-code), which does the actual work.
- [VirtualDesktop](https://github.com/MScholtes/VirtualDesktop) by Markus Scholtes (MIT), compiled on first use: fallback when the "Claude" desktop cannot be created (windows moved to a virtual desktop).
- [go-webview2](https://github.com/jchv/go-webview2) (MIT) for the Windows window, and [bubblewrap](https://github.com/containers/bubblewrap) for the sandbox.

## License

[MIT](LICENSE) © Aprilox
