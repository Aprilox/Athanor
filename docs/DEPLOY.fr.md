# Déployer avec Athanor

*English: [DEPLOY.md](DEPLOY.md)*

Comment Athanor met un projet en ligne : qui peut lancer un déploiement, les trois méthodes, ce qu'il écrit sur vos serveurs, comment il revient en arrière, comment il surveille le site, et ce qui n'atteint jamais l'IA. Dans l'application, *Guide › Mettre un site en ligne* vous accompagne ; les environnements et Cloudflare sont dans [CLOUDFLARE.md](CLOUDFLARE.md).

## Qui lance un déploiement

- **L'agent Serveurs**, dans une tâche lancée *Pour l'environnement* sur la fiche d'un serveur : son plan contient une étape `deployer` (classée *changement*), qui ne s'exécute qu'une fois le plan accepté. Son premier plan choisit la méthode ; Athanor la retient dans l'environnement pour que les déploiements suivants se passent d'agent.
- **Le pilote du projet**, avec l'outil `demander_deploiement` du hub : *dev* et *staging* suivent leur politique (*Demander mon accord* ou *Automatique quand rien n'est détruit*) ; *prod* vous attend toujours.
- **Le relais après un jalon** : avec *Déployer en dev à chaque jalon livré* coché sur la carte *dev*, chaque jalon coché envoie le dernier commit du projet en *dev*. Jamais en *prod*.
- **Vous** : *Revenir à la version d'avant* sur la carte d'un environnement, *Réessayer* ou *Revenir* sur un incident.

Un déploiement en *prod*, ou un retour arrière en *prod*, demande votre accord à chaque fois : le mot de passe d'Athanor retapé, ou un téléphone appairé. Un accord est lié au SHA complet du commit, aux serveurs, au projet et à qui l'a demandé ; il ne resservira pas pour un autre commit, et rien ne touche le serveur avant lui.

## Les méthodes

| Méthode | Quand | Ce qui tourne sur le serveur | Retour arrière |
|---|---|---|---|
| `statique` | un site de fichiers (HTML, un front construit) | Caddy sert `current/<dossier publié>` | `current` pointe sur la version d'avant |
| `compose` | le projet a un `compose.yaml` / `docker-compose.yml`, ou un `Dockerfile` (Athanor écrit alors un petit fichier compose) | `docker compose -p <projet>-<env> up -d` dans le dossier de la version | `compose up` de la version d'avant |
| `k3s` | seulement si vous le demandez, un serveur par environnement | `kubectl apply -k` (ou `-f`) d'un dossier du dépôt, puis attente de ses Deployments, StatefulSets et DaemonSets | `kubectl rollout undo` de ce que la version a changé |

