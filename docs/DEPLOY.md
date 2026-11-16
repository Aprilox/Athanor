# Deploying with Athanor

*Français : [DEPLOY.fr.md](DEPLOY.fr.md)*

How Athanor puts a project online: who can start a deployment, the three methods, what it writes on your servers, how it goes back, how it watches the site, and what never reaches the AI. In the app, *Guide › Put a site online* walks through it; environments and Cloudflare are in [CLOUDFLARE.md](CLOUDFLARE.md).

## Who starts a deployment

- **The Servers agent**, in a task launched *For the environment* on a server's sheet: its plan holds a `deployer` step (classed as a *change*), which runs only once you accept the plan. Its first plan chooses the method; Athanor records it in the environment so that later deployments need no agent.
- **The pilot of the project**, with the hub's `demander_deploiement` tool: an environment follows its policy, set by its mission (*Ask for my approval* or *Automatic when nothing is destroyed*); a *Production* environment always waits for you.
- **The relay after a milestone**: on the environment a mission links to the project, each ticked milestone sends the project's last commit there. Never to a *Production* environment.
- **You**: *Go back to the previous version* on an environment's card, *Retry* or *Go back* on an incident.

A deployment to a *Production* environment, or a going back there, needs your agreement each time: Athanor's password retyped, or a paired phone. An agreement is bound to the full commit SHA, the servers, the project and who asked; it cannot be reused for another commit, and nothing touches the server before it.

## The methods

| Method | When | What runs on the server | Going back |
|---|---|---|---|
| `statique` | a site of files (HTML, a built front end) | Caddy serves `current/<published folder>` | `current` points to the version before |
| `compose` | the project has a `compose.yaml` / `docker-compose.yml`, or a `Dockerfile` (Athanor then writes a small compose file) | `docker compose -p <project>-<env> up -d` in the version's folder | `compose up` of the version before |
| `k3s` | only when you ask for it, one server per environment | `kubectl apply -k` (or `-f`) of a folder of the repository, then waits for its Deployments, StatefulSets and DaemonSets | `kubectl apply` of the previous version again |

**Where the code comes from.** Either **git**: the server fetches the exact commit from GitHub with the project's deployment key (read-only on the repository, created in the *Environments* panel), using a pinned github.com host key; or an **archive**: Athanor makes a `tar.gz` of the commit on this PC (`git archive`, or the output of the environment's build command run in a clean copy of that commit, in a temporary folder and inside the pilot's barrier; in native Windows mode, refused unless the project is forced on its *Access* page, and logged), sends it by SFTP and checks its SHA-256. An archive with an absolute path, a `..`, or a link leaving the version is refused before anything is written.

**What only the owner can let through.** A changed build file, compose file, Dockerfile or file of the k3s manifests' folder (compared with the last deployment the owner accepted) makes the deployment destructive: it waits for the owner even on an environment with the *auto* policy, a rule or the deployment after each milestone. Without the owner's accord for that very deployment, a Compose configuration (read with `docker compose config --format json` on the server) that asks for `privileged`, `cap_add`, `devices`, `security_opt`, the host's network, PID, IPC, user or cgroup namespace, or a host path outside the version is refused before anything starts; so are k3s manifests with a `hostPath`, `hostNetwork`/`hostPID`/`hostIPC`, a privileged container or a remote base.

## What Athanor writes on a server

```
/srv/athanor/<project>-<env>/
  releases/<YYYYMMDD-hhmmss>-<sha7>/   one folder per version (the 5 newest are kept)
  current  -> releases/…               the version served, switched atomically
  previous -> releases/…               the version before, for going back
/etc/caddy/sites/<host>.caddy          the site's Caddy file (Caddyfile: import sites/*)
/etc/athanor/deploy/<project>          the deployment key (git source), 0600 root, removed right after the fetch
/etc/athanor/tls/                      the origin certificate (Cloudflare), root and Caddy only
```

The steps, in Go code, never a shell script written by an agent: prepare the version (fetch or receive, check) → build if asked → switch `current` and `previous` at once (`ln -sfn` then `mv -T`) → `caddy validate`, then reload Caddy → **external check**. A failure before the switch leaves the site as it was; a failed check after it marks the deployment *Failed* and becomes an incident (below), where you choose what happens next. One deployment at a time per environment. When a build file (`package.json`, lockfiles, `Makefile`, scripts named by the command) changed since the last deployment you accepted, Athanor asks for your accord, even in dev `auto` and for the after-milestone relay; that accord never becomes a rule. For Compose, the version's `.env` is written `0600 root` from the vault's *code* entries granted to the environment, sent on standard input, never on a command line.

**The external check**, from this PC: HTTP status 2xx or 3xx, a valid certificate and its days left, the security headers (HSTS, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options` or a CSP `frame-ancestors`) and, for a protected environment, that the site refuses a visitor without credentials (401 for the password, a redirection to Cloudflare Access).

## Going back and history

Each environment's history (commit, method, version, state, check, who started it, dates) is kept in Athanor's folder (`modules/servers/deploiements/<project>-<env>.json`) and shown as *Deployments* on its card. *Go back to the previous version* serves the version noted before it starts on every server (`current` points at it; `compose up` / `kubectl apply` of it), reloads and checks again; if a server or the check fails, every server is put back on the version it left. *Retry* of a failed way back aims at the same version. On an environment under the *auto* policy, a version that fails its check after the switch goes back by itself (never on a *Production* one); the bell still tells it. A deployment that failed before its switch changed nothing: *Go back* is not offered.

## Health and incidents

Every 15 minutes by default (*Health of the deployed sites* on the *Servers* page: 5 to 60 minutes, or off), for each deployed environment: the external check, the certificate (a warning under 14 days, an incident under 3), the disk and memory of its servers (over 90 %). A failed deployment, or a site found unwell twice in a row, rings the bell and the paired phones in plain words (what failed, what was tried) with three actions: *Retry* (the last deployment again), *Go back to the previous version*, *I take over* (stops the automatic deployments of this environment until you turn them on again, and opens the server's sheet).

## Hardening a new server

*Harden this server*, on a server's sheet, is a fixed plan written by Athanor's code, shown step by step and run once you accept it: a non-root user with sudo and Athanor's key, SSH keys only, `PermitRootLogin no` (checked with `sshd -t`, with a scheduled undo), ufw refusing all but 22, 80 and 443 (with a scheduled undo), `unattended-upgrades`, `systemd-timesyncd`, Caddy from its official repository. A step already in place is skipped.

## What never reaches the AI

The server's SSH key, the deployment key, the Cloudflare token, the origin certificate's private key, the proxy password and the values of the project's `.env` stay in the vault. Athanor's engine uses them itself, in memory; agents and the pilot get tools that use them, never the values, and these values are kept out of tool answers, the bell, notifications, the audit log, session environments and files on this PC (`app/deploiements_temoin_test.go` checks it). The k3s kubeconfig (`/etc/rancher/k3s/k3s.yaml`) is among the files the agent may not read. Every deployment, agreement and going back is written in the audit log.
