# Missions

*Français : [MISSIONS.fr.md](MISSIONS.fr.md)*

A mission is one sentence that several modules carry out together: **Studio** builds the project, **Servers** prepares where it runs. This page says what happens between your sentence and the lanes at work, what the orchestrator may and may not do, and what you approve. In the app, *Guide › Launch a mission* walks through it; deployments themselves are in [DEPLOY.md](DEPLOY.md).

## From a sentence to a card

1. **The sentence.** In the *What do you want to do?* bar of the home page, for example “Make a site for the Martin bakery and put it online at martin.fr, with a protected dev on @vps”. Typing `@` lists your projects, your servers and your vault entries **by name** (never a value); a choice becomes a chip. The chips under the bar add a recent project, a server or *New project*. Athanor checks every mention again (an unknown one is refused) and keeps the sentence with any secret-looking text masked.
2. **The orchestrator.** *Send* creates the mission and starts the orchestrator; the card shows “The orchestrator is preparing the card…”. It reads the state of the modules, may ask you a question, then proposes a card.
3. **The card.** What Studio will build (a new project and its folder, or one of yours), what Servers will prepare (DNS, certificates, proxy, protection, firewall), the environments (*dev*, *staging*, *prod*: address, protection, servers), and the vault entries each one gets. Athanor checks the card before showing it: an existing project or a valid new name, servers from your inventory, addresses not used by another environment, vault entries that exist. An entry the vault does not hold is shown *to fill*; the orchestrator never creates one.

What the card shows next is **worked out by Athanor, not by the orchestrator**:

| Part | How |
|---|---|
| *I will ask for your approval for* | production always; then by the policy below; hardening a server not hardened yet; unlocking the vault when an *infra* entry is used |
| *What will be touched* | the servers, the DNS names and their zones, the files `/etc/caddy/sites/<host>.caddy` and `/srv/athanor/<project>-<env>/`, the project's folder |
| *Estimate* | the milestones the orchestrator expects × the average cycle time and quota of the projects on this PC (25 minutes and “quota unknown” before a first cycle is measured), shown as a range |

## The policy

| Choice | *dev* and *staging* deployments | Servers plans | *prod* |
|---|---|---|---|
| *Ask me for production* (default) | automatic when nothing is destroyed | those of *prod* wait for you | waits for you |
| *Ask me for everything* | wait for you | every plan waits for you | waits for you |
| *Autonomous* | automatic when nothing is destroyed | as usual for each server | waits for you |

A deployment to *prod* waits for your agreement whatever the choice, and so do destructive steps on a server and anything on a *prod* server (see the Servers agent's guardrails in the README).

## Edit, launch, cancel

- **Edit.** Untick a vault entry or change the policy: the card is worked out again at once, with no agent. Write a change (“put the dev on the home server”): the orchestrator gets the card and your words and proposes a new one.
- **Launch.** Once only, under a lock, in this order: (1) the Studio lane: a new project is created (same checks as *New project*) with the Studio brief as its request, or the brief is added to an existing project's queue; its pilot starts; (2) the ticked entries are given (*code* entries to the project, *infra* entries to the environment); (3) the environments are set (*dev* and *staging* with the mission's policy, *prod* always on approval; *dev* deploys after each milestone); (4) the Servers lane: one task per environment to prepare, whose plans wait for your approval as usual. A step that fails stops the following ones, rings the bell and is written in the mission's history; what is already done stays done. If an *infra* entry is used, the vault must be unlocked first, and in native Windows mode it is refused (the card says so: untick it, switch to WSL, or *Force* an existing project on its Access page). On the computer, *Launch* asks for Athanor's password, since it gives keys. The new project's folder is checked before anything starts (a full local path, its parent writable, not taken). An existing project's environments are never weakened: their policy stays the strictest of theirs and the mission's, and their protection and servers stay as they are unless you change them yourself; the card shows “Before:” under each environment the mission changes. From a phone, environments and the project are changed on the computer only.
- **Cancel.** Before launching, *Cancel the mission* drops it: nothing has been built or deployed.

The *Missions* page lists every mission with its state and its lanes (read live from the project's pilot and the Servers task); a mission's page shows its card and links to its project and its task. The last three are on the home page.

## From your phone

A paired, unlocked phone can read a mission, answer the orchestrator, edit, launch and cancel. When the orchestrator asks you to launch, “Launch the mission …?” comes in the bell, on *For you* and as a notification: it shows the card's environments (prod included) and the ticked access, and its link opens the mission. Accepting launches it exactly as *Launch* does, and only the card it was asked for: while it waits, neither the orchestrator nor *Edit* can change the card, and a card changed anyway refuses the request (launch it again from the card). On the computer, accepting asks for Athanor's password. Such an agreement is never “always”. The boxes you untick stay unticked when the orchestrator proposes a new card. The orchestrator's questions come in the bell too, with their choices as buttons; your answer restarts it where it stopped.

## What the orchestrator can and cannot do

The orchestrator is a short Claude session started by Athanor: an empty working folder, no built-in tool (`--tools ""`), only Athanor's MCP server, no secret in its environment, a token valid for this mission only and revoked when it stops, ten minutes at most. Its tools:

| Tool | What it does |
|---|---|
| `modules_etat` | a summary of the modules: projects with their state and their plan in one line, servers with their name, state and hardening, environments with their address and protection, names and types of the vault entries — never a value or a file |
| `proposer_mission` | hands over the card; Athanor checks it and works out the rest, or answers with what to correct |
| `demander_module` | a read-only question to a module, from an allow-list (today: a project's state) |
| `question_proprietaire` | asks you a question with up to 4 choices; one open question at a time |
| `lancer_mission` | asks you to launch (the approval above); it never launches by itself |

Every other tool — Studio's, the Servers agent's, Cloudflare's, files, SSH — is refused to its token, and each call is written to the audit log. It cannot read a project's files, reach a server, see a key, build or deploy: once launched, the work is done by the project's pilot and the Servers agent, under their usual rules.

## Where it is kept

One file per mission in Athanor's folder, `hub/missions/<id>.json` (readable by your account only): the sentence, the mentions, the card, the policy, the lanes, the questions and the history. The orchestrator's workshop (`hub/missions/<id>-session/`) is deleted when its session ends.
