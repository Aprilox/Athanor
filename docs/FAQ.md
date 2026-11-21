# FAQ and troubleshooting

<sub>Part of the [Athanor Hub README](../README.md) · [Français](FAQ.fr.md)</sub>

## FAQ

<details>
<summary><b>What does it cost?</b></summary>

Only your Claude subscription: Athanor chains Claude Code sessions, nothing else. The most capable models use quota fastest; Sonnet lasts longer. `FALLBACK_MODEL` keeps working while the main model's quota is exhausted, and `CHEAP_MODEL` handles the mechanical work. The Overview page shows the cost of every cycle at list price, which is what your subscription quota pays. With an API key instead (*Settings → Claude → Connection*), each run is billed to the API account at that price: the Overview then shows the spend against the budget you set, and the pilot waits once it is spent.
</details>

<details>
<summary><b>What can it build?</b></summary>

Anything Claude Code can build and check by itself in a folder: websites and web apps, desktop and command-line tools, games (Unreal Engine, Godot, web), Android apps, libraries. Projects that need your Windows software use the gateways. The checks are written by Claude for each project (`check.sh`), with screenshots for anything visual.
</details>

<details>
<summary><b>Can I steer it while it works?</b></summary>

Yes. A request added with *Requests → Right away* is read at the start of the next cycle. New requests are queued and can be reordered. *Stop › Finish the current milestone* finishes it, saves it, then stops, so nothing is lost. *Put on pause* stops the pilot now and makes the project's deployment wait; *Stop immediately* cuts the cycle off and cancels the server tasks of its deployment.
</details>

<details>
<summary><b>Will it get in my way while I use the PC?</b></summary>

The pilots run in the background, without a window. The programs they open (an editor, a game under test) go to the "Claude" desktop, never on your screen. The PC stays awake on its own while a pilot runs.
</details>

<details>
<summary><b>Does the project stay clean?</b></summary>

Yes: Athanor only adds `.athanor/`, kept out of git through `.git/info/exclude` (never through your `.gitignore`). Delete it when you are done and the project is as you left it. To share the workshop with the people you work with, tick *Share the workshop (.athanor/) on GitHub* in the project's GitHub settings: whoever clones the repository and adds it to Athanor picks the work up where it stopped.
</details>

<details>
<summary><b>Can I use it on several PCs?</b></summary>

Yes. Nothing is tied to a user name or a drive letter: paths come from your profile and software is detected on every drive. Download Athanor on each PC and run *Install everything* there. To go on with the same project from another PC: link it to GitHub with *Share the workshop (.athanor/) on GitHub*; on the other PC, connect to GitHub, create a project in an empty folder linked to the same repository (*Use one of my repositories*) and *Get GitHub's work* (*Start* offers it too): the plan comes along and the pilot takes it up where it stopped. Both at the same time: [Several computers](STUDIO.md#several-computers).
</details>

<details>
<summary><b>Do I need a server or Cloudflare?</b></summary>

No. Studio works alone, as Athanor always did: the other modules wait until you add a server or a Cloudflare token. A project with no server in its *Deployment* tab is only built.
</details>

<details>
<summary><b>Can Claude see my server keys or deploy to production without me?</b></summary>

No. A server's key is an *infra* entry of the vault: it never leaves Athanor's process, and an agent only calls tools that Athanor runs with it. Destructive steps always wait for your accord, with Athanor's password retyped (a paired, unlocked phone can accept too), and so do the deployments of an older project's production site. The one exception is a project you put in *Ignore permissions* yourself, in Athanor's window with its password: it acts without asking, and the audit log says so. See the [security model](SECURITY.md#security-model).
</details>

<details>
<summary><b>What if I forget Athanor's password?</b></summary>

The project keys still reopen on this PC through the system's keystore. The *infra* keys (your servers', Cloudflare's) are wrapped by the password alone, so they cannot be recovered: add them again (a server's key can be made again by Athanor and placed with the server's password). Nothing else is lost: projects, settings and servers' sheets stay.
</details>

<details>
<summary><b>I used Athanor before the hub. What changes for my projects?</b></summary>

Nothing you have to do. The update moves nothing: projects, settings, the request queue, the team, remote access and old links stay where they were. At the first start the keys of each project's `.athanor/secrets.env`, of *Skills & MCP* and of Athanor's `.env` are copied into the vault (those files are left as they are), and each project key is granted back to its project, so the pilots get exactly what they had. *Undo the import* removes what was added.
</details>

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| *WSL and the Linux distribution* failing in Setup & health | *Install everything* in the WSL tab has not set up the Linux distribution yet (open "Ubuntu" once after the first restart, then run it again) |
| The pilot stops at once, with a message about the sandbox | *Repair* on the *System sandbox* line: bubblewrap, socat or the seccomp filter is missing |
| Sandbox test: "CMD.EXE LAUNCHED FROM THE SANDBOX" | the seccomp filter is missing: *Repair* on the *System sandbox* line |
| Unreal: "MCP unavailable for this cycle" | editor 5.8 with the Unreal MCP plugins, *Auto Start Server* checked, WSL network "mirrored" (`Athanor.exe reseau-unreal`) |
| `tool`: "Docker not found" | start Docker Desktop, enable *WSL integration* |
| "Usage limit reached, Claude is waiting" | expected; with `FALLBACK_MODEL`, Claude goes on with the fallback model, unless the limit hit is the account's own (5 hours, the week), which the fallback model shares |
| "Connection problem" | missing or wrong token: *Settings*, or *Repair* on the *Token and model access* line |
| An installation needs administrator rights | *Setup & health* → *Administrator rights for installations* → *Repair* (once) |
| “The vault is locked” on an action of Servers or a deployment | unlock Athanor (its password, in the window or on a paired phone): the *infra* key opens only with it |
| A server refused with “the host key changed” | the server was reinstalled, or someone is in between: check the new fingerprint on the server itself (`for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done`), then remove and add the server again |
