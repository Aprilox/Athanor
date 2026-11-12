# What Athanor does on a new server — Ce qu'Athanor fait sur une machine neuve

[English](#english) · [Français](#français)

## English

This page lists everything Athanor reads, writes or runs on a server you add, and what it never does. Athanor installs no agent, no daemon and no package of its own: it connects over SSH when it needs to, and leaves.

### When you add the server

- **Reads the host key fingerprint** and shows it to you. Nothing goes further until you tick *It is my server's fingerprint*; a fingerprint that changes later is refused.
- **Puts one public key** (ed25519, made by Athanor, its private part kept in the vault's *infra* scope) at the end of `~/.ssh/authorized_keys` of the user you gave — only if you chose *The server's password, once*. With *A key of the vault* or *A new key made by Athanor*, it writes nothing: you put the key there yourself. The password is used once and never saved.
- **Offers** *Cut password logins*, never on its own. If you accept: one file, `/etc/ssh/sshd_config.d/00-athanor.conf`, checked with `sshd -t`, SSH reloaded, then a new connection with the key; if anything fails, the file is removed again.

### The sheet (read only)

One read-only shell script, run on adding, on *Refresh* and every 6 hours while the vault's *infra* key is open: `/etc/os-release`, `uname`, `nproc`, memory and disk, `ss` (or `netstat`), `command -v` for Docker, Compose, k3s, kubectl, nginx, Caddy and ufw, `ufw status`, running services and containers. `sudo -n` is used only where it needs no password. Nothing is written.

### Anything else: only through a plan you accepted

The Servers agent can read without asking (files, logs, services, containers), except secrets: `/etc/shadow`, `/etc/gshadow`, `~/.ssh/*` (other than `authorized_keys`), `*.env` files and private keys are refused. **Any change** — installing, writing a file, a service, a firewall rule, Docker — runs only as a step of a plan you accepted, once, exactly as shown. Destructive steps and any step on a *prod* server always wait for you.

What an accepted plan can leave on the server, besides the change itself:

| Where | What | When |
|---|---|---|
| `/var/backups/athanor/prev/<path>` | the previous version of a replaced file (mode 0600, root; the result gives its former mode and owner; `~/.athanor-backups/prev/<path>` when the account has neither root nor sudo) | a file is written or sent over SFTP |
| `/var/backups/athanor/<plan>-<step>.tar.gz` | an archive of what a destructive step deletes (mode 0600; the 10 newest are kept) | before a destructive step; the step is refused if the archive fails or would take more than half the free space |
| `/var/backups/athanor/annul-<id>/` | the state before a firewall or SSH change, and its scheduled undo (a `systemd-run` timer `athanor-annul-<id>`, or a detached `sh`) | before a `ufw` rule or a write under `/etc/ssh/`; removed once confirmed or undone (after 5 minutes without *Keep this change*; an undo leaves a short `.log` beside it) |

### What Athanor never does

- It never sends the server's key or password to an agent, a page, a phone or the audit log.
- It never runs a change, a destructive command or anything on a *prod* server without your agreement.
- It never accepts a host key it was not shown, nor a fingerprint that changed.
- It never installs software of its own, opens a port, creates a user or touches another server than the one of the task.
- *Remove this server* forgets it in Athanor and changes nothing on the server (its key in `authorized_keys` stays until you remove it).

The history of what was done, the backups and how to go back are in the server's `RUNBOOK.md` (*Runbook* on its sheet), kept by Athanor on your computer, never on the server.

## Français

Cette page liste tout ce qu'Athanor lit, écrit ou exécute sur un serveur que vous ajoutez, et ce qu'il ne fait jamais. Athanor n'installe ni agent, ni démon, ni paquet à lui : il se connecte en SSH quand il en a besoin, puis repart.

### À l'ajout du serveur

- **Lit l'empreinte de la clé d'hôte** et vous la montre. Rien ne va plus loin tant que vous n'avez pas coché *C'est l'empreinte de mon serveur* ; une empreinte qui change ensuite est refusée.
- **Pose une clé publique** (ed25519, créée par Athanor, sa partie privée gardée dans la portée *infra* du coffre) à la fin du `~/.ssh/authorized_keys` de l'utilisateur donné — seulement si vous avez choisi *Le mot de passe du serveur, une fois*. Avec *Une clé du coffre* ou *Une nouvelle clé créée par Athanor*, il n'écrit rien : c'est vous qui posez la clé. Le mot de passe sert une fois et n'est jamais enregistré.
- **Propose** *Couper la connexion par mot de passe*, jamais de lui-même. Si vous acceptez : un fichier, `/etc/ssh/sshd_config.d/00-athanor.conf`, vérifié avec `sshd -t`, SSH rechargé, puis une nouvelle connexion avec la clé ; si quelque chose échoue, le fichier est retiré.

### La fiche (lecture seule)

Un seul script shell en lecture seule, lancé à l'ajout, sur *Actualiser* et toutes les 6 heures tant que la clé *infra* du coffre est ouverte : `/etc/os-release`, `uname`, `nproc`, mémoire et disque, `ss` (ou `netstat`), `command -v` pour Docker, Compose, k3s, kubectl, nginx, Caddy et ufw, `ufw status`, services actifs et conteneurs. `sudo -n` seulement là où il ne demande pas de mot de passe. Rien n'est écrit.

### Tout le reste : seulement par un plan que vous avez accepté

L'agent Serveurs peut lire sans demander (fichiers, journaux, services, conteneurs), sauf les secrets : `/etc/shadow`, `/etc/gshadow`, `~/.ssh/*` (hors `authorized_keys`), les fichiers `*.env` et les clés privées sont refusés. **Tout changement** — installer, écrire un fichier, un service, une règle de pare-feu, Docker — ne s'exécute que comme étape d'un plan que vous avez accepté, une fois, exactement comme montré. Les étapes destructrices et toute étape sur un serveur *prod* vous attendent toujours.

Ce qu'un plan accepté peut laisser sur le serveur, en plus du changement lui-même :

| Où | Quoi | Quand |
|---|---|---|
| `/var/backups/athanor/prev/<chemin>` | la version d'avant d'un fichier remplacé (droits 0600, root ; le résultat donne ses anciens droits et propriétaire ; `~/.athanor-backups/prev/<chemin>` si le compte n'a ni root ni sudo) | un fichier est écrit ou envoyé par SFTP |
| `/var/backups/athanor/<plan>-<étape>.tar.gz` | une archive de ce qu'une étape destructrice supprime (droits 0600 ; les 10 plus récentes sont gardées) | avant une étape destructrice ; l'étape est refusée si l'archive échoue ou prendrait plus de la moitié de la place libre |
| `/var/backups/athanor/annul-<id>/` | l'état d'avant un changement de pare-feu ou de SSH, et son annulation programmée (minuteur `systemd-run` `athanor-annul-<id>`, ou un `sh` détaché) | avant une règle `ufw` ou une écriture sous `/etc/ssh/` ; retiré une fois confirmé ou annulé (au bout de 5 minutes sans *Garder ce changement* ; une annulation laisse à côté un court journal `.log`) |

### Ce qu'Athanor ne fait jamais

- Il ne donne jamais la clé ni le mot de passe du serveur à un agent, une page, un téléphone ou le journal d'audit.
- Il n'exécute jamais un changement, une commande destructrice ni quoi que ce soit sur un serveur *prod* sans votre accord.
- Il n'accepte jamais une clé d'hôte qu'il ne vous a pas montrée, ni une empreinte qui a changé.
- Il n'installe jamais de logiciel à lui, n'ouvre pas de port, ne crée pas d'utilisateur et ne touche à aucun autre serveur que celui de la tâche.
- *Retirer ce serveur* l'oublie dans Athanor et ne change rien sur le serveur (sa clé dans `authorized_keys` reste jusqu'à ce que vous la retiriez).

L'historique de ce qui a été fait, les sauvegardes et la façon de revenir en arrière sont dans le `RUNBOOK.md` du serveur (*Runbook* sur sa fiche), gardé par Athanor sur votre ordinateur, jamais sur le serveur.
