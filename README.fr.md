<p align="center"><img src="docs/images/logo.svg" width="96" height="96" alt="Logo Athanor"></p>

<h1 align="center">Athanor · guide en français</h1>

<p align="center"><a href="README.md">English</a> · <a href="https://github.com/Aprilox/Athanor/releases/latest">Télécharger</a></p>

> *L'athanor était le four des alchimistes, conçu pour brûler des jours entiers sans s'éteindre et sans que personne ne le surveille.*

Vous décrivez ce que vous voulez (un jeu, une application, un site, un outil…). Claude s'en occupe seul, cycle après cycle : cadrage, conception, code, contenus, tests, corrections, livraison. Claude tourne sur votre PC (dans WSL2, le Linux intégré à Windows), avec votre abonnement Claude, et peut utiliser **vos logiciels** (Unreal Engine, Blender, SDK Android, Docker…). Une barrière du système l'empêche d'écrire ou de lire en dehors du projet.

**Tout se fait depuis l'application `Athanor.exe`.** Les commandes à la main sont décrites plus bas pour qui préfère le terminal.

## L'outil et vos projets

- **L'outil** : ce dossier (`Athanor.exe`, `commands\`, `system\`, `template\`, votre `.env`). Il reste tel qu'il est sur GitHub, plus votre jeton. Il ne contient aucun projet.
- **Un projet** : n'importe quel dossier de développement de votre PC, existant ou à créer (`D:\Dev\mon-site`, un projet Unreal déjà commencé…). Vous en ajoutez autant que vous voulez, ils tournent en même temps ou non, comme des conversations : on choisit le projet dans le sélecteur en haut à gauche.
- **L'atelier `.athanor\`** : le seul sous-dossier que l'outil ajoute dans un projet. Il contient la demande (`REQUEST.md`), le plan (`PLAN.md`), le journal, les demandes en attente (`queue\`) et faites (`done\`), le script de vérification (`check.sh`), les captures d'écran, l'état du pilote et ses logs. **Supprimez-le à la fin : le projet est propre.** Il est exclu de git automatiquement (`.git\info\exclude`).

## Ce qu'il faut

- **Windows 11** (ou 10 récent) avec WSL2 ; ou **Linux**.
- Un abonnement **Claude Pro, Max ou Team** (Max pour Fable).
- Facultatif : **Unreal Engine 5.8** (pilotage par MCP), **Blender**, **Docker Desktop** (commande `tool`).

## Démarrer

1. Double-cliquez sur **`Athanor.exe`** (sous Linux : lancez `./athanor`, la version Linux rangée juste à côté ; chacune garde ses propres réglages). Au premier lancement, choisissez la langue (anglais par défaut ; français, espagnol, allemand, italien, portugais) : elle vaut pour l'application **et** pour les documents que Claude écrit dans vos nouveaux projets.
2. **Installer l'outillage** : une fenêtre installe WSL2, Claude Code, la barrière et les outils de base (mot de passe Linux demandé une fois ; la toute première fois, un redémarrage du PC peut être nécessaire, puis ouvrez « Ubuntu » une fois pour créer votre utilisateur et relancez l'installation).
3. **Vérifier et connecter** : *Installation & santé* → *Tout vérifier*. Chaque ligne en défaut a son bouton *Réparer* ou *Demander à Claude*. Pour le jeton : *Réparer* sur la ligne *Jeton* ouvre une fenêtre de connexion ; collez le jeton dans *Réglages*.
4. **Ajouter un projet** : son nom, son dossier de développement (l'explorateur intégré ou un chemin tapé ; il sera créé s'il n'existe pas) et ce que vous voulez. Les technologies sont libres sauf si vous en imposez.
5. **Démarrer**. Fermez l'application ou verrouillez-la : Claude continue dans WSL, en arrière-plan.

## L'application

| Page | Ce qu'on y fait |
|---|---|
| **Suivi** | état en une phrase, jalon en cours, dernier commit, plan coché, consommation (cycles, durée, coût catalogue, cache) ; boutons Démarrer, Arrêter après le cycle, Arrêter maintenant, Voir le bureau Claude |
| **Équipe** | une carte par session Claude (chef de projet, sous-agents) : ce qu'elle fait, sa dernière parole, son rapport |
| **Demandes** | la demande en cours, les demandes à la suite (horodatées, réordonnables), les remarques en cours de route, le fichier `REQUEST.md` complet |
| **Journal** | le journal lisible (Claude parle, outils, résultats, sous-agents, pilote) ou brut (JSON), filtrable |
| **Installation & santé** | tout vérifier, tout installer, tester la barrière pour de vrai, réparer ligne par ligne, demander à Claude de réparer ou d'installer quelque chose |
| **Réglages** | le `.env` (jeton masqué, modèles, effort, surveillance…), « Enregistrer et redémarrer après le cycle » |
| **Options** | langue, liste des projets (retirer de la liste ne touche jamais au dossier), relance des pilotes à l'ouverture, démarrage avec Windows, bureau virtuel « Claude », mot de passe, dossier de l'outil |

**Mode parallèle** (page *Suivi* → *Mode de travail*, par projet) : le plan est organisé en **vagues** de jalons indépendants ; chaque jalon d'une vague est réalisé par un **ouvrier** (sa propre session Claude, sa propre copie de travail git sur la branche `auto/Mx`), tous en même temps, puis le chef de projet fusionne les branches, lance la vérification complète, coche ce qui est réussi et note le reste dans `BUGS.md`. Beaucoup plus rapide ; consomme plus de quota (une session par ouvrier, 2 à 5 par vague). S'applique au prochain cycle : si le plan n'a pas encore de vagues, le chef le réorganise d'abord (`## Wave 1`, `## Wave 2`…, la vague 1 = squelette et contrats communs). La page *Équipe* montre une carte par ouvrier, le *Journal* étiquette leurs lignes `[M4]`, et `.athanor\WORKER_REPORTS.md` garde leurs rapports.

**Cadenas** : avec un mot de passe (Options), le bouton 🔒 verrouille l'application : Claude continue, mais rien n'est visible ni modifiable (jeton compris) sans le mot de passe. L'application démarre verrouillée.

**Bureau virtuel « Claude »** : les fenêtres que le pilote ouvre (Unreal, Blender, navigateurs de test…) sont envoyées sur un bureau Windows dédié. Vous continuez à travailler ou à jouer sans alt-tab ; *Voir le bureau Claude* / *Revenir* ou Ctrl + Win + ← / →.

## Ce que Claude peut et ne peut pas faire

| | |
|---|---|
| Écrire | seulement dans le dossier du projet (et `tools\` de l'outil pour ce qu'il installe) |
| Lire | seulement le projet, `tools\` et les fichiers du système Linux ; ni vos documents, ni le disque Windows, ni `.env` |
| Internet | oui, librement |
| Installer des outils | oui : dans `tools\` (outils Linux, logiciels Windows portables dans `tools\windows\`), ou dans un conteneur Docker jetable ; chaque installation est notée dans `tools\INSTALLS.md` |
| Installer des logiciels Windows | oui, avec winget : pour votre utilisateur, ou avec les droits administrateur si vous l'avez autorisé (ligne *Droits administrateur* de la santé, une seule demande Windows) |
| Pare-feu Windows | réglé tout seul avec la même autorisation : les programmes du projet peuvent recevoir des joueurs **du réseau local seulement** |
| Vos logiciels Windows | oui, par les passerelles prévues : `unreal-editor`, `unreal-cmd`, `unreal-uat`, `blender`, `adb`, `tool`, `pc-exec`, `windows-var`. **C'est une sortie voulue de la barrière** : ces programmes tournent avec vos droits Windows, et `pc-exec` lance n'importe quel programme Windows que Claude dépose dans le projet ou dans `tools\windows`. Traitez un projet comme du code que vous lanceriez vous-même |
| `cmd.exe`, `powershell.exe` | non, bloqués depuis la barrière (les passerelles ci-dessus restent possibles) |
| Modifier le pilote, la barrière, ses réglages | non (`system\` est hors de ses dossiers) |

Deux protections se complètent : la **barrière du système** (bac à sable de Claude Code) pour toutes ses commandes, et un **garde-fou** pour ses outils de lecture et d'écriture. *Tester la barrière* contrôle la barrière pour de vrai (une minute, très peu de quota) ; le garde-fou est un simple filtre de chemins (`system\guard.py`).

Le pilote, lui, tourne hors de la barrière et ne fait jamais confiance au dossier du projet : son état (pid, verrou, réglages de la barrière, consignes, compteurs) est dans `logs\pilotes\` de l'outil, hors de portée de Claude ; il n'écrit jamais à travers un lien symbolique posé dans `.athanor\` ; il vérifie chaque valeur relue ; il appelle git sans hooks, filtres ni fsmonitor ; il ne charge que ses propres réglages et serveurs MCP. L'application refuse un dossier de projet qui contiendrait l'outil, une racine de disque ou tout votre dossier personnel.

## Vos outils

La liste détectée est écrite avant chaque cycle dans `.athanor\PC_TOOLS.md`. Si un logiciel est installé ailleurs que dans `C:\Program Files`, indiquez son chemin dans *Réglages* (`UE_DIR`, `BLENDER_EXE`).

**Unreal Engine (MCP)** : mettez `UNREAL_MCP=1` dans *Réglages* et lancez une fois `commands\unreal-network.cmd`. Claude active lui-même les plugins du MCP dans le projet Unreal, et le pilote ouvre l'éditeur avant chaque cycle. Claude le pilote (acteurs, lumières, matériaux, tests), lance tests et captures d'écran avec `unreal-cmd`, fabrique le jeu Windows avec `unreal-uat` dans `deliverable\`. Le MCP d'Unreal est expérimental : s'il ne répond pas, Claude continue sans.

**Logiciels manquants** : Claude se débrouille seul (portable dans `tools\windows\`, sinon winget, sinon tâche administrateur si autorisée). Si une installation échoue, il l'écrit dans `.athanor\TODO_FOR_YOU.md` et continue autrement.

**Jeu mobile (Unreal)** : Android d'abord (SDK et Java portables, `.apk` installé sur le téléphone branché en USB), iOS gardé portable (`docs\IOS_PORT.md`).

**Docker (facultatif)** : `tool IMAGE COMMAND` lance une commande dans un conteneur jetable qui ne voit que le projet. Il faut Docker Desktop avec Settings > Resources > WSL integration.

## Modèles et quota

| Dans *Réglages* | Effet |
|---|---|
| `MODEL` | `claude-fable-5-1` (le plus puissant, abonnement Max), `claude-opus-5-5`, `claude-sonnet-5-5` (plus économe), `claude-haiku-5-5`, ou les alias `fable`, `opus`, `sonnet` |
| `FALLBACK_MODEL` | pris **automatiquement** quand le quota de `MODEL` est épuisé, jusqu'à sa réouverture, puis retour au principal. Vide = pause |
| `EFFORT` | vide = normal ; `medium` plus rapide ; `high`, `xhigh`, `max` réfléchissent davantage |
| `SUBAGENT_MODEL` | vide = le même ; `claude-sonnet-5-5` économise le quota |
| `CHEAP_MODEL` | modèle bon marché (par défaut `haiku`) du sous-agent `executor`, à qui le chef confie le travail mécanique : sorties de commandes longues, recherches dans tout le code, modifications en série, copies et conversions de fichiers. Le contexte et le quota du chef restent pour la réflexion. Vide = comme les autres sous-agents |

Si le quota global est épuisé (tous modèles), le pilote attend la réouverture et reprend seul.

## Optimisations automatiques

- **Un jalon par cycle** (`CLOSE_AFTER_MILESTONE`), contexte neuf à chaque cycle.
- **Surveillance** : cycle muet 30 min ou plus long que 3 h → arrêté, repris au cycle suivant.
- **RTK** : sorties de `git`, `ls`, `grep`, `find`… compressées avant que Claude les lise.
- **Captures comparées**, **carte du projet** (`.athanor\MAP.md`) refaite avant chaque cycle, **archives** des vieux jalons.
- **Consignes allégées** : le mode d'emploi des logiciels du PC est dans `PC_TOOLS_GUIDE.md`, lu seulement si le projet en a besoin.
- **Garde-fou** : jalon coché sans `CHECK: PASSED` vue par le pilote → vérification complète obligatoire au cycle suivant.

## Laisser tourner plusieurs jours

- Les pilotes tournent dans WSL sans fenêtre : vous pouvez fermer l'application.
- Empêchez la mise en veille (Paramètres > Système > Alimentation > Veille : Jamais).
- *Options* : « Ouvrir l'application à l'allumage de Windows » et « Relancer les pilotes qui tournaient ».
- Arrêt de sécurité au bout de `MAX_HOURS` (depuis le premier démarrage du projet) ; pour continuer, augmentez la valeur dans *Réglages*.

## Projet terminé, suite, nouveau projet

- **Demandes à la suite** : page *Demandes*. Dès que la demande en cours est terminée, le pilote prend la première, l'ajoute à `REQUEST.md`, rouvre le projet avec un jalon de reprise.
- **Remarque en cours de route** : page *Demandes*, lue au début du cycle suivant, sans redémarrage.
- **Rouvrir à la main** : remplacez `STATUS: DONE` par `STATUS: REOPENED` dans `.athanor\FINAL_REPORT.md`, puis Démarrer.
- **Nouveau projet** : sélecteur → *+ Nouveau projet*. **Nettoyer un projet fini** : supprimez son dossier `.athanor\` (et `.claude\` s'il existe).

## Les commandes à la main (sans l'application)

Dans `commands\` (Windows : double-clic ou `commands\start.cmd D:\Dev\mon-projet` ; Linux : les `.sh` depuis le dossier de l'outil, avec le dossier du projet en argument).

| Pour | Windows | Linux / WSL |
|---|---|---|
| Installer ou réparer | `install.cmd` | `commands/install.sh` |
| Configurer le `.env` | `configure.cmd` | `commands/configure.sh` |
| Vérifier la barrière | `test-sandbox.cmd` | `commands/test-sandbox.sh` |
| Lancer le pilote sur un projet | `start.cmd DOSSIER` | `commands/start.sh --background DOSSIER` |
| Suivre en direct | `follow.cmd DOSSIER` | `commands/follow.sh DOSSIER` |
| Arrêter tout de suite | `stop.cmd DOSSIER` | `commands/stop.sh DOSSIER` |
| Arrêter à la fin du cycle | `stop-after-cycle.cmd DOSSIER` | `commands/stop-after-cycle.sh DOSSIER` |
| Rendre le MCP d'Unreal joignable (une fois) | `unreal-network.cmd` | — |
| Autoriser les installations administrateur | `allow-admin.cmd` | — |

Sans dossier, les commandes utilisent `<outil>\projet`. La demande se lit et s'écrit dans `DOSSIER\.athanor\REQUEST.md` ; les demandes suivantes dans `DOSSIER\.athanor\queue\` (`01-….md`, prises dans l'ordre).

## Où regarder

- `.athanor\STATE.json` : phase, cycle, modèle, jalon en cours, reprise prévue ; `stats.tsv` : une ligne par cycle.
- `.athanor\PLAN.md`, `LOG.md` ; `pilot.log` (cycles, quotas, bascules) ; `logs\` (brut de chaque cycle).
- À la fin : `.athanor\FINAL_REPORT.md`, `TEST_GUIDE.md`, et le livrable dans `deliverable\`.
- Côté outil : `logs\` (réparations, test de barrière, `TODO_FOR_YOU.md`).

## Dépannage

| Symptôme | Solution |
|---|---|
| « Lancez d'abord install.cmd » / WSL en défaut dans la santé | *Tout installer* (ou `install.cmd`) n'a pas encore choisi la distribution WSL |
| Le pilote s'arrête aussitôt, message sur la barrière | *Réparer* sur la ligne *Barrière* : bubblewrap, socat ou le filtre seccomp manquent |
| Test de barrière : « cmd.exe lancé depuis la barrière » | le filtre seccomp manque : *Réparer* sur la ligne *Barrière* |
| Unreal : « MCP indisponible pour ce cycle » | éditeur 5.8 avec les plugins Unreal MCP, Auto Start Server coché, réseau WSL « mirrored » (`unreal-network.cmd`) |
| `tool` : « Docker introuvable » | lancez Docker Desktop, activez WSL integration |
| « Quota atteint, Claude patiente » | normal ; avec `FALLBACK_MODEL`, Claude continue sur le modèle de secours |
| « Problème de connexion » | jeton absent ou faux : *Réglages*, ou *Réparer* sur la ligne *Jeton* |
| Un script `.sh` échoue avec `$'\r'` | fins de ligne Windows : réenregistrez-le en « LF » |