**D'où vient le code.** Soit **git** : le serveur va chercher le commit exact sur GitHub avec la clé de déploiement du projet (en lecture seule sur le dépôt, créée dans le panneau *Environnements*), avec la clé d'hôte de github.com épinglée ; soit une **archive** : Athanor fait un `tar.gz` du commit sur ce PC (`git archive`, ou le résultat de la commande de construction de l'environnement lancée dans une copie propre de ce commit, dans un dossier temporaire et dans la même barrière que le pilote ; en mode Windows natif, refusée sauf si le projet est forcé sur sa page *Accès*, et journalisée), l'envoie en SFTP et contrôle son SHA-256. Une archive avec un chemin absolu, un `..` ou un lien qui sort de la version est refusée avant que rien ne soit écrit.

**Ce que seul le propriétaire peut laisser passer.** Un fichier de construction, un fichier compose, un Dockerfile ou un fichier du dossier des manifestes k3s modifié (par rapport au dernier déploiement accepté par le propriétaire) rend le déploiement destructif : il attend le propriétaire même en dev ou en staging avec la politique *auto*, une règle ou *Déployer en dev à chaque jalon*. Sans l'accord du propriétaire pour ce déploiement-là, une configuration Compose (lue par `docker compose config --format json` sur le serveur) qui demande `privileged`, `cap_add`, `devices`, `security_opt`, l'espace réseau, PID, IPC, utilisateurs ou cgroup de l'hôte, ou un chemin de l'hôte hors de la version est refusée avant tout démarrage ; de même pour des manifestes k3s avec un `hostPath`, `hostNetwork`/`hostPID`/`hostIPC`, un conteneur privilégié ou une base distante.

## Ce qu'Athanor écrit sur un serveur

```
/srv/athanor/<projet>-<env>/
  releases/<AAAAMMJJ-hhmmss>-<sha7>/   un dossier par version (les 5 plus récentes sont gardées)
  current  -> releases/…               la version servie, basculée d'un coup
  previous -> releases/…               la version d'avant, pour revenir en arrière
/etc/caddy/sites/<hôte>.caddy          le fichier Caddy du site (Caddyfile : import sites/*)
/etc/athanor/deploy/<projet>           la clé de déploiement (source git), 0600 root, effacée juste après le fetch
/etc/athanor/tls/                      le certificat d'origine (Cloudflare), root et Caddy seulement
```

Les étapes, en code Go, jamais un script shell écrit par un agent : préparer la version (récupérer ou recevoir, contrôler) → construire si demandé → basculer `current` et `previous` d'un coup (`ln -sfn` puis `mv -T`) → `caddy validate`, puis recharger Caddy → **vérification externe**. Un échec avant la bascule laisse le site tel qu'il était ; une vérification en échec après elle marque le déploiement *Échec* et devient un incident (plus bas), où vous choisissez la suite. Un seul déploiement à la fois par environnement. Quand un fichier de build (`package.json`, lockfiles, `Makefile`, scripts nommés par la commande) a changé depuis le dernier déploiement que vous avez accordé, Athanor vous demande l'accord, même en dev `auto` et pour le relais après jalon ; cet accord ne devient jamais une règle. Pour Compose, le `.env` de la version est écrit `0600 root` à partir des entrées *code* du coffre accordées à l'environnement, envoyé sur l'entrée standard, jamais sur une ligne de commande.

**La vérification externe**, depuis ce PC : statut HTTP 2xx ou 3xx, un certificat valide et ses jours restants, les en-têtes de sécurité (HSTS, `X-Content-Type-Options`, `Referrer-Policy`, `X-Frame-Options` ou une CSP `frame-ancestors`) et, pour un environnement protégé, que le site refuse un visiteur sans identifiant (401 pour le mot de passe, une redirection vers Cloudflare Access).

## Retour arrière et historique

L'historique de chaque environnement (commit, méthode, version, état, vérification, qui l'a lancé, dates) est gardé dans le dossier d'Athanor (`modules/servers/deploiements/<projet>-<env>.json`) et montré dans *Déploiements* sur sa carte. *Revenir à la version d'avant* remet `previous` en `current` (ou `compose up` / `rollout undo`), recharge et revérifie.

## Santé et incidents

Toutes les 15 minutes par défaut (*Santé des sites déployés* sur la page *Serveurs* : de 5 à 60 minutes, ou désactivé), pour chaque environnement déployé : la vérification externe, le certificat (une alerte sous 14 jours, un incident sous 3), le disque et la mémoire de ses serveurs (au-delà de 90 %). Un déploiement en échec, ou un site trouvé mal en point deux fois de suite, fait sonner la cloche et les téléphones appairés en mots simples (ce qui a échoué, ce qui a été tenté) avec trois actions : *Réessayer* (le dernier déploiement à nouveau), *Revenir à la version d'avant*, *Je prends la main* (arrête les déploiements automatiques de cet environnement jusqu'à ce que vous les remettiez, et ouvre la fiche du serveur).

## Durcir un serveur neuf

*Durcir ce serveur*, sur la fiche d'un serveur, est un plan fixe écrit par le code d'Athanor, montré étape par étape et exécuté une fois accepté : un utilisateur non root avec sudo et la clé d'Athanor, les clés SSH seules, `PermitRootLogin no` (contrôlé par `sshd -t`, avec un retour arrière programmé), ufw qui refuse tout sauf 22, 80 et 443 (avec un retour arrière programmé), `unattended-upgrades`, `systemd-timesyncd`, Caddy depuis son dépôt officiel. Une étape déjà en place est sautée.

## Ce qui n'atteint jamais l'IA

La clé SSH du serveur, la clé de déploiement, le jeton Cloudflare, la clé privée du certificat d'origine, le mot de passe du proxy et les valeurs du `.env` du projet restent au coffre. Le moteur d'Athanor s'en sert lui-même, en mémoire ; les agents et le pilote reçoivent des outils qui s'en servent, jamais les valeurs, et ces valeurs sont tenues hors des réponses des outils, de la cloche, des notifications, du journal d'audit, de l'environnement des sessions et des fichiers de ce PC (`app/deploiements_temoin_test.go` le vérifie). Le kubeconfig de k3s (`/etc/rancher/k3s/k3s.yaml`) fait partie des fichiers que l'agent ne peut pas lire. Chaque déploiement, accord et retour arrière est inscrit au journal d'audit.
