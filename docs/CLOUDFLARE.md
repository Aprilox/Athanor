# Cloudflare and Athanor — Cloudflare et Athanor

[English](#english) · [Français](#français)

## English

Athanor can point your domains at your servers, set up HTTPS and protect a site, through Cloudflare. It does so with an API token you create, kept in Athanor's vault. This page says which rights to give that token, what Athanor calls with it, and what the AI never gets.

### Create a token with the minimum rights

On Cloudflare's dashboard: *My Profile › API Tokens › Create Token › Create Custom Token*.

| Permission | Why |
|---|---|
| *Zone › Zone › Read* | list your zones, find the zone of a site's address |
| *Zone › DNS › Edit* | read, create, change and delete the DNS records of that address |
| *Zone › SSL and Certificates › Edit* | the HTTPS mode of the zone, and the origin certificate between Cloudflare and your server |
| *Account › Access: Apps and Policies › Edit* | **only** if a site is protected with Cloudflare Access |

*Zone Resources*: *Include › Specific zone ›* your domain. Never *All zones*, never a *Global API Key*. Then *Continue to summary › Create Token*, copy it, and paste it in Athanor: *Vault › Add*, type *Cloudflare*, *infra* scope. *Test*, on its card, shows the zones the token sees and what it may do on each. To use it, choose it among the APIs of a project's *Deployment* tab, or name it with `@` in a request on the home page.

### What Athanor calls

Every call goes to `https://api.cloudflare.com/client/v4`, from Athanor's own process, and leaves a line in the audit log (method, path, status; never a header, so never the token).

| When | Calls |
|---|---|
| *Test* (changes nothing) | `GET /user/tokens/verify`, `GET /zones`; per zone `GET …/dns_records`, `GET …/settings/ssl`, `GET /certificates?zone_id=…`, `GET /accounts/{account}/access/apps`, and one `POST …/dns_records` with an incomplete body that Cloudflare refuses, to learn whether the token may write |
| DNS (agent tools `dns_lister`, `dns_ecrire`, `dns_supprimer`) | `GET`, `POST`, `PUT`, `DELETE /zones/{zone}/dns_records…`: A, AAAA, CNAME and TXT records of the site's host name only |
| HTTPS mode (`ssl_lire`, `ssl_regler`) | `GET`, `PATCH /zones/{zone}/settings/ssl` |
| Origin certificate (`certificat_origine`) | `POST /certificates` with a signing request made by Athanor (ECDSA key generated in memory) |
| Cloudflare Access (`access_proteger`, `access_retirer`) | `GET`, `POST`, `PUT`, `DELETE /accounts/{account}/access/apps…` and `…/policies`: a self-hosted application on the host name, allowing only the e-mail addresses you give; first a `GET …/dns_records` of the name, refused unless its A, AAAA or CNAME records all go through Cloudflare's proxy. On such a site `dns_ecrire` always writes `proxied: true`, and the site's Caddy block answers 403 to any address outside Cloudflare's published ranges, so nobody goes around Access by the server's address |

Reading runs at once. Any change is a step of a plan you accept first (*For you*, the bell or your phone), unless the permission mode of the project or of the task lets it through; deleting a record, turning HTTPS off or removing Access is *destructive*, and the production site of a project from before 3.0 always waits for you. The agent only acts on the host name of the site of the project its task was launched for (for a task launched with the API alone, the zones of that API): another name or another zone gets the same answer as an unknown one.

### What never reaches the AI

- **The token.** It is an *infra* entry of the vault (it opens only with Athanor's password), decrypted in memory for each call. The Servers agent gets tools that use it, never the token: not in its prompt, its environment variables, its files, a tool's answer, the audit log, the RUNBOOK or a notification.
- **The origin certificate's private key.** Made by Athanor, kept in the vault and copied by Athanor to every server of the site (`/etc/athanor/tls/<host>.key`, owner root, group `caddy`, mode 0640: Caddy reads it, no one else; mode 0600 root where Caddy is not installed, and that server keeps Caddy's own certificate until `certificat_origine` runs again).
- **The password in front of a site.** A *mot_de_passe* entry of the vault: the web server (Caddy) receives only its bcrypt hash.
- **What Cloudflare returns** is handed to the agent as data, with any value of the vault masked: a DNS record that hides an instruction cannot make it act.

To revoke everything at once: delete the token on Cloudflare (*My Profile › API Tokens*), then delete its entry in Athanor's *Vault*.

## Français

Athanor peut faire pointer vos domaines vers vos serveurs, mettre en place le HTTPS et protéger un site, grâce à Cloudflare. Il le fait avec un jeton d'API que vous créez, gardé dans le coffre d'Athanor. Cette page dit quels droits donner à ce jeton, ce qu'Athanor appelle avec, et ce que l'IA ne reçoit jamais.

### Créer un jeton aux droits minimum

Sur le tableau de bord de Cloudflare : *My Profile › API Tokens › Create Token › Create Custom Token*.

| Permission | Pourquoi |
|---|---|
| *Zone › Zone › Read* | lister vos zones, trouver la zone de l'adresse d'un site |
| *Zone › DNS › Edit* | lire, créer, changer et supprimer les enregistrements DNS de cette adresse |
| *Zone › SSL and Certificates › Edit* | le mode HTTPS de la zone, et le certificat d'origine entre Cloudflare et votre serveur |
| *Account › Access: Apps and Policies › Edit* | **seulement** si un site est protégé par Cloudflare Access |

*Zone Resources* : *Include › Specific zone ›* votre domaine. Jamais *All zones*, jamais une *Global API Key*. Puis *Continue to summary › Create Token*, copiez-le et collez-le dans Athanor : *Coffre › Ajouter*, type *Cloudflare*, portée *infra*. *Tester*, sur sa carte, montre les zones que voit le jeton et ce qu'il peut faire sur chacune. Pour vous en servir, choisissez-le parmi les API de l'onglet *Déploiement* d'un projet, ou nommez-le avec `@` dans une demande à l'accueil.

### Ce qu'Athanor appelle

Chaque appel part vers `https://api.cloudflare.com/client/v4`, depuis le processus d'Athanor lui-même, et laisse une ligne au journal d'audit (méthode, chemin, code ; jamais un en-tête, donc jamais le jeton).

| Quand | Appels |
|---|---|
| *Tester* (ne change rien) | `GET /user/tokens/verify`, `GET /zones` ; par zone `GET …/dns_records`, `GET …/settings/ssl`, `GET /certificates?zone_id=…`, `GET /accounts/{compte}/access/apps`, et un `POST …/dns_records` au corps incomplet que Cloudflare refuse, pour savoir si le jeton peut écrire |
| DNS (outils de l'agent `dns_lister`, `dns_ecrire`, `dns_supprimer`) | `GET`, `POST`, `PUT`, `DELETE /zones/{zone}/dns_records…` : enregistrements A, AAAA, CNAME et TXT du nom d'hôte du site seulement |
| Mode HTTPS (`ssl_lire`, `ssl_regler`) | `GET`, `PATCH /zones/{zone}/settings/ssl` |
| Certificat d'origine (`certificat_origine`) | `POST /certificates` avec une demande de signature faite par Athanor (clé ECDSA générée en mémoire) |
| Cloudflare Access (`access_proteger`, `access_retirer`) | `GET`, `POST`, `PUT`, `DELETE /accounts/{compte}/access/apps…` et `…/policies` : une application *self-hosted* sur le nom d'hôte, ouverte aux seules adresses e-mail que vous donnez ; d'abord un `GET …/dns_records` du nom, refusé si ses enregistrements A, AAAA ou CNAME ne passent pas tous par le proxy de Cloudflare. Sur un tel site `dns_ecrire` écrit toujours `proxied: true`, et le bloc Caddy du site répond 403 à toute adresse hors des plages publiées par Cloudflare : personne ne contourne Access par l'adresse du serveur |

La lecture s'exécute aussitôt. Tout changement est une étape d'un plan que vous acceptez d'abord (*Pour vous*, la cloche ou votre téléphone), sauf si le mode de permission du projet ou de la tâche le laisse passer ; supprimer un enregistrement, couper le HTTPS ou retirer Access est *destructeur*, et le site de production d'un projet d'avant la 3.0 vous attend toujours. L'agent n'agit que sur le nom d'hôte du site du projet pour lequel sa tâche a été lancée (pour une tâche lancée avec l'API seule, les zones de cette API) : un autre nom ou une autre zone reçoit la même réponse qu'un nom inconnu.

### Ce qui n'atteint jamais l'IA

- **Le jeton.** C'est une entrée *infra* du coffre (elle ne s'ouvre qu'avec le mot de passe d'Athanor), déchiffrée en mémoire à chaque appel. L'agent Serveurs reçoit des outils qui s'en servent, jamais le jeton : ni dans sa consigne, ni dans ses variables d'environnement, ses fichiers, la réponse d'un outil, le journal d'audit, le RUNBOOK ou une notification.
- **La clé privée du certificat d'origine.** Faite par Athanor, gardée dans le coffre et copiée par Athanor sur chaque serveur du site (`/etc/athanor/tls/<hôte>.key`, propriétaire root, groupe `caddy`, mode 0640 : Caddy la lit, personne d'autre ; mode 0600 root là où Caddy n'est pas installé, et ce serveur garde le certificat de Caddy tant que `certificat_origine` n'est pas relancé).
- **Le mot de passe devant un site.** Une entrée *mot_de_passe* du coffre : le serveur web (Caddy) n'en reçoit que l'empreinte bcrypt.
- **Ce que renvoie Cloudflare** est remis à l'agent comme des données, toute valeur du coffre masquée : un enregistrement DNS qui cache une consigne ne peut pas le faire agir.

Pour tout révoquer d'un coup : supprimez le jeton sur Cloudflare (*My Profile › API Tokens*), puis supprimez son entrée dans le *Coffre* d'Athanor.
