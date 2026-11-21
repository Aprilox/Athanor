# Référence : réglages, commandes, dossiers

<sub>Fait partie du [README d'Athanor Hub](../README.fr.md) · [English](REFERENCE.md)</sub>

## macOS en détail

**Ce que *Tout installer* met sur le Mac**, uniquement par les installeurs officiels : [Homebrew](https://brew.sh) s'il manque (son propre installeur demande votre mot de passe et Entrée) ; sept outils en ligne de commande via Homebrew : `git`, `jq`, `ripgrep`, `node`, `imagemagick`, `ffmpeg`, `gh` ; Claude Code (l'installeur d'Anthropic, dans `~/.local/bin`, avec une ligne dans `~/.zshrc`) ; RTK, qui raccourcit la sortie des commandes pour économiser des tokens, dans le dossier d'Athanor. Rien qui ait une fenêtre, rien qui démarre tout seul. La barrière est celle de macOS (Seatbelt) : seulement vérifiée. Tout est écrit dans `logs/install-mac.log`, dans le dossier d'Athanor.

**Sa fenêtre est la sienne**, avec le moteur web du Mac (WebKit) dans Athanor : aucun navigateur ne s'ouvre. **Pendant qu'il tourne**, Athanor a son icône dans le Dock et une flamme près de l'horloge (la barre des menus) ; les deux montrent les pilotes au travail et rouvrent la fenêtre. Le bouton rouge cache la fenêtre : Athanor continue. **Quitter** (⌘Q, ou le menu de l'icône du Dock) avec des pilotes en cours propose de garder Athanor dans la barre des menus (l'icône du Dock part, les pilotes continuent, la flamme reste) ou de les arrêter et quitter ; *Quitter* depuis la flamme quitte, les pilotes continuant ou non, au choix. Le Mac reste éveillé tant qu'un pilote tourne (`caffeinate` ; un MacBook refermé s'endort quand même, sauf branché sur secteur avec un écran externe).

Claude travaille dans Seatbelt, la barrière intégrée à macOS (celle qu'utilise Claude Code) : il n'écrit que dans le projet. Les serveurs MCP ont leur propre cage Seatbelt. Unreal, Blender, Roblox Studio, Xcode et le SDK Android sont trouvés à leurs places habituelles et Claude les lance directement ; les programmes Windows et Docker via `tool` restent réservés à Windows et Linux. Sur le Mac, les programmes que Claude ouvre s'affichent sur votre écran (pas de bureau à part comme sous Windows).


## Ce qu'une mise à jour garde

Votre jeton, vos réglages, vos projets, `logs/` et `tools/` ne sont jamais touchés par une mise à jour. Une installation faite avant la 2.26 (un dossier avec `Athanor.exe`, `template/` et `athanor-config.json` côte à côte) reste où elle est : mettez-y le nouvel exécutable, ou utilisez le bouton. Une mise à jour fait aussi le ménage dans un tel dossier : les fichiers des anciennes versions qu'aucune version n'utilise plus (`commands/`, les scripts bash et Python de `system/`, `docs/`, le README…) partent s'ils sont exactement tels que livrés ; un fichier que vous avez modifié est déplacé dans `logs/ancien-kit/<date>/`, et `logs/nettoyage-<date>.txt` liste ce qui est parti. Rien d'autre du dossier n'est touché.

## Configuration

Tout se modifie dans l'application (*Réglages*), ou dans `.env` ([`.env.example`](../.env.example) documente chaque clé). Dans *Réglages → Claude*, un « i » à côté d'un réglage explique ce que change une valeur plus basse ou plus haute. La durée du cadrage (`FRAMING_MAX_MIN`) peut aussi se régler projet par projet : *Gérer → Mode de travail → Cadrage le plus long* (25, 45, 60 ou 90 minutes, ou une durée à vous de 10 à 240 minutes dans la case « Autre » ; « Comme les Réglages » reprend la valeur commune).

| Clé | Rôle |
|---|---|
| `CLAUDE_AUTH` | comment Athanor se connecte à Claude : `subscription` (par défaut : le jeton ci-dessous, le quota de votre forfait) ou `api` (une clé API, chaque passage facturé au compte API) |
| `CLAUDE_CODE_OAUTH_TOKEN` | jeton de votre abonnement (`claude setup-token`), jamais partagé |
| `ANTHROPIC_API_KEY` | clé API (mode `api`), créée sur console.anthropic.com › API keys, jamais partagée |
| `API_BUDGET_USD` | mode `api` : le maximum à dépenser, en dollars (le crédit restant sur le compte API) ; vide = pas de limite |
| `API_SPENT_USD` | mode `api` : ce qui est déjà dépensé au moment où vous l'enregistrez (le décompte continue à partir de là) |
| `MODEL` / `FALLBACK_MODEL` / `SUBAGENT_MODEL` | modèle du chef, modèle utilisé tant que le quota du chef est épuisé, modèle des sous-agents (`auto` : choisi par le chef à chaque appel) |
| `CHEAP_MODEL` | modèle du sous-agent `executor` (par défaut `haiku`) |
| `FRAMING_MODEL` / `WORKER_MODEL` | modèle du cycle de cadrage (vide = `MODEL`), modèle des ouvriers en parallèle (par défaut `sonnet`, vide = `MODEL`) |
| `FRAMING_MAX_MIN` | durée maximale du cadrage, en minutes (25 par défaut) |
| `RELAY_CONTEXT_K` | contexte, en milliers de tokens, au-delà duquel une nouvelle session reprend le jalon (110 par défaut, 0 = jamais) |
| `EFFORT` | effort de réflexion, de `low` à `max` |
| `LANGUAGE` | langue dans laquelle Claude travaille et documente (réglée par l'application) |
| `MAX_HOURS` | limite de temps de travail, en heures : seul compte le temps où le pilote travaille (ni ses attentes, ni le temps où il est arrêté) ; 0 = illimité, par défaut, le pilote s'arrêtant de lui-même à la livraison ; un projet peut avoir la sienne : *Gérer → Mode de travail* |
| `QUOTA_WAIT_MIN` | attente quand l'heure de réouverture du quota est illisible |
| `IDLE_MAX_MIN`, `CYCLE_MAX_MIN`, `CLOSE_AFTER_MILESTONE` | surveillance des cycles, un jalon par cycle |
| `KEEP_AWAKE` | le PC reste éveillé tant qu'un pilote tourne : `1` (par défaut ; l'écran aussi pour Roblox Studio et Unreal), `screen` (l'écran toujours), `0` (désactivé) |
| `RTK`, `TZ` | compression des sorties de commandes, fuseau horaire des journaux (vide = celui du système) |
| `PONYTAIL` | les règles de Ponytail pour les cycles qui construisent : `1` (par défaut), `0` (désactivé) |
| `UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`, `ANDROID_JDK_DIR` | chemins des outils quand la détection automatique les manque (facultatifs, absents par défaut) |

Si le quota est épuisé pour tous les modèles, le pilote attend la réouverture et reprend seul.

**Permissions.** *Réglages → Permissions* règle le mode par défaut de deux usages : l'*Assistant SSH et SFTP* (« L'assistant à côté du terminal et des fichiers d'un serveur, sur chaque serveur sans mode propre. ») et les *Projets* (« Chaque projet sans mode propre, sur ses serveurs. ») ; un serveur et un projet peuvent encore choisir leur propre mode. Les quatre modes sont ceux du [déploiement d'un projet](SERVERS.fr.md#le-déploiement-dun-projet) ; *Manuel* par défaut, et *Ignorer les permissions* seulement depuis la fenêtre d'Athanor, avec son mot de passe.

**Abonnement ou clé API.** *Réglages → Claude → Connexion → Se connecter avec* : votre abonnement Claude (son jeton) ou une clé API. Seule la connexion choisie parvient à Claude Code (avec les deux, Claude Code facturerait la clé). Avec une clé API, les plugins et connecteurs d'un compte claude.ai (Gmail, Drive… ajoutés dans l'appli Claude) ne fonctionnent pas ; les Skills, les serveurs MCP et toutes les extensions d'Athanor fonctionnent pareil. *Installation & santé* vérifie la clé, et *Suivi* affiche la **dépense API** à la place du quota de l'abonnement : ce que Claude Code a indiqué pour chaque passage (prix catalogue) depuis le début du décompte, face à `API_BUDGET_USD` s'il est défini (dépensé, restant). *Déjà dépensé* (Réglages) fait partir le décompte de ce que le compte a déjà dépensé. Un **arrêt automatique** en dollars (*Suivi → Dépense API → Arrêt auto*, par exemple à 80 $) arrête le pilote après le cycle qui l'atteint : le coût d'un cycle est connu à sa fin, il s'arrête donc autour de ce montant, jamais au milieu d'un cycle. Une fois le budget dépensé, ou quand l'API dit que le compte n'a plus de crédit, le pilote attend au lieu d'échouer cycle après cycle. *Recompter à partir de maintenant* (crédit ajouté), un budget plus haut ou une autre clé, et il reprend aussitôt. Enregistrer une nouvelle clé, ou un premier budget, relance le décompte ; un budget augmenté ensuite le garde. Dans un terminal : `athanor depense [zero]`.

**Une nouvelle connexion, aussitôt.** Le pilote relit la connexion à chaque cycle et toutes les quelques secondes de ses attentes : un jeton ou une clé enregistré dans *Réglages* pendant qu'il attend un quota, un problème de connexion, le budget ou le crédit (un autre compte, une clé qui a encore du crédit) sert aussitôt, l'attente écourtée. Le modèle de secours et la pause des ouvriers de l'ancien compte sont levés.

---

## Les commandes sans l'application

Tout ce que fait l'application est une commande de l'exécutable (`Athanor.exe` sous Windows, le `./Athanor-Linux-x64` téléchargé, `Athanor.app/Contents/MacOS/athanor` sur le Mac, ou `athanor` dans le dossier d'Athanor ; `Athanor.exe wsl COMMANDE …` la lance dans WSL) :

| Commande | Pour |
|---|---|
| `installer` | tout installer (Linux, macOS, le côté WSL) ; Windows : `installer-windows` (WSL) ou `installer-natif` |
| `configurer` | connexion (jeton de l'abonnement ou clé API, budget), modèles, options (la langue se règle dans l'application) |
| `depense [zero \| arret DOLLARS \| arret off]` | la connexion, et avec une clé API la dépense face au budget ; `zero` la recompte à partir de maintenant, `arret` règle l'arrêt automatique après le cycle |
| `demarrer [--background] DOSSIER` | lancer le pilote sur un projet |
| `suivre DOSSIER` | suivre en direct |
| `arreter --apres-cycle DOSSIER` | arrêter proprement à la fin du jalon en cours |
| `arreter DOSSIER` | arrêter tout de suite |
| `tester-cage` | prouver la barrière avec une vraie session |
| `autoriser-admin [--retirer]` | autoriser ou retirer les installations administrateur (Windows) |
| `reseau-unreal` | réseau WSL pour le MCP d'Unreal (Windows, une fois) |
| `arret-urgence` | l'arrêt d'urgence de l'Athanor en marche (pilotes arrêtés, jetons du hub révoqués, approbations refusées, coffre verrouillé) |
| `mcp-client appairer NOM [--sorte desktop\|code]` | appairer Claude Desktop ou Claude Code au MCP d'Athanor (voir [docs/MCP.md](MCP.fr.md)) |
| `module servers [--nom NOM]` / `module appairer` | faire tourner Athanor sans fenêtre comme son module Serveurs, sur un VPS, et l'appairer à un hub (voir [docs/REMOTE.md](REMOTE.fr.md)) |
| `tui` / `gui` | l'application dans le terminal / dans sa fenêtre (Linux, macOS ; voir [Dans un terminal](HUB.fr.md#dans-un-terminal)) |
| `aide` | toutes les commandes |

Sans dossier, les commandes utilisent `projet` dans le dossier d'Athanor. La demande se lit et s'écrit dans `DOSSIER/.athanor/REQUEST.md`, les demandes suivantes dans `DOSSIER/.athanor/queue/` (un fichier par demande, prises dans l'ordre).

## Où regarder

- `.athanor/STATE.json` : phase, cycle, modèle, jalon en cours, reprise prévue ; `stats.tsv` : une ligne par cycle.
- `.athanor/PLAN.md`, `LOG.md` ; `pilot.log` (cycles, quotas, bascules) ; `logs/` (brut de chaque cycle).
- À la fin : `.athanor/FINAL_REPORT.md`, `TEST_GUIDE.md`, et le livrable dans `deliverable/`.
- Dans le dossier d'Athanor : `logs/` (réparations, test de la barrière, `TODO_FOR_YOU.md`).

---

## Le dossier d'Athanor

```
Athanor.exe, athanor   l'application (Windows ; athanor seul sous Linux et macOS, et sous Windows celui du mode WSL) ;
                       tout le reste en est une commande : le pilote, ses passerelles (liens de system/bin), installer…
template/              les textes pour Claude, remis en place à chaque mise à jour
.env, .env.example     vos réglages et votre jeton, et le modèle de chaque réglage
athanor-config*.json   les réglages de l'application (projets, options)
logs/, tools/          les journaux et dossiers privés des pilotes ; les outils que Claude installe (tools/INSTALLS.md)
extensions/, system/   la bibliothèque Skills & MCP ; ce qu'a le PC et les commandes de Claude
local/                 réparations faites sur ce PC (un fichier ici remplace le même de template/ ou app/catalogue.json,
                       jamais écrasé par une mise à jour)
modules/, hub/         les données de chaque module (un dossier par module, créé quand il en a besoin) ; celles du hub (jalons du jour)
app/                   les sources Go, écrites seulement quand une réparation en a besoin (Recompiler Athanor, une réparation de Claude)
```

## Organisation du dépôt

```
app/                   les sources Go de l'application, l'interface (HTML, CSS, JS sans dépendance), ressources Windows ;
                       go run . construire construit Athanor.exe et athanor, qui portent chacun template/, .env.example,
                       LICENSE et ces sources (app/embarque.go)
template/              CLAUDE.md (consignes de Claude), guides/ (les parties dont seuls certains cycles ont besoin), PC_TOOLS_GUIDE.md, .claude/agents/ (l'équipe)
docs/images/           images du README
docs/*.md              guides des modules : déploiements, Cloudflare, MCP, Athanor distants, un serveur de test, une machine neuve, anciennes missions
docs/releases/         les nouveautés de chaque version (affichées sur la page de la version)
.env.example           tous les réglages, documentés
```

Dans chaque projet, `.athanor/` contient REQUEST.md, PLAN.md, LOG.md, queue/, done/, check.sh, STATE.json, stats.tsv et logs/. C'est la seule chose qu'Athanor ajoute.

---

## Publier une version

Poussez un tag de version : GitHub lance d'abord toutes les vérifications de la CI sur le commit du tag (tests sous Windows, Linux et macOS, exécutable unique, toutes les cibles), puis compile les applications et publie la version ([`.github/workflows/release.yml`](../.github/workflows/release.yml)). Si une seule vérification échoue (ou la suite), rien n'est publié : le tag est supprimé, ainsi que la version si elle avait déjà été créée, et les mises à jour d'Athanor ne la proposent jamais. Corrigez, commitez, puis poussez à nouveau le même tag (`git tag -f v2.42.0 && git push origin v2.42.0`) :

```bash
git tag v3.0.0 && git push origin v3.0.0
```

Gardez la version de [`app/winres/athanor.rc`](../app/winres/athanor.rc) et `versionAthanor` dans [`app/commandes.go`](../app/commandes.go) alignées sur le tag (un test vérifie qu'elles concordent), et écrivez les nouveautés dans `docs/releases/VERSION.md` (par ex. [`docs/releases/2.31.0.md`](releases/2.31.0.md)) : elles ouvrent la page de la version.

Chaque push est vérifié sous Windows, Linux et macOS ([`.github/workflows/ci.yml`](../.github/workflows/ci.yml)), l'exécutable unique compris (rien d'écrit avant l'accord, dossier d'Athanor installé, rien à côté du fichier) ; `cd app && go run . construire --all` compile toutes les cibles dans `dist/`. Une version publie `Athanor-Windows-x64.exe`, `Athanor-Windows-ARM64.exe`, `Athanor-macOS.zip` (Athanor.app, signé ad hoc sur une machine Mac de GitHub), `Athanor-Linux-x64`, `Athanor-Linux-ARM64` et `SHA256SUMS.txt`, sous les mêmes noms à chaque version ; les mises à jour d'Athanor téléchargent ces mêmes fichiers.

Après une modification de `app/` sur un PC (une réparation par Claude, ou la vôtre : `athanor reparer --diff` écrit d'abord les sources dans le dossier d'Athanor si elles n'y sont pas), `./athanor reparer --recompiler` (`.\Athanor.exe reparer --recompiler` sous Windows) y recompile les exécutables du kit : il montre la modification par rapport aux sources de l'exécutable en cours, installe un Go portable hors du kit s'il n'en trouve pas, lance `go vet`, compile, lance l'autotest du nouvel exécutable et seulement ensuite le met en place, en gardant l'ancien (`reparer --annuler` le remet).
