# Deploying with Athanor

*Français : [DEPLOY.fr.md](DEPLOY.fr.md)*

How Athanor puts a project online: who can start a deployment, the three methods, what it writes on your servers, how it goes back, how it watches the site, and what never reaches the AI. In the app, *Guide › Put a site online* walks through it; where a project goes online, and when, is set in its *Deployment* tab ([README](../README.md#a-projects-deployment)); Cloudflare is in [CLOUDFLARE.md](CLOUDFLARE.md).

## Who starts a deployment

- **Athanor itself**, at the moment the project chose in its *Deployment* tab: *After each milestone* (each ticked milestone sends the project's last commit) or *At delivery* (each delivery, a later one included). A project without a server is never deployed.
- **The Servers agent**, for the first deployment only: a task that Athanor launches by itself at that moment, or that you launch with *Prepare now*. The agent reads the project and your plan; its own plan holds a `deployer` step (classed as a *change*) and chooses the method. Athanor records it with the project's deployment so that the next deployments need no agent. A task you launch *For the project* on a server's sheet can deploy the same way.
- **The pilot of the project**, with the hub's `demander_deploiement` tool: the request waits for you (the bell, *For you*, your phone), unless you chose *Never ask again for this project* or the project is in the *Ignore permissions* mode.
- **You**: *Go back to the previous version* in the project's *Deployment* tab, *Retry* or *Go back* on an incident.

An agreement is bound to the full commit SHA, the servers, the project and who asked; it cannot be reused for another commit, and nothing touches the server before it. It is given with Athanor's password retyped, or from a paired phone. Two cases always ask for it, whoever starts the deployment and whatever was accepted before: build files that changed (below), and the production site of a project from before 3.0 (or a site on a server that carries one), for each deployment and each going back. Only the *Ignore permissions* mode, chosen for the project in Athanor's window, lets them through, and the audit log writes each one.

## The methods

| Method | When | What runs on the server | Going back |
|---|---|---|---|
| `statique` | a site of files (HTML, a built front end) | Caddy serves `current/<published folder>` | `current` points to the version before |
| `compose` | the project has a `compose.yaml` / `docker-compose.yml`, or a `Dockerfile` (Athanor then writes a small compose file) | `docker compose -p <project>-site up -d` in the version's folder | `compose up` of the version before |
| `k3s` | only when you ask for it, on one server | `kubectl apply -k` (or `-f`) of a folder of the repository, then waits for its Deployments, StatefulSets and DaemonSets | `kubectl apply` of the previous version again |

**The web server.** By default Athanor writes the site's Caddy file and reloads Caddy. When the deployment plan names another web server (nginx, Apache…), the agent of the first deployment sets that server up once, to serve `current/<published folder>` (`statique`) or to proxy to the published port (`compose`), and records the method with `proxy: aucun`: from then on Athanor only prepares each version and switches `current`, writes no Caddy file and never installs Caddy. A project's **other servers** (a database, a worker…, chosen in its mission) are prepared with the plan and never deployed to.

**Where the code comes from.** Either **git**: the server fetches the exact commit from GitHub with the project's deployment key (read-only on the repository, made by *Create the deployment key* in the *Deployment* tab), using a pinned github.com host key; or an **archive**: Athanor makes a `tar.gz` of the commit on this PC (`git archive`, or the output of the deployment's build command run in a clean copy of that commit, in a temporary folder and inside the pilot's barrier; in native Windows mode, refused unless the project is forced on its *Access* page, and logged), sends it by SFTP and checks its SHA-256. An archive with an absolute path, a `..`, or a link leaving the version is refused before anything is written.

**What only the owner can let through.** A changed build file, compose file, Dockerfile or file of the k3s manifests' folder (compared with the last deployment the owner accepted) makes the deployment destructive: it waits for the owner even with a *Never ask again* rule, after a milestone or at delivery. Without the owner's accord for that very deployment, a Compose configuration (read with `docker compose config --format json` on the server) that asks for `privileged`, `cap_add`, `devices`, `security_opt`, the host's network, PID, IPC, user or cgroup namespace, or a host path outside the version is refused before anything starts; so are k3s manifests with a `hostPath`, `hostNetwork`/`hostPID`/`hostIPC`, a privileged container or a remote base.

## What Athanor writes on a server

```
/srv/athanor/<project>-site/           (<project>-dev, -staging or -prod for a project from before 3.0)
  releases/<YYYYMMDD-hhmmss>-<sha7>/   one folder per version (the 5 newest are kept)
  current  -> releases/…               the version served, switched atomically
  previous -> releases/…               the version before, for going back
/etc/caddy/sites/<host>.caddy          the site's Caddy file (Caddyfile: import sites/*)
/etc/athanor/deploy/<project>          the deployment key (git source), 0600 root, removed right after the fetch
/etc/athanor/tls/                      the origin certificate (Cloudflare), root and Caddy only
```

The steps, in Go code, never a shell script written by an agent: prepare the version (fetch or receive, check) → build if asked → switch `current` and `previous` at once (`ln -sfn` then `mv -T`) → `caddy validate`, then reload Caddy → **external check**. A failure before the switch leaves the site as it was; a failed check after it marks the deployment *Failed* and becomes an incident (below), where you choose what happens next. One deployment at a time per site. When a build file (`package.json`, lockfiles, `Makefile`, scripts named by the command) changed since the last deployment you accepted, Athanor asks for your accord, even for the deployments it starts by itself; that accord never becomes a rule. For Compose, the version's `.env` is written `0600 root` from the vault's *code* entries granted to the project, sent on standard input, never on a command line.

**The external check**, from this PC: HTTP status 2xx or 3xx, a valid certificate and its days left, the security headers (HSTS, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options` or a CSP `frame-ancestors`) and, for a protected site, that it refuses a visitor without credentials (401 for the password, a redirection to Cloudflare Access).

## Going back and history

The history of a project's deployments (commit, method, version, state, check, who started it, dates) is kept in Athanor's folder (`modules/servers/deploiements/<project>-site.json`) and shown as *Deployments* in its *Deployment* tab. *Go back to the previous version* serves the version noted before it starts on every server (`current` points at it; `compose up` / `kubectl apply` of it), reloads and checks again; if a server or the check fails, every server is put back on the version it left. *Retry* of a failed way back aims at the same version. A version that fails its check after the switch stays online until you decide (the incident below); only a project from before 3.0 that was set to deploy automatically still goes back by itself, never on its production site, and the bell tells it. A deployment that failed before its switch changed nothing: *Go back* is not offered.

## Health and incidents

Every 15 minutes by default (*Health of the deployed sites* on the *Servers* page: 5 to 60 minutes, or off), for each deployed site: the external check, the certificate (a warning under 14 days, an incident under 3), the disk and memory of its servers (over 90 %). A failed deployment, or a site found unwell twice in a row, rings the bell and the paired phones in plain words (what failed, what was tried) with three actions: *Retry* (the last deployment again), *Go back to the previous version*, *I take over* (stops the automatic deployments of this site until you turn them on again, and opens the server's sheet).

## Hardening a new server

*Harden this server*, on the sheet of a server Athanor reaches with a key (not offered while it connects with a password), is a fixed plan written by Athanor's code, shown step by step and run once you accept it: a non-root user with sudo and Athanor's key, SSH keys only, `PermitRootLogin no` (checked with `sshd -t`, with a scheduled undo), ufw refusing all but 22, 80 and 443 (with a scheduled undo), `unattended-upgrades`, `systemd-timesyncd`, Caddy from its official repository. A step already in place is skipped.

## What never reaches the AI

The server's SSH key, the deployment key, the Cloudflare token, the origin certificate's private key, the proxy password and the values of the project's `.env` stay in the vault. Athanor's engine uses them itself, in memory; agents and the pilot get tools that use them, never the values, and these values are kept out of tool answers, the bell, notifications, the audit log, the sessions' environment variables and files on this PC (`app/deploiements_temoin_test.go` checks it). The k3s kubeconfig (`/etc/rancher/k3s/k3s.yaml`) is among the files the agent may not read. Every deployment, agreement and going back is written in the audit log.
