<p align="center">
  <img src="docs/images/logo.svg" width="112" height="112" alt="Logo Athanor">
</p>

<h1 align="center">Athanor</h1>

<h3 align="center">Laissez Claude Code construire des projets entiers pendant que vous faites autre chose.</h3>

<p align="center">
  <i>L'athanor était le four des alchimistes, conçu pour brûler des jours entiers sans s'éteindre et sans que personne ne le surveille.</i>
</p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-MIT-5fe3c1?style=flat-square" alt="Licence MIT"></a>
  <img src="https://img.shields.io/badge/plateforme-Windows%2010%2F11%20%C2%B7%20Linux%20%C2%B7%20macOS-1b2430?style=flat-square" alt="Windows 10/11, Linux et macOS">
  <a href="https://claude.com/claude-code"><img src="https://img.shields.io/badge/moteur-Claude%20Code-d97757?style=flat-square" alt="Fonctionne avec Claude Code"></a>
  <img src="https://img.shields.io/badge/app-Go%20%C2%B7%20un%20seul%20fichier-7fd8ee?style=flat-square" alt="Application Go en un seul fichier">
  <img src="https://img.shields.io/badge/interface-6%20langues-7c8da1?style=flat-square" alt="6 langues">
</p>

<p align="center">
  <a href="#démarrage-rapide"><b>Démarrage rapide</b></a> ·
  <a href="#comment-ça-marche"><b>Comment ça marche</b></a> ·
  <a href="#mode-parallèle"><b>Mode parallèle</b></a> ·
  <a href="#mode-éco"><b>Mode éco</b></a> ·
  <a href="#économiser-le-quota"><b>Économiser le quota</b></a> ·
  <a href="#plusieurs-ordinateurs"><b>Plusieurs ordinateurs</b></a> ·
  <a href="#lapplication"><b>L'application</b></a> ·
  <a href="#sécurité"><b>Sécurité</b></a> ·
  <a href="#faq"><b>FAQ</b></a> ·
  <a href="#dépannage"><b>Dépannage</b></a> ·
  <a href="README.md"><b>English</b></a>
</p>

<p align="center">
  <a href="https://github.com/Aprilox/Athanor/releases/latest"><img src="https://img.shields.io/badge/T%C3%A9l%C3%A9charger-Windows%20%C2%B7%20Linux%20%C2%B7%20macOS-5fe3c1?style=for-the-badge" alt="Télécharger pour Windows, Linux et macOS"></a>
</p>

<p align="center">
  <img src="docs/images/overview.png" alt="Page Suivi d'Athanor : trois ouvriers réalisent des jalons en parallèle, le plan du projet organisé en vagues, le quota de l'abonnement avec un arrêt automatique et l'avancement vers le dernier jalon" width="100%">
</p>

Vous décrivez ce que vous voulez : un jeu, un site, une application, un outil. Athanor lance [Claude Code](https://claude.com/claude-code) **cycle après cycle** sur votre propre PC, avec votre abonnement Claude. Chaque cycle termine **un jalon du plan** : Claude fait le travail, lance les vérifications du projet, coche le jalon et le commite. Puis une session neuve prend le suivant, jusqu'à la livraison. Vous pouvez fermer l'application, la verrouiller ou aller dormir : le four continue de brûler.

> [!NOTE]
> Athanor est un projet open source indépendant. Il n'est ni affilié à Anthropic ni approuvé par Anthropic. Il pilote la commande officielle Claude Code avec votre propre abonnement.

---

## Points forts

| | |
|---|---|
| **Une boucle autonome** | Un jalon par cycle et un contexte neuf à chaque fois : les tours restent économes et le projet ne dérive pas. Quota atteint → pause jusqu'à sa réouverture, ou bascule sur un modèle de secours. Erreur → nouvel essai avec un délai croissant. Cycle muet ou trop long → coupé et repris. PC redémarré → reprise là où il s'était arrêté. Tout l'état est dans des fichiers. |
| **Mode parallèle** | Un interrupteur par projet. Un plan fin avec dépendances ; jusqu'à 10 ouvriers réalisent des jalons indépendants en même temps, chacun dans sa copie de travail git ; le pilote fusionne chacun dès qu'il termine et lance le suivant, et le chef vérifie une fois à la fin. |
| **Mode éco** | Un interrupteur par projet, quand le quota compte plus que la vitesse : le chef fait le travail lui-même au lieu de le confier à des sous-agents, une seule revue à la livraison, 4 images au plus par cycle, des sorties de commandes plus courtes. Le modèle et l'effort restent tels que vous les avez réglés. |
| **Ponytail, intégré** | Activé par défaut : Claude réalise chaque jalon avec la plus petite modification complète (les règles de [Ponytail](https://github.com/dietrichgebert/ponytail)), jamais moins que la demande. Le Suivi montre ce qui est mesuré : le cache, ce que RTK a épargné, et les cycles avec et sans, côte à côte. |
| **Plusieurs ordinateurs** | Deux, trois ou dix ordinateurs construisent un même projet ensemble par son dépôt GitHub, chacun avec un compte qui peut y envoyer (celui du propriétaire, ou celui d'un contributeur : pas besoin d'organisation) : chacun prend un jalon qu'aucun autre ne construit, d'abord ceux pour lesquels il est le meilleur (une grosse carte graphique pour la 3D, un réseau rapide pour les gros téléchargements), et ils fusionnent le travail les uns des autres après chaque jalon. Le Suivi montre quels ordinateurs sont là et ce que chacun construit. |
| **Une vraie équipe** | Un chef de projet et des sous-agents : `scout`, `architect`, `creator`, `developer`, `tester`, `critic`, et un `executor` bon marché (Haiku par défaut) pour le travail mécanique. Les éclaireurs lisent pour le cadrage, les tests indépendants et la critique finale sont toujours délégués. |
| **Skills & MCP** | Donnez à Claude de l'expertise et des outils : un catalogue vérifié (skills d'Anthropic, superpowers, Context7, Serena, Playwright, GitHub…), une recherche dans des milliers de skills communautaires (SkillsMP) et dans le registre officiel MCP, n'importe quel skill depuis un lien GitHub, vos propres skills, n'importe quel serveur MCP collé depuis son README, et les plugins Claude Code du catalogue officiel (et de tout autre que vous ajoutez), comme ceux de Shopify ou de Stripe. Activés projet par projet, pour le chef comme pour les ouvriers en parallèle. Les serveurs MCP locaux tournent dans une cage, loin de vos fichiers et du jeton Claude. |
| **Des tâches pour vous** | Ce que Claude ne peut pas faire lui-même (créer un compte, payer, recevoir un code par SMS, générer une clé) devient une tâche sur la page *Pour vous*, avec les étapes exactes ; en attendant, Claude continue le reste, et le pilote se met en pause s'il n'y a plus rien d'autre à faire. Vous répondez *c'est fait* (avec un texte, des fichiers, des clés dans un champ protégé) ou *passer : trouve une autre solution* ; Claude vérifie aussitôt et vous dit si ça marche ou ce qui manque encore. |
| **Une barrière** | Les commandes de Claude tournent sous bubblewrap + seccomp. Elles ne peuvent écrire que dans le projet et ne lisent jamais le reste de votre machine ; Internet reste ouvert. |
| **Vos logiciels** | Unreal Engine, Blender, le SDK Android, Docker et winget sont joignables par un pont contrôlé, commande par commande. |
| **Aucune fenêtre UAC** | Sur autorisation. Un seul consentement, puis Claude installe et lance ce dont le projet a besoin avec les droits administrateur, sans rien vous demander. |
| **Une application de bureau** | Un seul fichier, qui se met à jour en un clic. Elle montre l'équipe en direct, le plan, l'avancement et le quota de l'abonnement (avec arrêts automatiques), le journal lisible et la file des demandes, permet de discuter avec Claude du projet, et lance des vérifications qui se réparent en un clic. Elle fait aussi tourner plusieurs projets à la fois et garde les programmes que Claude lance sur leur propre bureau, loin de vous. Thèmes, 6 langues. |
| **Aussi dans un terminal** | Sous Linux et macOS, `./athanor tui` affiche toute l'application dans le terminal, comme btop : les projets, le suivi, le journal en direct, les demandes, la discussion et les vérifications, au clavier et à la souris. En SSH, ou sur un Linux sans écran, elle s'ouvre d'elle-même ; `./athanor gui` force la fenêtre. |
| **Depuis votre téléphone** | Activez *À distance*, scannez un QR code, et Athanor est sur votre téléphone (ou un autre ordinateur), de n'importe où : les mêmes pages en direct, démarrer et arrêter, discuter, les demandes. Par un tunnel Cloudflare, rien à configurer ; seuls les appareils que vous appairez entrent, et un clic en retire un. Depuis un terminal aussi (page *0*) : un serveur sans écran appaire un téléphone. |
| **GitHub, connecté une fois** | Connectez chaque ordinateur à votre compte GitHub une fois (*Réglages → Options → GitHub*). Un projet se lie ensuite à un nouveau dépôt privé, ou à tout dépôt que votre compte peut modifier, sans fenêtre, et ses commits y partent après chaque jalon. La clé reste hors de portée de Claude ; chaque envoi est vérifié à la recherche de secrets et n'est jamais forcé. Deux personnes peuvent se relayer sur un même dépôt, ou plusieurs ordinateurs le construire en même temps. |
| **Des projets propres** | Un projet est n'importe quel dossier de votre PC, et Athanor garde ses propres fichiers dans un dossier à lui. Athanor n'y ajoute qu'un atelier `.athanor/`, avec ses documents de travail (exigences, design, architecture), tenu hors de git sur votre ordinateur seulement, jamais via votre `.gitignore` (sauf si vous choisissez de le partager sur GitHub avec ceux qui travaillent avec vous) : supprimez-le à la fin et il ne reste aucune trace. |
| **Économe par conception** | Règles de lecture ciblées, vérification rapide pendant le jalon et complète une seule fois, captures assemblées en planches, carte du projet refaite avant chaque cycle, archives automatiques, compression des sorties de commandes. La page *Suivi* montre en direct quelle part de chaque session est relue depuis le cache. |

---

## Démarrage rapide

**Il faut :** Windows 10/11, Linux ou macOS, et un abonnement Claude (Pro, Max ou Team). Facultatif : Unreal Engine 5.8, Blender, Docker Desktop.

### 1. Récupérer Athanor

Un seul fichier (un zip sur le Mac). Depuis la [**dernière version**](https://github.com/Aprilox/Athanor/releases/latest) :

| Votre système | Téléchargement |
|---|---|
| **Windows 10 / 11** | [**Athanor-Windows-x64.exe**](https://github.com/Aprilox/Athanor/releases/latest/download/Athanor-Windows-x64.exe) (un PC ARM, Snapdragon… : [Athanor-Windows-ARM64.exe](https://github.com/Aprilox/Athanor/releases/latest/download/Athanor-Windows-ARM64.exe)) |
| **macOS** 11 ou plus récent | [**Athanor-macOS.zip**](https://github.com/Aprilox/Athanor/releases/latest/download/Athanor-macOS.zip) : **Athanor.app**, pour les Mac Apple Silicon comme Intel ; voir [macOS, pas à pas](#macos-pas-à-pas) |
| **Linux** | [**Athanor-Linux-x64**](https://github.com/Aprilox/Athanor/releases/latest/download/Athanor-Linux-x64) (un PC ARM : [Athanor-Linux-ARM64](https://github.com/Aprilox/Athanor/releases/latest/download/Athanor-Linux-ARM64)) |

Gardez le fichier où vous voulez (sur le Bureau, par exemple ; sur le Mac, Athanor.app dans *Applications*) : c'est lui qui ouvre Athanor, et il n'écrit jamais rien à côté de lui.

> [!TIP]
> Windows SmartScreen peut afficher *« Windows a protégé votre ordinateur »* au premier lancement, parce que l'application n'est pas signée. Cliquez sur *Informations complémentaires → Exécuter quand même*.

### 2. Premier lancement

La première fois, Athanor dit où il va ranger ses fichiers et demande avant d'écrire quoi que ce soit (*Non* : il se ferme, rien n'a été écrit) :

| Système | Dossier d'Athanor |
|---|---|
| Windows | `%LOCALAPPDATA%\Athanor` |
| Linux | `~/.local/share/athanor` |
| macOS | `~/Library/Application Support/Athanor` |

Tout ce qui est à lui y va : sa copie de l'exécutable, ses réglages et votre jeton, ses journaux, les outils qu'il installe. Dans vos projets, il n'ajoute qu'un dossier `.athanor/`.

**Windows**

1. Double-cliquez sur **`Athanor-Windows-x64.exe`**, puis *Oui*. Choisissez la langue, le thème et l'accent : le guide du premier démarrage vous accompagne ensuite. La langue vaut pour l'application **et** pour les documents que Claude écrit dans vos nouveaux projets.
2. **Installer** : une installation neuve démarre en **Windows natif** (rien d'autre à installer : *Tout installer* pose Git pour Windows, Python, Node.js, quelques petits outils et Claude Code pour Windows). Pour Claude dans une barrière, ouvrez l'onglet **WSL** d'*Installation & santé* : *Tout installer* y installe WSL2, Claude Code et la barrière (Linux demande un mot de passe une fois ; la toute première fois, un redémarrage peut être nécessaire, ouvrez ensuite « Ubuntu » une fois pour créer votre utilisateur et relancez l'installation). Une courte visite montre ensuite le mode, *Installation & santé*, un nouveau projet et *Démarrer*.
3. **Se connecter** : *Installation & santé* → *Tout vérifier*. Sur la ligne *Jeton et accès au modèle*, cliquez sur *Réparer* : une fenêtre vous connecte à votre compte Claude. Collez le jeton dans *Réglages*.
4. **Facultatif, recommandé** : *Installation & santé* → *Droits administrateur pour les installations* → *Réparer*. Windows vous demande votre accord **une seule fois** ; Claude n'aura plus jamais besoin d'un clic UAC.
5. **Ajouter un projet** : un nom, son dossier (existant ou à créer, sur n'importe quel disque) et ce que vous voulez. Les technologies sont libres sauf si vous en imposez. ✨ *Améliorer avec Claude* transforme quelques mots en une demande complète ; vous pouvez revenir à la vôtre.
6. **Démarrer.** Ajoutez d'autres projets de la même façon et faites-les tourner en même temps.
7. **GitHub** (facultatif) : *Réglages → Options → GitHub → Se connecter à GitHub*, une fois ; ensuite *Gérer → GitHub* lie chaque projet à un dépôt (voir [GitHub](#lapplication) plus bas, et le *Guide* de l'application).

**Linux** (tout tourne nativement, sans WSL)

```bash
chmod +x Athanor-Linux-x64    # une fois : un fichier téléchargé n'est pas encore exécutable
./Athanor-Linux-x64           # l'application, dans une fenêtre d'application de Chrome/Edge/Chromium, sinon votre navigateur
./Athanor-Linux-x64 tui       # la même application dans le terminal (en SSH ou sans écran : d'elle-même)
```

Puis *Installation & santé* → *Tout installer* (Claude Code, la barrière et les outils ; Linux demande votre mot de passe une fois), le jeton dans *Réglages*, un projet et *Démarrer*, comme sous Windows (étapes 3, 5 et 6).

#### macOS, pas à pas

1. Téléchargez [**Athanor-macOS.zip**](https://github.com/Aprilox/Athanor/releases/latest/download/Athanor-macOS.zip). Safari l'ouvre tout seul : **Athanor.app** est dans *Téléchargements* (avec un autre navigateur, double-cliquez une fois sur le zip). Une application est un dossier, c'est pourquoi elle voyage zippée. Athanor.app marche depuis *Téléchargements* ; dans *Applications*, il est chez lui (macOS l'y rouvre ensuite après une mise à jour et à l'ouverture de session).
2. Ouvrez Athanor.app (double-clic). La première fois, macOS dit qu'il ne peut pas vérifier l'application, parce qu'elle n'est pas signée par Apple : cliquez sur *Terminé*, ouvrez *Réglages Système → Confidentialité et sécurité*, descendez jusqu'à *« Athanor » a été bloqué* et cliquez sur **Ouvrir quand même**, puis votre mot de passe. Une seule fois.
3. *Créer* : le dossier d'Athanor est créé, puis sa fenêtre s'ouvre.
4. *Installation & santé* → *Tout installer*, *Tout vérifier*, puis la ligne *Jeton* → *Réparer*, et collez le jeton dans *Réglages*.
5. Ajoutez un projet et *Démarrer*.

**Ce que *Tout installer* met sur le Mac**, uniquement par les installeurs officiels : [Homebrew](https://brew.sh) s'il manque (son propre installeur demande votre mot de passe et Entrée) ; sept outils en ligne de commande via Homebrew : `git`, `jq`, `ripgrep`, `node`, `imagemagick`, `ffmpeg`, `gh` ; Claude Code (l'installeur d'Anthropic, dans `~/.local/bin`, avec une ligne dans `~/.zshrc`) ; RTK, qui raccourcit la sortie des commandes pour économiser des tokens, dans le dossier d'Athanor. Rien qui ait une fenêtre, rien qui démarre tout seul. La barrière est celle de macOS (Seatbelt) : seulement vérifiée. Tout est écrit dans `logs/install-mac.log`, dans le dossier d'Athanor.

**Sa fenêtre est la sienne**, avec le moteur web du Mac (WebKit) dans Athanor : aucun navigateur ne s'ouvre. **Pendant qu'il tourne**, Athanor a son icône dans le Dock et une flamme près de l'horloge (la barre des menus) ; les deux montrent les pilotes au travail et rouvrent la fenêtre. Le bouton rouge cache la fenêtre : Athanor continue. **Quitter** (⌘Q, ou le menu de l'icône du Dock) avec des pilotes en cours propose de garder Athanor dans la barre des menus (l'icône du Dock part, les pilotes continuent, la flamme reste) ou de les arrêter et quitter ; *Quitter* depuis la flamme quitte, les pilotes continuant ou non, au choix. Le Mac reste éveillé tant qu'un pilote tourne (`caffeinate` ; un MacBook refermé s'endort quand même, sauf branché sur secteur avec un écran externe).

Claude travaille dans Seatbelt, la barrière intégrée à macOS (celle qu'utilise Claude Code) : il n'écrit que dans le projet. Les serveurs MCP ont leur propre cage Seatbelt. Unreal, Blender, Roblox Studio, Xcode et le SDK Android sont trouvés à leurs places habituelles et Claude les lance directement ; les programmes Windows et Docker via `tool` restent réservés à Windows et Linux. Sur le Mac, les programmes que Claude ouvre s'affichent sur votre écran (pas de bureau à part comme sous Windows).

### 3. Mettre à jour

**Depuis l'application** : quand une nouvelle version sort, un bouton *Mettre à jour (X.Y.Z)* apparaît en haut de la fenêtre (Athanor regarde à son ouverture ; *Réglages → Options → Version* regarde tout de suite, et peut ne plus regarder à l'ouverture). *Mettre à jour maintenant* télécharge le fichier de votre système, le vérifie avec les empreintes de la version, puis Athanor se ferme, met la nouvelle version en place et se rouvre. Les pilotes en cours continuent.

**À la main** : fermez Athanor, téléchargez le nouveau fichier et ouvrez-le : il met à jour le dossier d'Athanor de la même façon. Sur le Mac, Athanor.app se télécharge une seule fois : ses mises à jour viennent ensuite de l'application (un Athanor.app plus ancien ouvre l'Athanor plus récent gardé dans son dossier).

Votre jeton, vos réglages, vos projets, `logs/` et `tools/` ne sont jamais touchés par une mise à jour. Une installation faite avant la 2.26 (un dossier avec `Athanor.exe`, `template/` et `athanor-config.json` côte à côte) reste où elle est : mettez-y le nouvel exécutable, ou utilisez le bouton. Une mise à jour fait aussi le ménage dans un tel dossier : les fichiers des anciennes versions qu'aucune version n'utilise plus (`commands/`, les scripts bash et Python de `system/`, `docs/`, le README…) partent s'ils sont exactement tels que livrés ; un fichier que vous avez modifié est déplacé dans `logs/ancien-kit/<date>/`, et `logs/nettoyage-<date>.txt` liste ce qui est parti. Rien d'autre du dossier n'est touché.

<details>
<summary><b>Le compiler soi-même</b> (depuis le code source)</summary>

Avec [Go](https://go.dev/dl/) 1.24 ou plus (pas de Go ? `winget install GoLang.Go`, `sudo apt install golang-go` ou `brew install go`) :

```bash
git clone https://github.com/Aprilox/Athanor.git
cd Athanor/app
go run . construire
```

Pareil sous Windows (PowerShell), Linux et macOS : cela construit `Athanor.exe` et `athanor` dans le dossier `Athanor`, chacun portant tout ce que porte une version publiée. Lancée là, une copie du code source est son propre dossier d'outil (son `template/` est utilisé tel quel) ; mettez-la à jour avec `git pull` puis à nouveau `go run . construire`. `go run . construire --all` compile tous les systèmes dans `dist/`, les fichiers que publie une version, et `dist/Athanor.app` (les deux exécutables Mac en un). L'application n'utilise que la bibliothèque standard de Go et quatre paquets embarqués (la fenêtre WebView2 sous Windows, l'icône du Dock sur le Mac) : une fois les sources sur votre disque, la compilation ne demande aucun accès réseau.
</details>

---

## Comment ça marche

<p align="center">
  <img src="docs/images/how-it-works.svg" alt="Fonctionnement d'Athanor : vous décrivez le projet, la boucle du pilote lance une session Claude Code par cycle, le chef délègue à des sous-agents ou à des ouvriers en parallèle, chaque cycle se termine par fusion, vérification, coche et commit" width="100%">
</p>

**Un cycle.** Le pilote (`athanor demarrer`) synchronise la demande, archive les vieux jalons, refait la carte du projet (`.athanor/MAP.md`), ouvre l'éditeur Unreal si le projet en a un, puis lance une session Claude Code avec [`template/CLAUDE.md`](template/CLAUDE.md) comme consignes, dans votre langue. Claude prend le premier jalon non coché de `.athanor/PLAN.md`, le réalise, lance `check.sh`, le coche, le note dans `LOG.md` et commite. Le pilote ferme le cycle peu après le commit et lance le suivant.

**Ce que chaque agent sait dès le départ.** `.athanor/PC_TOOLS.md` liste les outils déjà installés et leurs versions (Node, Python, PHP, Composer, Git…), vérifiés par le pilote : personne ne dépense de tours à les sonder. Deux fichiers courts rassemblent les problèmes déjà résolus, une ligne chacun : `.athanor/SOLUTIONS.md` pour le projet et `tools/SOLUTIONS.md` pour le PC, partagé par tous les projets. Chaque agent les lit en premier et ajoute une ligne quand il résout quelque chose ; le pilote retire les doublons, garde les 60 dernières lignes et récupère ce que les ouvriers en parallèle ont appris.

**Pas de fil perdu quand un contexte est plein.** Chaque session tient `.athanor/HANDOFF.md` à jour (où elle en est, 10 lignes). Au-delà de `RELAY_CONTEXT_K` milliers de tokens de contexte (110 par défaut), le pilote l'arrête entre deux étapes et lance une nouvelle session sur le même jalon, avec ce fichier, ses dernières actions et ses derniers messages : des tours moins chers, et pas de compaction automatique qui brouillerait le fil. Les ouvriers sont relayés de la même façon, sur leur propre branche.

**Le travail fait dans un programme est enregistré.** Un éditeur ou un moteur piloté par un serveur MCP garde son travail en mémoire : avant de cocher un jalon, l'agent l'enregistre dans le projet. Si un jalon a utilisé un serveur MCP et que son commit n'a changé aucun fichier, le pilote le fait vérifier en premier au cycle suivant. Les serveurs Roblox Studio et Unreal du catalogue disent comment enregistrer chez eux.

**La vie d'un projet.**

| Phase | Ce qui se passe |
|---|---|
| Cadrage | un cycle limité dans le temps (`FRAMING_MAX_MIN`, 25 min par défaut) qui n'écrit que le plan : jusqu'à 3 `scout` lisent en même temps les pièces jointes et le code existant, le chef écrit `.athanor/docs/REQUIREMENTS.md`, `DESIGN.md`, `ARCHITECTURE.md` et un plan de 8 à 25 jalons, chaque ligne étant une fiche autonome (fichiers, dépendances, critères d'acceptation, ce qu'il faut lire), et le `critic` relit le plan une fois. Ni code, ni installation, ni contenu de démo ; une fois le temps écoulé, le plan tel qu'il est part en développement |
| Squelette | un squelette exécutable et le `check.sh` propre au projet (compilation, tests, captures) |
| Tranche complète | une fonctionnalité qui marche de bout en bout, vérifiée par le `tester` |
| Production | les jalons restants, un par cycle, ou plusieurs à la fois en mode parallèle |
| Finition | le `critic` utilise le résultat comme un utilisateur exigeant et note les bugs dans `BUGS.md` |
| Livraison | `deliverable/`, `TEST_GUIDE.md`, puis `FINAL_REPORT.md` avec `STATUS: DONE`. Le pilote prend la demande suivante de la file, ou s'arrête |

---

## Mode parallèle

*Gérer → Mode de travail → Mode parallèle*, par projet, avec 2 à 10 ouvriers au maximum. Les ouvriers tournent sur `WORKER_MODEL` (Sonnet par défaut) : une fiche de jalon bien écrite est du travail d'exécution, le chef garde son modèle pour le plan et les fusions.

1. Le chef écrit un **plan fin avec des dépendances explicites** : beaucoup de petits jalons autonomes, chacun avec les fichiers qu'il touche, `Depends on:` les jalons dont il a besoin, et ses critères d'acceptation. M1 est toujours la fondation (squelette, `check.sh`, contrats partagés).
2. Le pilote fait tourner un **ordonnanceur** : chaque jalon dont les dépendances sont fusionnées reçoit un **ouvrier** (sa propre session Claude, sa propre copie de travail git `.athanor/work/M5` et sa branche `auto/M5`), en commençant par 2 et en en ajoutant un chaque fois qu'un ouvrier livre, jusqu'au nombre choisi. Dès qu'un ouvrier atteint le quota, plus aucun ne démarre ; s'ils l'ont tous atteint, la vérification du chef est sautée et le pilote attend, puis fait un jalon avec le chef seul avant de relancer des ouvriers. Le pilote laisse aussi le chef travailler seul, le temps d'un cycle, quand des ouvriers ne seraient pas rentables : moins de 3 jalons prêts en même temps, uniquement des jalons de documentation (ils se citent entre eux et entrent en conflit), ou le quota des 5 heures à 75 % ou celui de la semaine à 85 %. Deux jalons dont les fiches citent les mêmes fichiers ne tournent jamais en même temps. Les ouvriers partagent un tableau, `.athanor/BOARD.md`, écrit par le pilote toutes les quelques secondes : ce que fait chacun et les fichiers qu'il tient. Un ouvrier qui a besoin d'un fichier hors de sa fiche le réserve d'abord (`.athanor/RESERVE`) ; le pilote l'accorde sauf si un autre ouvrier le tient, et aucun nouvel ouvrier ne démarre sur un fichier réservé.
3. Dès qu'un ouvrier termine, le pilote **fusionne sa branche immédiatement** quand elle s'applique sans conflit, coche le jalon et lance les jalons suivants devenus prêts. Personne n'attend l'ouvrier le plus lent.
4. En fin de cycle, **une seule** session du chef vérifie tout : elle fusionne les branches en conflit, lance la vérification complète, corrige ce que les fusions ont cassé et note le reste dans `BUGS.md`.
5. **Les demandes en attente n'attendent pas.** Au début de chaque cycle, elles rejoignent la demande en cours : le chef ajoute leurs jalons au plan, avec leurs dépendances.

Chaque ouvrier ne porte que son jalon en contexte : le travail est découpé en petites sessions économes au lieu d'une seule énorme. Un ouvrier peut confier ses longues séries de tests et ses recherches à `executor` (modèle bon marché) et ne lire que les échecs ; il n'appelle aucun autre sous-agent. La page *Équipe* montre une carte par ouvrier avec les sous-agents qu'il a appelés, et le *Journal* étiquette leurs lignes `[M5]`. Le mode parallèle consomme plus de quota par heure (une session par ouvrier) mais beaucoup moins de temps.

---

## Mode éco

*Gérer → Mode de travail → Mode éco*, par projet (dans un terminal : **M**, puis *Mode de travail*). Pour quand le quota compte plus que la vitesse. Il ne tourne jamais avec le mode parallèle : activer l'un coupe l'autre.

Mesuré sur un vrai projet de jeu, **ce sont les sous-agents qui consomment le plus** (80 à 87 % des jetons de ses cycles) : chacun repart de zéro et repaie tout son contexte à chaque tour. Le mode éco coupe donc d'abord là :

- **Le chef fait le travail lui-même.** Pas de sous-agent pour un jalon ordinaire ni pour un gros morceau (il découpe le jalon à la place) ; les sous-agents du projet seulement quand la demande les exige pour ce jalon ; aucun nouveau sous-agent proposé.
- **Une seule revue, à la fin.** `critic` et `tester` passent une fois chacun, pour la livraison : ni après la tranche complète, ni au polish, jamais de contre-revue. Le cadrage se passe des éclaireurs (un au plus pour de grosses pièces jointes) et de la relecture du plan.
- **Peu d'images.** 4 lectures d'image au plus par cycle (captures MCP comprises), en planches réduites ; une nouvelle capture seulement après un changement visible.
- **Moins de lecture, des sorties courtes.** La fin du journal, le jalon en cours, les seules sections utiles ; les sorties de commandes coupées à 8 000 caractères (`BASH_MAX_OUTPUT_LENGTH`, 20 000 sinon).

Le **modèle et l'effort de réflexion ne sont pas touchés** : ils restent tels que vous les avez réglés. Pour économiser encore plus, choisissez un modèle plus petit pour le chef (Sonnet) et un effort plus bas (`medium` ou `low`) dans *Réglages → Modèles*. Le Suivi affiche *mode éco* pendant un cycle ; l'interrupteur s'applique dès le prochain cycle.

---

## Économiser le quota

Trois mécanismes agissent sur chaque projet, et la *Consommation* du Suivi montre ce que chacun apporte de façon mesurée.

- **Ponytail** (`PONYTAIL=1`, activé par défaut ; *Réglages → Code le plus simple*). Athanor embarque les règles de [Ponytail](https://github.com/dietrichgebert/ponytail) 5.3.2 (MIT) et les donne à **toutes** les sessions Claude du projet : le chef, les ouvriers en parallèle, chaque sous-agent (ceux d'Athanor et les vôtres) et la discussion de l'appli. Ceux qui écrivent du code reçoivent ses règles : avant d'écrire, réutiliser ce que le projet, la bibliothèque standard ou une dépendance installée offre déjà ; puis la plus petite modification qui fonctionne entièrement, avec un petit test pour toute logique non triviale. Ceux qui planifient, explorent ou jugent (le cadrage, `architect`, `scout`, `critic`, les agents en lecture seule, une question dans la discussion) reçoivent sa façon de travailler sobre : ne lire que ce que la question demande, répondre court et, pour `critic`, signaler le code qui devrait être plus petit. Une phrase d'Athanor fixe sa portée : Ponytail décide *comment* un jalon est construit, jamais *combien* de la demande est livré. Les hooks de son plugin ne sont pas utilisés (ils écrivent dans `~/.claude` et installent une ligne de statut). *Installation & santé* indique s'il est activé.
- **RTK** compresse les sorties de `git`, `ls`, `grep`, des tests… avant que Claude les lise. Ses mesures sont maintenant gardées par projet (`.athanor/rtk/`, hors de git), et le pilote les relève après chaque cycle.
- **Le cache** : ce que Claude relit pendant une session coûte un dixième au tarif public.

Dans *Consommation* : la part de lecture économisée par le cache, les jetons épargnés par RTK et, dès 3 cycles réussis de chaque sorte, le coût moyen d'un cycle **avec et sans** Ponytail (et le mode éco) sur ce projet. Cette dernière ligne est une tendance, pas un test : les jalons diffèrent d'un cycle à l'autre, et personne ne peut mesurer le code que Ponytail a évité d'écrire. Dans un terminal, le cadre *Avancement* dit la même chose en une ligne.

---

## Plusieurs ordinateurs

Un projet, plusieurs ordinateurs qui y travaillent en même temps : votre PC fixe et sa grosse carte graphique, un portable sur la fibre, une vieille tour. Ils se retrouvent par le dépôt GitHub du projet, rien d'autre à installer, sous Windows, macOS comme Linux.

**Ce qu'il faut.** Un dépôt GitHub, et pour chaque personne un compte qui peut y envoyer : celui du propriétaire, ou celui d'un contributeur (le propriétaire ajoute chaque personne dans *Settings → Collaborators* du dépôt sur GitHub, avec l'accès en écriture). **Pas besoin d'organisation GitHub.** Chaque ordinateur est connecté une fois à GitHub, avec ce compte : *Réglages → Options → GitHub → Se connecter à GitHub*.

**Pour commencer.** Sur le premier ordinateur, liez le projet au dépôt (*Gérer → GitHub*), puis activez *Suivi → Autres ordinateurs → Travailler avec d'autres ordinateurs* (cela partage l'atelier, `.athanor/`, et l'envoie aussitôt, pour que les autres reçoivent la demande et le plan ; l'interrupteur demande d'abord s'il est désactivé). Sur chaque autre ordinateur : *Nouveau projet → Rejoindre un projet*, avec le même dépôt, la même branche et un nouveau dossier vide : le projet arrive aussitôt (sa demande, son plan, son journal de bord), déjà en travail avec les autres ; il part de leur travail, jamais d'un historique à lui. S'il n'a pas encore de demande, *Démarrer* le lance quand même : son pilote ramène le travail des autres chaque minute, et la demande avec. L'encadré *Autres ordinateurs* du Suivi rappelle ces étapes, et *Plusieurs ordinateurs* dans le Guide les déroule avec un bouton pour chacune. Chaque ordinateur a sa propre clé sur son compte (« Athanor (ordinateur) »), donc chacun peut être déconnecté seul.

**Qui construit quoi.** Avant chaque cycle, le pilote d'un ordinateur ramène le travail des autres dans le projet, puis prend un jalon : un jalon dont les dépendances sont faites et qu'aucun autre ordinateur ne construit. Le cadrage est écrit par un seul ordinateur, les autres attendent le plan ; le rapport final aussi, à la fin, par un seul. Deux ordinateurs qui choisissent le même jalon au même moment sont départagés par celui qui l'a pris le premier ; l'autre en prend un autre. S'il n'y a rien à prendre (tout ce qui reste attend un jalon en cours), un ordinateur attend et regarde de nouveau chaque minute, et son Suivi dit pourquoi.

**Ce que chaque ordinateur sait bien faire.** *À distance → Cet ordinateur, pour les autres ordinateurs* : son nom, ce qu'il a (`RTX 4090, 24 Go`) et quelques mots (`gpu`, `network`). Claude connaît l'équipe quand il écrit le plan : un jalon qui a besoin de ce que seuls certains ordinateurs ont reçoit `Needs: gpu.` dans sa ligne, et seul un ordinateur qui a ce mot le construit, pendant que les autres prennent le reste. Deux ordinateurs avec le même mot construisent ces jalons en même temps : Claude découpe donc un gros travail d'un même genre (les armures de plusieurs types, plusieurs niveaux) en jalons séparés. Les mots sont facultatifs : si aucun ordinateur en marche n'a le mot dont un jalon a besoin (personne n'a rien déclaré), Athanor le répartit comme les autres, dans l'ordre du plan.

**Ce que vous voyez.** *Suivi → Ordinateurs sur ce projet* : chaque ordinateur de l'équipe, en ligne ou vu à telle heure, ce qu'il construit en ce moment et depuis quand, son dernier envoi. Un ordinateur est en ligne tant que son Athanor tourne (il signale sa présence chaque minute), même pilote arrêté ; les horloges des ordinateurs doivent être à l'heure (réglées automatiquement, comme par défaut).

**Un niveau pour chaque mot.** `gpu:3` contre `gpu:1` : un mot peut porter un niveau de 1 à 9. Un jalon qui en demande beaucoup (`Needs: gpu:3.`) va à un ordinateur de ce niveau ou plus, pendant que les moins puissants prennent les jalons plus légers en même temps ; si aucun ordinateur n'a ce niveau, le plus puissant le prend.

**Des notes entre les ordinateurs.** Les Claude des ordinateurs se laissent des notes dans `.athanor/TEAM_NOTES.md` (une interface qui a changé, un fichier partagé à manier avec soin, un piège) : chaque Claude lit les dernières avant de commencer, et le Suivi les montre.

**Un travail jamais perdu.** Toutes les 10 minutes, et quand son pilote s'arrête ou passe la main sur un jalon, un ordinateur envoie son travail inachevé sur GitHub, sur une ref à lui (`refs/athanor/encours/<ordinateur>`, un patch par jalon). Ce n'est pas une branche : GitHub ne la montre pas dans la liste des branches et ne lance aucun workflow du dépôt quand elle change ou disparaît ; elle part une fois ses jalons finis. L'ordinateur qui prend ensuite ce jalon repart de là ; s'il ne peut pas fusionner ce travail, il le transmet dans le sien, et un troisième ordinateur reçoit quand même les deux. Un ordinateur qui revient après qu'un autre a repris son jalon garde sa propre copie de côté dans son `.git` (`refs/athanor/sauve/…`) et repart du travail de l'équipe ; ses fichiers ignorés par git que la version de l'équipe remplace sont d'abord copiés dans les données d'Athanor pour le projet (gardés 30 jours, les 5 plus récents toujours).

**Les fusions.** Après chaque jalon, le pilote fusionne d'abord les nouveaux commits des autres, puis envoie (jamais en force). Les notes de l'atelier (`LOG.md`, `BUGS.md`, `SOLUTIONS.md`…) sont jointes ligne à ligne, et deux jalons cochés sur des lignes voisines du plan sont fusionnés tout seuls. Quand deux ordinateurs ont vraiment changé les mêmes lignes, le pilote annule la fusion (rien n'est perdu) et Claude la fait au début de son cycle suivant, en gardant le travail des deux côtés (`.athanor/MERGE_NEEDED.md`).

**Quand un programme ne marche pas sur un ordinateur** (Blender plante, un SDK est cassé) : Claude cherche d'abord la cause et répare ce qu'il peut sur cet ordinateur, même si cela prend 5 ou 10 minutes ; c'est seulement quand la cause est hors de sa portée (le matériel, un pilote, une licence) ou que sa réparation n'a pas marché qu'il le signale, sans relancer en boucle le même test qui échoue. Chaque ordinateur de l'équipe reçoit une notification, et les autres essaient le jalon à sa place, d'abord ceux qui ont le mot de ce programme. Celui qui réussit garde le mot : ces jalons lui reviennent désormais ; le Suivi montre qui construit un jalon à la place de qui, et ce qui ne marche pas où. Si aucun n'y arrive (ou si aucun ne tourne), l'ordinateur qui devait le construire le reprend et Claude vous le demande dans *Pour vous*, en disant quels ordinateurs ont essayé. Un ordinateur réessaie un programme 12 heures plus tard, au cas où il aurait été réparé. En travaillant seul, Claude vous le demande à ce moment-là.

**Bon à savoir.** Le dépôt a besoin de *Partager l'atelier (.athanor/) sur GitHub* : activer les ordinateurs l'active, avec l'envoi après chaque jalon. Chaque ordinateur fait un jalon à la fois : le mode parallèle est donc coupé pendant que plusieurs ordinateurs travaillent sur un projet. Chaque ordinateur laisse une petite ref `refs/athanor/pc/<id>` sur le dépôt, pas une branche (jamais dans la liste des branches, aucun workflow ne tourne pour elle) : sa présence (son nom, ce qu'il sait bien faire, ce qu'il construit) ; elle disparaît quand il cesse de travailler avec les autres, et après 30 jours sans nouvelles. Les branches `athanor/pc/…` et `athanor/encours/…` de la 2.36 sont retirées une fois, chacune par son ordinateur. Un projet dont l'historique n'a rien de commun avec celui du dépôt est refusé : deux projets qui partagent seulement un nom ne sont jamais mélangés. Les fichiers du dossier que le dépôt a aussi ne sont jamais écrasés quand un ordinateur rejoint l'équipe : il dit lesquels déplacer.

Pourquoi GitHub et pas un tunnel entre les ordinateurs : chaque ordinateur y envoie déjà, il est joignable de partout sans rien ouvrir, il continue de fonctionner quand un ordinateur est éteint, et son historique fait la fusion. Un tunnel demanderait que tous les ordinateurs soient allumés en même temps, et l'un d'eux devrait servir les autres.

---

## L'équipe

| Agent | Rôle | Modèle |
|---|---|---|
| **Chef de projet** | lit le plan, code les jalons ordinaires, délègue, relit chaque rapport, commite | `MODEL` |
| `scout` | lit ce qu'on lui désigne (code, pièces jointes, documentation) et répond à une question en un court résumé, ou écrit un document ; pour le cadrage | `SUBAGENT_MODEL`, Sonnet s'il est vide |
| `architect` | exigences, architecture et plan des jalons pour une grosse révision du plan | `SUBAGENT_MODEL` |
| `creator` | contenus : histoire, monde, dialogues, textes d'interface, ressources 2D/3D/son générées par programme | `SUBAGENT_MODEL` |
| `developer` | une tâche de développement précise, avec ses tests | `SUBAGENT_MODEL` |
| `tester` | tests automatiques, captures, rapports de bugs (ne corrige jamais le code lui-même) | `SUBAGENT_MODEL` |
| `critic` | utilise le résultat comme un utilisateur final exigeant, sans complaisance (lecture seule) | `SUBAGENT_MODEL` |
| `executor` | travail mécanique sans décision de conception : sorties de commandes longues, recherches, modifications en série, copies de fichiers ; le seul sous-agent que les ouvriers en parallèle peuvent appeler | `CHEAP_MODEL` (Haiku) |

Avec `SUBAGENT_MODEL=auto` (*Réglages*, « choisi par le chef »), le chef choisit lui-même le modèle de chaque appel : Haiku pour le travail mécanique, Sonnet pour l'essentiel du travail, Opus seulement pour le jugement (architecture, bug incompris, critique finale). Un appel qui n'en précise pas garde le modèle du tableau.

Les agents sont dans [`template/.claude/agents/`](template/.claude/agents) : modifiez-les pour changer la façon de travailler de votre équipe.

**Vos propres sous-agents.** Sur la page *Équipe*, **+ Nouveau sous-agent** ajoute un spécialiste à vous (un traducteur, un chercheur dans un domaine, un level designer…) : un nom, quand le chef doit l'appeler, ses consignes (**✨ Améliorer avec Claude** transforme quelques lignes en consignes structurées), un modèle (comme les autres, économique ou le plus puissant), lecture seule ou non, ce projet ou tous vos projets. Ou décrivez-le à Claude dans **Discuter** (« un spécialiste Blender pour les objets ») : il pose ses questions, puis montre le sous-agent sous forme de fiche encore modifiable (✨ *Améliorer avec Claude* là aussi), et **Ajouter à l'équipe** le crée (de même pour modifier l'un des vôtres). Le chef peut aussi en proposer un quand un besoin revient : il écrit `.athanor/agents/<nom>.md`, le pilote le vérifie et l'ajoute à l'équipe au cycle suivant, marqué *proposé par le chef* ; vous le gardez, le modifiez ou le retirez (une proposition retirée n'est jamais refaite). Ces sous-agents fonctionnent comme les autres : même barrière, skills, outils MCP et fichiers joints, appelables par le chef et par les ouvriers en parallèle ; le chef les trouve dans `.athanor/TEAM.md`, et une demande peut en nommer un (« fais faire les textes par `traducteur` »). Ils sont rangés dans `extensions/agents.json` (12 par projet au maximum).

---

## L'application

| | |
|:---:|:---:|
| <img src="docs/images/team.png" alt="Page Équipe : le chef de projet avec les sous-agents qu'il a appelés, et trois ouvriers, chacun avec son executor"><br>**Équipe** : qui a appelé qui, ce que fait chacun, en direct | <img src="docs/images/log.png" alt="Page Journal en thème clair : événements du pilote, paroles de Claude, outils et sous-agents, lignes des ouvriers étiquetées [M5]"><br>**Journal** : lisible ou brut, filtrable, ouvriers étiquetés |
| <img src="docs/images/requests.png" alt="Page Demandes : la demande en cours avec son plan en vagues, et deux demandes en attente réordonnables"><br>**Demandes** : une file réordonnable, des fichiers, des remarques en cours de route | <img src="docs/images/chat.png" alt="Page Discuter pendant que le pilote tourne : Claude répond à une question sur le code et transforme la correction en remarque envoyée au pilote"><br>**Discuter** : des questions sur le projet à tout moment |
| <img src="docs/images/skills.png" alt="Page Skills &amp; MCP, onglet Skills : quatre skills installés, dont trois activés pour ce projet, et la recherche communautaire en dessous"><br>**Skills** : installés une fois, activés par projet | <img src="docs/images/mcp.png" alt="Page Skills &amp; MCP, onglet MCP : Context7 et Godot activés, un champ de clé API facultative, et la recherche dans le MCP Registry"><br>**MCP** : distants ou locaux dans une cage, testés en un clic |
| <img src="docs/images/health.png" alt="Page Installation &amp; santé sous Windows, onglet WSL : toutes les vérifications au vert sauf Docker, facultatif"><br>**Installation & santé** : un onglet par mode, réparation ligne par ligne | <img src="docs/images/options.png" alt="Réglages, onglet Options : langue, cinq palettes et cinq accents, démarrage, bureau Claude, dossier de l'outil et version"><br>**Réglages → Options** : 6 langues, 5 palettes × 5 accents |

Les captures sont en anglais ; l'interface s'affiche dans la langue choisie.

| Page | Ce qu'on y fait |
|---|---|
| **Suivi** | état en une phrase, jalon en cours, le dernier commit de cet ordinateur et le dernier sur GitHub (et pourquoi un envoi n'est pas parti), plan coché, le quota de l'abonnement (5 heures, la semaine) avec un arrêt automatique au seuil choisi, l'avancement et sa projection (quand le dernier jalon devrait tomber, en temps et en coût), la consommation par cycle (durée, coût catalogue, cache), les ordinateurs qui travaillent sur le projet (ce que chacun construit) ; *Démarrer*, *Arrêter après le cycle*, *Arrêter maintenant*, *Mode de travail* |
| **Équipe** | un arbre : le chef et chaque ouvrier avec les sous-agents qu'il a appelés ; un clic sur un sous-agent montre la consigne reçue et le rapport rendu ; le modèle de chacun ; qui peut être appelé, et vos propres sous-agents (créer, modifier, retirer) |
| **Pour vous** | ce dont Claude a besoin de vous : chaque tâche avec ses étapes et ses liens, un champ protégé pour chaque clé demandée, des fichiers ; *C'est fait* ou *Passer : trouve une autre solution* ; le verdict de Claude (vérifiée, ou ce qui manque encore) et l'historique. Une pastille dans le menu, un bandeau sur *Suivi* et une notification système annoncent une nouvelle tâche |
| **Demandes** | la demande en cours et son plan, les demandes à la suite (horodatées, réordonnables), les remarques en cours de route ; ✨ *Améliorer avec Claude* reformule une demande plus claire et plus complète, et vous pouvez revenir à la vôtre ; 📎 joignez des fichiers à une demande (cahier des charges, maquettes, photos, PDF, données) : ils restent dans l'atelier et Claude les lit avec la demande |
| **Discuter** | parler avec Claude du projet : des questions à tout moment (il lit le code, le plan, le journal) ; pilote arrêté, il peut aussi modifier des fichiers (sans commandes) et Athanor les commite ; pilote en marche, une modification devient une remarque pour le pilote, envoyée d'un clic ; modèle au choix |
| **Journal** | le journal lisible (Claude parle, outils, résultats, sous-agents, pilote) ou brut (JSON), filtrable |
| **À distance** | l'accès à distance activé ou coupé, le lien et le QR code qui appairent un téléphone ou un autre ordinateur, les appareils de confiance (connecté maintenant ou vu à telle heure, d'où), les renommer ou les retirer d'un ✕ ; le nom de cet ordinateur et ce qu'il sait bien faire, pour les autres ordinateurs d'un projet |
| **Guide** | des tutoriels pas à pas, toujours dans le menu, avec pour chaque étape un bouton qui ouvre la bonne page : démarrer un projet, GitHub (connecter l'ordinateur, lier, travailler chacun son tour), votre téléphone (et votre propre domaine, et comment ne plus l'utiliser), plusieurs ordinateurs (ce qu'il faut, connecter chaque ordinateur, rejoindre, ce que chacun sait bien faire) ; les premiers pas et les pages y renvoient |
| **Nouveau projet / Rejoindre un projet** (dans le sélecteur de projet) | un nom, le dossier (*Parcourir…* ouvre la fenêtre de choix de dossier du système) et ce que vous voulez ; ou rejoindre un projet que d'autres ordinateurs construisent déjà, depuis son dépôt |
| **Gérer** (dans le sélecteur de projet) | renommer le projet ; déplacer ou renommer son dossier ; le faire pointer vers un autre dossier (le plan et les demandes suivent, les jalons sont vérifiés d'après son contenu) ; vérifier les jalons ; l'archiver ; le supprimer (de la liste, son atelier ou tout le dossier, à la corbeille) ; le lier à GitHub |
| **Skills & MCP** | la bibliothèque (catalogue vérifié, recherche dans SkillsMP et le registre MCP, lien GitHub, vos propres skills, configurations MCP collées), un interrupteur par projet, les clés d'API des serveurs (rangées à part, jamais réaffichées) et leurs réglages (une adresse, un port, un chemin : détectés dans la configuration, affichés en clair avec leur valeur par défaut), *Voir / modifier* la configuration d'un serveur, ses réglages et le programme à ouvrir en JSON (y compris un serveur collé), *Tester* démarre un serveur dans sa cage et liste ses outils ; *Se connecter* une fois à un serveur distant qui le demande (OAuth), pour tous les projets ; **Plugins** : tout le catalogue des plugins Claude Code (catalogue officiel plus les vôtres), avec recherche, installés en un clic et activés projet par projet |
| **Installation & santé** | Un onglet par mode (Windows natif et WSL sous Windows ; Linux ; macOS), l'actuel marqué : *Tout installer* pour lui, ou y basculer. *Tout vérifier*, *Tester la barrière* pour de vrai, *Réparer* ligne par ligne, *Demander à Claude* de réparer ou d'installer quelque chose |
| **Réglages → Claude** | les réglages du pilote, le `.env` de l'outil (jeton masqué, modèles, effort, surveillance…, les plus rares sous *Avancé*), *Enregistrer et redémarrer après le cycle* |
| **Réglages → Options** | langue, palette et accent, la connexion de cet ordinateur à GitHub, liste des projets (retirer de la liste ne touche jamais au dossier), relance des pilotes à l'ouverture, démarrage avec l'ordinateur, bureau « Claude », mot de passe, dossier de l'outil, version et mises à jour |

**Skills & MCP, en sécurité** : les skills sont chargés comme un plugin de session (`--plugin-dir`) : Claude ne lit un skill que quand il en a besoin, et peut les lire mais jamais les modifier. Les serveurs MCP s'ajoutent à la configuration MCP de la session ; un serveur local démarre dans une cage bubblewrap (`athanor mcp-cage`) : système en lecture seule, seuls le projet et les caches de l'outil en écriture, ni dossier personnel, ni disques Windows, ni dossier d'Athanor, ni jeton Claude. Un *programme Windows* (le serveur de Roblox Studio, un `.exe` ou un `.bat`, reconnu au collage et confirmé par l'option *Programme Windows*) ne peut pas être mis en cage depuis WSL : il démarre sous Windows via l'interopérabilité de WSL (`athanor mcp-windows`), avec vos droits, et ne reçoit que ses propres variables ; Claude ne voit que ses outils, et tout son arbre de processus s'arrête avec la session. Les skills demandent une version de Claude Code qui a `--plugin-dir`.

**Les plugins, démontés** : un plugin est téléchargé une fois dans le dossier de l'outil (depuis GitHub, figé sur le commit que donne son catalogue) et activé projet par projet. Ses skills, ses commandes et ses sous-agents sont chargés avec `--plugin-dir`, en lecture seule pour Claude ; ses serveurs MCP rejoignent la bibliothèque MCP (les locaux dans la cage, les clés rangées à part, un serveur distant connecté une fois pour tous les projets) ; ses hooks, ses serveurs LSP et ses moniteurs sont écartés, car ils tourneraient hors de la barrière. Les plugins construits par une commande ou publiés seulement sur npm sont listés mais pas installés.

**Des projets qui suivent leur dossier** : quand un projet pointe vers un autre dossier, Athanor compare les jalons cochés avec ce qui s'y trouve. Dans un dossier vide, plus rien n'est fait ; un jalon dont les fichiers listés manquent est décoché ; quand les fichiers reviennent, il se recoche tout seul. Au démarrage suivant, Claude vérifie chaque jalon plus en profondeur (`.athanor/REVERIFY`) et décoche ce qui manque vraiment. Les projets archivés quittent la liste et ne sont jamais relancés : *Projets archivés*, en bas de la liste des projets, les ramène.

**GitHub, connecté une fois par ordinateur** : *Réglages → Options → GitHub → Se connecter à GitHub* se connecte une seule fois et met une clé « Athanor (cet ordinateur) » sur votre compte GitHub, gardée dans les données d'Athanor (hors de portée de Claude dans un bac à sable). Chaque projet de l'ordinateur envoie avec elle ; lier un projet la demande. Cette clé ouvre tous les dépôts que votre compte peut modifier ; *Se déconnecter* la retire de cet ordinateur (GitHub garde alors une entrée à retirer dans *Settings › SSH and GPG keys*). Un projet lié par une version d'avant la 2.39 avec sa propre clé (une deploy key) continue d'envoyer avec elle, et *Utiliser ma connexion GitHub* le fait passer à la connexion.

**GitHub, pour chaque projet** : à la création d'un projet, ou plus tard dans *Gérer → GitHub*, choisissez *Utiliser un de mes dépôts* (PROPRIÉTAIRE/NOM : lié aussitôt, sans fenêtre) ou *Créer un dépôt privé* (une connexion, seulement pour le créer ; rien n'en est gardé). Athanor vérifie tout de suite que votre compte peut lire le dépôt et y envoyer, et dit pourquoi sinon (introuvable, lecture seule, réseau) ; un dépôt avec des workflows GitHub Actions vous demande d'abord. Ensuite, le pilote envoie dès qu'un jalon est commité, de nouveau à la fin de chaque cycle si quelque chose n'est pas encore sur GitHub (y compris un cycle coupé par le contexte, la limite de temps ou le quota), et à la livraison (désactivez-le pour n'envoyer qu'avec *Envoyer maintenant* ; le journal le dit alors). Les envois ne sont jamais forcés : si GitHub a des commits que le projet n'a pas, Athanor s'arrête et le dit. Avant chaque envoi, les nouveaux commits sont vérifiés à la recherche de secrets (clés, jetons, fichiers `.env`, clés privées) ; si un secret est trouvé, rien n'est envoyé, et Claude le retire des commits non envoyés à son cycle suivant. Une modification des workflows GitHub Actions du dépôt (du code que GitHub exécute, avec les secrets du dépôt) vous attend : vérifiez-la, puis *Envoyer quand même*. Le même encadré règle l'**auteur des commits** (votre nom, et votre email GitHub ou votre adresse GitHub privée *noreply*, proposés depuis votre compte) et si Claude apparaît en co-auteur (par défaut non : aucune ligne « Co-Authored-By: Claude ») ; Claude écrit chaque titre de commit sous la forme `M3: ce que le jalon apporte`. *Partager l'atelier (.athanor/) sur GitHub* met le plan, les demandes, le journal de bord et les documents dans les commits (jamais les secrets, les journaux bruts ni l'état du pilote) : la cocher les commite aussitôt, seuls (jamais le code en cours d'écriture), et les envoie ; *Envoyer maintenant* prend aussi ce qui y a changé depuis ; quelqu'un qui récupère le dépôt et l'ajoute à son propre Athanor reprend le projet ; un seul pilote à la fois sur un projet, sauf si les ordinateurs travaillent ensemble ([Plusieurs ordinateurs](#plusieurs-ordinateurs)).

**À deux, chacun son tour, sur un même dépôt** (sans organisation, sans *Travailler avec d'autres ordinateurs*) : le propriétaire ajoute l'autre comme contributeur (*Settings › Collaborators* du dépôt). Chacun connecte son ordinateur à GitHub (ci-dessus) et lie le projet au dépôt. Cochez *Partager l'atelier* des deux côtés. Avant de travailler, *Gérer → GitHub → Récupérer le travail de GitHub* ramène ce que l'autre a poussé (comme `git pull` : une avance rapide ou une fusion ; jamais par-dessus des modifications non commitées, et un vrai conflit est laissé à Claude à son cycle suivant, sans rien changer). *Démarrer* le vérifie aussi : si GitHub a des commits que le dossier n'a pas, Athanor demande s'il faut les ramener d'abord ou démarrer d'où vous en êtes.

**Le plan se façonne** : un clic sur un jalon montre ses fichiers, ses dépendances et ses critères d'acceptation ; on peut le monter, le descendre ou le retirer (les autres jalons n'en dépendent plus), ou éditer `PLAN.md` en entier.

**Démarrage avec l'ordinateur** : *Ouvrir Athanor au démarrage de l'ordinateur* l'ouvre avec sa fenêtre, sous Windows, dans une session de bureau Linux et sur macOS (à l'ouverture de session). Avec *Relancer les pilotes qui tournaient*, les projets que vous aviez démarrés, ni arrêtés ni livrés, repartent. Relancer Athanor alors qu'il tourne déjà ramène simplement sa fenêtre : il n'y en a jamais deux.

**Accès à distance** : *À distance* → activez-le. Athanor télécharge une fois le programme de tunnel de Cloudflare (`cloudflared`, vérifié avec la somme de contrôle de sa version) et ouvre un « tunnel rapide » : une adresse `https://….trycloudflare.com`, sans compte. Scannez le QR code avec l'appareil photo du téléphone (ou ouvrez le lien sur un autre ordinateur), puis tapez le code à 6 chiffres que la page *À distance* affiche quand vous passez la souris dessus (renouvelé toutes les 20 secondes) : l'appareil devient de confiance et retrouve le même Athanor, mis en page pour son écran, en direct ; sur un téléphone, *Ajouter à l'écran d'accueil* en fait une appli. Tout s'y fait, sauf ce que seul l'ordinateur doit faire : les secrets, l'accès à distance lui-même, le mot de passe, ouvrir des dossiers sur l'ordinateur. La page *À distance* liste les appareils, connectés ou non, et ✕ en retire un aussitôt : son prochain clic est refusé. Tant que l'accès à distance est activé, l'ordinateur ne se met pas en veille (l'écran peut toujours s'éteindre). Le tunnel survit aux redémarrages d'Athanor (une mise à jour), mais son adresse change quand l'ordinateur redémarre : scannez alors le code à nouveau. Cloudflare ne garantit pas la disponibilité des tunnels rapides.

**Les notifications sur le téléphone** : sur un téléphone appairé, *À distance → Cet appareil → Notifications sur cet appareil*. Le téléphone reçoit alors une notification même Athanor fermé, quand Claude a besoin de vous, quand un jalon est fini, quand un projet est livré et quand un ordinateur d'une équipe bloque ; un appui ouvre la bonne page. Sur un iPhone ou un iPad, ajoutez d'abord Athanor à l'écran d'accueil (iOS 16.4 ou plus récent) et activez-les depuis là.

**Votre propre domaine, une adresse fixe** : avec un compte Cloudflare gratuit et un domaine dont le DNS est chez Cloudflare, *À distance → Adresse → Utiliser mon domaine* donne une adresse qui ne change jamais (`athanor.example.com`). Athanor ouvre une fois la connexion à Cloudflare, dans le navigateur, puis crée un tunnel de cet ordinateur et y fait pointer le nom (un enregistrement DNS existant n'est jamais remplacé) ; le certificat du compte et les identifiants du tunnel restent dans le dossier d'Athanor. Les appareils gardent alors leur accès quand l'ordinateur redémarre. Le guide de la page met en place **Cloudflare Access** en quelques clics dans le tableau de bord de Cloudflare : Cloudflare demande alors votre e-mail et un code avant que quoi que ce soit n'atteigne l'ordinateur, si bien qu'un inconnu qui trouve l'adresse ne voit même pas Athanor. *Revenir à une adresse temporaire* supprime le tunnel chez Cloudflare.

**Cadenas** : avec un mot de passe (*Réglages → Options*), le bouton 🔒 verrouille l'application. Claude continue, mais rien n'est visible ni modifiable (jeton compris) sans le mot de passe, et l'application démarre verrouillée.

**Bureau « Claude »** : les programmes que Claude lance ne s'ouvrent jamais sur votre écran. Ils n'interrompent ni votre travail ni un jeu en plein écran.
- **Windows** : Unreal, Blender, un jeu en test et tout ce qu'ils lancent s'ouvrent sur un bureau Windows à part, comme celui de l'invite UAC (les passerelles les lancent avec `Athanor.exe isole`). Ils ne peuvent prendre ni la main, ni votre souris : tant qu'un pilote tourne, un garde lève le verrouillage de la souris d'un jeu lancé sur le bureau de Claude (un test en jeu Roblox, le « play » d'Unreal) et remet en place un curseur qu'il a déplacé (chaque correction dans `logs/souris.log`). On ne les entend pas non plus : tant qu'un pilote tourne, leur son est coupé, sauf quand vous regardez le bureau de Claude ou avec *Réglages → Options → Bureau « Claude » → Entendre le bureau de Claude* (Windows retient la sourdine d'un programme pour son prochain lancement : Athanor rend donc le son dès que le programme se ferme, à l'arrêt du pilote, et à son prochain démarrage après un plantage). *Voir le bureau Claude* bascule l'écran dessus ; la barre en haut, ou Ctrl + Alt + B, vous ramène.
- **Linux**, et les programmes Linux sous WSL que WSLg afficherait sur Windows : chaque pilote fait tourner un serveur X privé (Xvfb). Claude, ses outils et ses serveurs MCP ne voient que cet écran (`athanor ecran`), et votre affichage, X11 ou Wayland, leur est inaccessible. *Voir l'écran de Claude* l'affiche en direct dans l'application.
- Option désactivée, les nouveaux programmes s'ouvrent à nouveau sur votre écran. Ceux qui tournent déjà restent où ils sont jusqu'à leur fermeture : l'application vous laisse aller les fermer.

L'interface est traduite en anglais, français, espagnol, allemand, italien et portugais.

---

## Dans un terminal

<img src="docs/images/terminal.png" alt="Athanor dans un terminal : les projets et les pages à gauche, l'état du pilote, le dernier commit de cet ordinateur et sur GitHub, le quota de l'abonnement et les cycles déjà faits au milieu, le plan en vagues à droite">

Sous Linux et macOS, Athanor tourne aussi dans le terminal, comme btop ou htop : le même Athanor que sa fenêtre, sur le même serveur, donc les deux montrent toujours la même chose. Fait pour un serveur joint en SSH, un Linux sans bureau, ou simplement votre propre terminal.

- **L'ouvrir** : `./athanor tui` dans le dossier d'Athanor (ou le fichier téléchargé : `./Athanor-Linux-x64 tui`). En SSH (`SSH_CONNECTION` défini), ou sur un Linux sans écran (ni `DISPLAY` ni `WAYLAND_DISPLAY`), `./athanor` seul l'ouvre. `./athanor gui` ouvre la fenêtre quoi qu'en dise la détection.
- **Sur un serveur** : lancez Athanor avec un utilisateur normal, pas en root : Claude Code refuse de travailler en root (Installation & santé le dit, et *Démarrer* refuse). Sur un VPS neuf : `adduser NOM`, `usermod -aG sudo NOM`, connectez-vous en `NOM`, puis `./Athanor-Linux-x64`.
- **Avec Athanor déjà ouvert** (sa fenêtre, ou un autre terminal) : `./athanor tui` montre ce même Athanor. Quand c'est le terminal qui a lancé Athanor, Athanor s'arrête avec lui (les pilotes continuent) ; *w* ouvre aussi la fenêtre, sur un ordinateur avec écran.
- **Les pages** : *1* Suivi (l'état, le dernier commit ici et sur GitHub, le quota, les cycles et le plan), *2* Journal (en direct ; *o*, *r*, *u* filtrent outils, résultats et sous-agents, */* cherche, *f* suit), *3* Demandes (*n* une nouvelle, *e* une remarque en cours de route, *d* supprime, *Alt+↑/↓* réordonne), *4* Discuter (son champ est prêt dès l'ouverture de la page : *Entrée* envoie, *Alt+Entrée* une nouvelle ligne ; *Échap* le quitte pour *c* une nouvelle conversation et *m* le modèle, *i* y revient), *5* Installation & santé (*v* vérifie tout ; *r* répare et *I* installe tout directement dans le terminal, chaque commande affichée et sudo répondu sur place, puis Athanor revient ; *T* le jeton). *6* Pour vous (*Entrée* répond, avec les valeurs demandées dans des champs masqués ; *k* passe), *7* Équipe, *8* GitHub (*c* connecte cet ordinateur dans le terminal, *l* lie un de vos dépôts, *r* en crée un privé, *p* envoie, *a* envoi automatique, *u* délie), *9* Réglages (*Entrée* modifie un réglage, secrets masqués ; *l*, *p*, *a* la langue, la palette, l'accent), *0* À distance (*o* active ou coupe l'accès, le QR code d'appairage dessiné dans le terminal avec son lien, *v* montre les 6 chiffres pendant 10 secondes, *r* renomme et *d* retire un appareil de confiance : un serveur joint en SSH appaire ainsi un téléphone sans fenêtre). Sur les pages 8, 9 et 0, leurs propres lettres passent en premier, en minuscule comme en majuscule. Les formulaires ont des boutons *Enregistrer* et *Annuler* (Tab, ou un clic ; *F2* enregistre aussi) ; un formulaire refusé par le serveur reste ouvert avec la raison, et en fermer un qui contient du texte demande d'abord.
- **Partout** : *s* Démarrer (avec les mêmes questions que la fenêtre : pas encore de demande, outils non installés, GitHub en avance), *a* Arrêter après le cycle (à nouveau : le laisser continuer), *x* Arrêter maintenant, *p* ou *[ ]* le projet, *N* un nouveau projet, *M* le gère (renommer, archiver, retirer de la liste ; les archivés reviennent depuis *p*), *g* Récupérer le travail de GitHub, *?* toutes les touches, *q* quitte. La souris clique projets, pages et lignes ; la molette fait défiler.
- **Son allure** : la palette et l'accent de l'application, sa langue, en couleurs 24 bits quand le terminal dit les avoir (sinon 256). En SSH, le terminal ne le dit pas toujours : `ATHANOR_COULEURS=24 ./athanor tui` les force. Tant que le terminal fait tourner Athanor, ce qu'Athanor affiche va dans `logs/terminal.log`.

## Ce que Claude peut et ne peut pas faire

| | |
|---|---|
| Écrire | seulement dans le dossier du projet (et dans `tools/` de l'outil pour ce qu'il installe) |
| Lire | seulement le projet, `tools/` et les fichiers du système Linux ; ni vos documents, ni les disques Windows, ni `.env` |
| Internet | oui, librement |
| Installer des outils | oui : dans `tools/` (outils Linux, logiciels Windows portables dans `tools/windows/`), ou dans un conteneur Docker jetable ; chaque installation est notée dans `tools/INSTALLS.md` |
| Installer des logiciels Windows | oui, avec winget ou un installateur : pour votre utilisateur, ou avec les droits administrateur si vous l'avez autorisé (une seule demande Windows) |
| Pare-feu Windows | réglé tout seul avec la même autorisation : les programmes du projet peuvent recevoir des connexions **du réseau local seulement** |
| Vos logiciels Windows | oui, par les passerelles prévues (voir ci-dessous). **C'est une sortie voulue de la barrière** : ces programmes tournent avec vos droits Windows |
| `cmd.exe`, `powershell.exe` | non, bloqués depuis la barrière (les passerelles restent possibles) |
| Modifier le pilote, la barrière, ses réglages | non (`system/` est hors de ses dossiers) |

Ce tableau décrit le mode WSL, celui par défaut. Dans le mode Windows natif ci-dessous, il n'y a pas de barrière.

### Mode Windows natif (expérimental)

*Réglages → Options → Mode d'exécution → Windows natif*. Le pilote est `Athanor.exe` lui-même et Claude Code est le programme Windows (ses commandes tournent dans Git Bash, le shell que Claude Code utilise sous Windows) : pas de WSL, le dossier Windows du projet et les programmes Windows sont atteints directement, et l'accès aux fichiers est plus rapide.

**Il n'y a pas de barrière dans ce mode** (Claude Code n'a pas de bac à sable sous Windows natif) : Claude peut lire et modifier n'importe quel fichier de votre PC et lancer n'importe quel programme, sans demander. Le garde-fichiers limite toujours ses outils de fichiers au projet et à `tools/`, et ses consignes lui disent d'y rester, mais rien ne l'impose à ses commandes. À réserver aux projets de confiance. Cela vaut aussi pour les fichiers d'Athanor : dans ce mode, les commandes de Claude pourraient lire le `.env` de l'outil (votre jeton), les clés des serveurs MCP et la clé GitHub, et modifier le pilote lui-même.

- Prérequis : Git pour Windows, jq, Python, Node.js, GitHub CLI, uv et Claude Code pour Windows ; *Installation et santé → Tout installer* les installe (winget, puis l'installeur de Claude Code).
- Le projet doit être dans un dossier d'un lecteur Windows (`C:\...`, `D:\...`), pas dans le système de fichiers WSL. Son historique (cycles, statistiques) passe d'un mode à l'autre.
- Le mode s'applique aux pilotes lancés ensuite ; arrêtez d'abord ceux qui tournent. Si vous fermez Athanor pendant que des pilotes natifs tournent, il demande s'ils continuent en arrière-plan ou s'arrêtent.
- L'écran Linux de Claude n'existe pas dans ce mode ; le bureau Windows de Claude garde toujours à l'écart les programmes qu'il ouvre.

---

## Vos outils

Claude travaille sous Linux (WSL2 sous Windows) et atteint vos logiciels Windows par des passerelles. Le pont exécute chaque commande **hors** de la barrière, avec vos droits :

| Passerelle | Ce qu'elle fait |
|---|---|
| `unreal-editor`, `unreal-cmd`, `unreal-uat` | ouvre l'éditeur Unreal (piloté par Unreal MCP si activé), lance commandlets et tests, fabrique les versions Windows et Android |
| `roblox-studio` | ouvre Roblox Studio sur une place du projet (rien si un Studio est déjà ouvert) |
| `blender` | le Blender du PC, généralement en arrière-plan avec un script Python (`blender -b -P scene.py`) ; `blender --open FICHIER.blend` ouvre sa fenêtre (rien si un Blender est déjà ouvert) |
| `adb` | l'outil Android du PC : installer et tester sur un téléphone branché en USB |
| `tool IMAGE CMD…` | lance une commande dans un conteneur Docker jetable qui ne voit que le projet |
| `install-windows ID` / `--file setup.exe` | installe un logiciel Windows (winget ou installateur), avec élévation si autorisée |
| `pc-exec [--admin] programme.exe` | lance un programme Windows construit ou téléchargé par le projet |
| `windows-var NOM CHEMIN` | définit une variable d'environnement Windows pour les outils de développement (`ANDROID_HOME`, `JAVA_HOME`…) |

Les logiciels installés sont détectés sur **tous les disques** (`C:`, `D:`…), et la version la plus récente l'emporte. La liste détectée est écrite avant chaque cycle dans `.athanor/PC_TOOLS.md`. Si la détection manque un logiciel, indiquez son chemin dans *Réglages* (`UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`).

**Unreal Engine (MCP)** : installez **Unreal Engine** depuis le catalogue vérifié de *Skills & MCP*, activez-le pour le projet (son adresse est un réglage du serveur) et, en mode WSL, lancez une fois `Athanor.exe reseau-unreal`. Claude active lui-même les plugins du MCP dans le projet Unreal, et le pilote ouvre l'éditeur avant chaque cycle. Claude le pilote (acteurs, lumières, matériaux, tests), lance tests et captures avec `unreal-cmd`, et fabrique le jeu Windows avec `unreal-uat` dans `deliverable/`. Le MCP d'Unreal est expérimental : s'il ne répond pas, Claude continue sans.

**Roblox Studio (MCP)** : installez **Roblox Studio** depuis le catalogue vérifié de *Skills & MCP* et activez-le pour le projet. Dans Studio, *Assistant > … > Manage MCP Servers*, activez *Enable Studio as MCP server*. Avant chaque cycle, le pilote ouvre dans Studio la première place `.rbxl`/`.rbxlx` du projet (sur le bureau de Claude si cette option est activée), sauf si un Studio est déjà ouvert : Claude travaille alors dans le vôtre. Claude modifie scripts et instances, lance des tests en jeu et insère des ressources via le serveur MCP intégré à Studio. C'est un programme Windows : il tourne sous Windows, hors de la cage (voir *Skills & MCP, en sécurité* plus haut). Expérimental.

**Blender (MCP)** : installez **Blender** depuis le catalogue vérifié de *Skills & MCP* (le serveur officiel de Blender Lab ; nécessite Blender 5.1+, uv et Git pour Windows) et activez-le pour le projet. Dans Blender, installez le module MCP depuis [blender.org/lab/mcp-server](https://www.blender.org/lab/mcp-server/), activez son *Auto Start* et autorisez l'*accès en ligne* (*Préférences > Système*). Avant chaque cycle, le pilote ouvre le premier `.blend` du projet (ou un Blender vide), sauf si un Blender est déjà ouvert : Claude travaille alors dans le vôtre. Claude exécute du Python dans Blender, inspecte la scène, prend des captures et cherche dans la documentation de Blender ; il enregistre la scène dans le projet en `.blend`. Le serveur exécute dans Blender le code écrit par Claude, avec vos droits, hors de la cage. Expérimental.

**Tout serveur MCP qui pilote un programme** : le programme à ouvrir avant chaque cycle fait partie de la description du serveur, dans le catalogue ou dans votre bibliothèque (*Voir / modifier*, clé `ouvrir`) : le programme (une passerelle comme `blender`, ou un programme Windows `C:\…\nom.exe`), ses arguments (`{FICHIER}` : le fichier du projet trouvé), les fichiers du projet à chercher (`*.blend`), le nom du processus ou le port qui disent qu'il est déjà ouvert, et combien de temps attendre ce port. Les réglages (`${PORT}`…) viennent des champs sous le serveur. Un nouveau serveur de ce genre ne demande aucune modification d'Athanor. L'ouverture de Blender est décrite ainsi ; Roblox Studio et Unreal gardent la leur (build Rojo, plugin MCP de l'éditeur).

**Logiciels manquants** : Claude se débrouille seul (version portable dans `tools/windows/`, sinon winget, sinon la tâche administrateur si elle est autorisée). Si une installation échoue, il l'écrit dans `.athanor/TODO_FOR_YOU.md` et continue autrement.

**Jeu mobile (Unreal)** : Android d'abord (SDK et Java portables, `.apk` installé sur le téléphone branché en USB), iOS gardé portable (`docs/IOS_PORT.md`).

**Docker (facultatif)** : il faut Docker Desktop avec *Settings > Resources > WSL integration*.

---

## Réglages

Tout se modifie dans l'application (*Réglages*), ou dans `.env` ([`.env.example`](.env.example) documente chaque clé).

| Clé | Rôle |
|---|---|
| `CLAUDE_CODE_OAUTH_TOKEN` | jeton de votre abonnement (`claude setup-token`), jamais partagé |
| `MODEL` | modèle du chef : `claude-fable-5-1`, `claude-opus-5-5`, `claude-sonnet-5-5` (plus économe), `claude-haiku-5-5`, ou les alias `fable`, `opus`, `sonnet` |
| `FALLBACK_MODEL` | pris **automatiquement** quand le quota de `MODEL` est épuisé, jusqu'à sa réouverture, puis retour au principal. Vide = pause |
| `SUBAGENT_MODEL` | modèle des sous-agents ; vide = le même que le chef, `claude-sonnet-5-5` économise le quota, `auto` = le chef le choisit à chaque appel |
| `CHEAP_MODEL` | modèle du sous-agent `executor` (par défaut `haiku`) : le contexte et le quota du chef restent pour la réflexion |
| `FRAMING_MODEL` | modèle du cycle de cadrage, qui écrit le plan ; vide = `MODEL` |
| `WORKER_MODEL` | modèle des ouvriers en mode parallèle ; par défaut `sonnet`, vide = `MODEL` |
| `FRAMING_MAX_MIN` | durée maximale du cadrage, en minutes (25 par défaut) |
| `RELAY_CONTEXT_K` | contexte, en milliers de tokens, au-delà duquel une nouvelle session reprend le jalon (110 par défaut, 0 = jamais) |
| `EFFORT` | effort de réflexion : vide = normal, `low` à `max` |
| `LANGUAGE` | langue dans laquelle Claude travaille et documente (réglée par l'application) |
| `MAX_HOURS` | arrêt de sécurité (168 par défaut, une semaine) |
| `QUOTA_WAIT_MIN` | attente quand l'heure de réouverture du quota est illisible |
| `IDLE_MAX_MIN`, `CYCLE_MAX_MIN`, `CLOSE_AFTER_MILESTONE` | surveillance des cycles (muet 30 min ou plus long que 3 h → coupé et repris), un jalon par cycle |
| `KEEP_AWAKE` | le PC reste éveillé tant qu'un pilote tourne : `1` (par défaut ; l'écran aussi pour Roblox Studio et Unreal), `screen` (l'écran toujours), `0` (désactivé) |
| `RTK`, `TZ` | compression des sorties de commandes, fuseau horaire des journaux (vide = celui du système) |
| `PONYTAIL` | les règles de Ponytail pour les cycles qui construisent : `1` (par défaut), `0` (désactivé) |
| `UE_DIR`, `BLENDER_EXE`, `ANDROID_SDK_DIR`, `ANDROID_JDK_DIR` | chemins des outils quand la détection automatique les manque (facultatifs, absents par défaut) |

Si le quota global est épuisé (tous modèles), le pilote attend la réouverture et reprend seul.

---

## Sécurité

Ce que la barrière garantit, et ce qu'elle ne fait volontairement pas :

- **Les commandes de Claude** tournent dans le bac à sable de Claude Code (bubblewrap + seccomp). Elles ne peuvent écrire que dans le dossier du projet et `tools/`. Elles ne peuvent lire ni le reste de votre dossier Linux, ni les disques Windows, ni lancer `cmd.exe` ou `powershell.exe` directement. *Installation & santé → Tester la barrière* le prouve avec une vraie session. Les outils de fichiers de Claude (Read, Edit, Write…) sont limités de la même façon par un garde-fou (`athanor hook fichiers`, [`app/outil_gardes.go`](app/outil_gardes.go)). Sur macOS, la barrière est Seatbelt, avec les mêmes règles.
- **Les logiciels Windows sont une sortie voulue.** Les passerelles tournent hors de la barrière, avec vos droits, et `pc-exec` lance n'importe quel programme Windows que Claude dépose dans le projet ou dans `tools/windows`. Traitez un projet comme du code que vous lanceriez vous-même.
- **Les droits administrateur sont sur autorisation.** *Droits administrateur pour les installations → Réparer* vous demande votre accord une fois et crée une tâche planifiée élevée (`Athanor - installations`). Elle lance une copie d'`Athanor.exe` gardée dans `C:\ProgramData\Athanor`, que seuls les administrateurs peuvent modifier. La tâche ne lance que des paquets winget et des programmes situés dans vos dossiers de projets ou dans `tools\windows`. Elle laisse aussi les programmes du projet accepter des connexions du réseau local sans la fenêtre du pare-feu. Retirez-la quand vous voulez avec `Athanor.exe autoriser-admin --retirer`.
- **Le pilote ne fait jamais confiance au dossier du projet.** Il garde son propre état (pid, verrou, réglages de la barrière, consignes, compteurs) dans `logs/`, hors de portée de Claude, et n'écrit jamais à travers un lien symbolique posé dans `.athanor/`. Il vérifie chaque valeur relue, et appelle git sans hooks, filtres, pilotes de fusion, fsmonitor, diff externe ni programmes de signature. Il lance Claude Code avec ses seuls réglages et serveurs MCP (`--setting-sources user`, `--strict-mcp-config` quand la commande les connaît), et `tools/` n'est jamais dans son `PATH`.
- **L'application** n'écoute que sur `127.0.0.1` et refuse tout autre en-tête `Host` (DNS rebinding). Seules les fenêtres qu'elle ouvre elle-même peuvent s'en servir : chacune s'ouvre sur une adresse à usage unique qui lui donne un cookie, si bien qu'un programme de l'ordinateur qui trouve le port (une commande lancée par Claude, par exemple) n'obtient ni la page ni l'API. Elle exige aussi un en-tête secret propre à la session sur chaque appel et stocke le mot de passe salé et haché. Elle n'envoie jamais les secrets du `.env` sans demande, et, verrouillée, n'affiche que « en marche / phase / cycle ». Elle refuse un dossier de projet qui contiendrait l'outil, une racine de disque ou tout votre dossier personnel.
- **L'accès à distance** (coupé par défaut) : un second serveur n'écoute que sur `127.0.0.1`, pour `cloudflared`, et ne répond qu'au nom d'hôte du tunnel. Sans la clé d'un appareil de confiance, rien ne passe, ni la page ni l'API. Un appareil reçoit sa clé en ouvrant le lien d'appairage : un code aléatoire, à usage unique, valable 2 minutes et renouvelé tout seul, qui n'existe que tant que la page *À distance* est ouverte sur l'ordinateur. Il n'ouvre qu'une demande : l'appareil doit ensuite taper 6 chiffres que l'ordinateur n'affiche que sous la souris, renouvelés toutes les 20 secondes, si bien qu'une photo du QR code ne suffit pas ; trois chiffres faux mettent fin à la demande, et après 20 mauvais codes en 10 minutes, l'appairage se met en pause. Chaque nouvel appareil déclenche une notification sur l'ordinateur. La clé est un long secret aléatoire dans un cookie `HttpOnly` et `Secure` ; la configuration n'en garde que le SHA-256. Avec un mot de passe, chaque appareil se déverrouille de son côté, et celui qui tape 5 mots de passe faux est retiré des appareils de confiance (l'ordinateur le signale). Un appareil n'atteint jamais les secrets, les réglages de l'accès à distance, le mot de passe ni les dossiers de l'ordinateur. Sur sa page *À distance*, il voit ses notifications, l'état de l'accès, les appareils de confiance (pour les renommer ou les retirer) et *Cet ordinateur, pour les autres ordinateurs* ; jamais l'interrupteur, les réglages d'adresse ni le lien d'appairage, son QR code ou ses chiffres, que le serveur n'envoie même pas à un appareil : un appareil ne peut jamais en faire entrer un autre. Au-delà de 60 requêtes par minute sans clé, une adresse est refusée pendant 10 minutes. Avec votre propre domaine, Cloudflare Access peut arrêter les inconnus avant l'ordinateur. Les notifications sont chiffrées pour chaque appareil seul (Web Push, RFC 8291) et signées d'une clé qu'Athanor crée une fois (VAPID) : le service de notifications du navigateur ne fait que relayer des octets qu'il ne peut pas lire, et Athanor n'envoie qu'aux services de notifications des navigateurs (ceux de Google, d'Apple, de Mozilla, de Microsoft), jamais à une adresse qu'un appareil choisirait. Le trafic est en HTTPS jusqu'à Cloudflare, qui le relaie vers votre ordinateur.
- **Les sessions « Demander à Claude »** (réparation de l'outillage) tournent hors de la barrière, dans le dossier de l'outil, sans `sudo` : c'est voulu, pour réparer l'outillage lui-même. Ce qui demande `sudo` ou Windows est écrit pour vous dans `logs/TODO_FOR_YOU.md`.
- **GitHub** : chaque ordinateur envoie avec une clé à lui sur votre compte GitHub (« Athanor (ordinateur) »), créée par *Se connecter à GitHub* et gardée dans les données GitHub d'Athanor (`~/.local/share/athanor/github/connexion` ; dans le dossier personnel Linux de WSL sous Windows), que la barrière ne peut pas lire. Elle ouvre tous les dépôts que votre compte peut modifier : *Se déconnecter* la retire de l'ordinateur, et *Settings › SSH and GPG keys* de GitHub la révoque. Les connexions elles-mêmes (pour connecter l'ordinateur, ou créer un dépôt) vivent dans un dossier temporaire supprimé juste après : aucune connexion GitHub n'est jamais gardée. Le pilote envoie depuis un dossier git privé : rien du `.git` du projet (que Claude peut écrire) n'est utilisé, l'identité du serveur de GitHub est vérifiée avec les clés que GitHub publie, et `GH_TOKEN`/`GITHUB_TOKEN` sont retirés de l'environnement de Claude.
- **Plusieurs ordinateurs** ne communiquent que par le dépôt du projet, chacun avec sa propre clé : aucun port n'est ouvert et aucun ordinateur n'en atteint un autre. Ce qu'un ordinateur publie sur lui-même : son nom, ce que vous avez écrit qu'il sait bien faire, sa version d'Athanor et ce qu'il construit ; rien sur l'ordinateur lui-même. Les commits des autres arrivent par le même dossier git privé, sont fusionnés par git avec les hooks et les filtres coupés, et l'état du pilote, ses journaux et les secrets ne quittent jamais un ordinateur.
- **Rien de personnel n'est jamais commité** : `.env`, `logs/`, `tools/` et les réglages de l'application sont ignorés par git, et les projets vivent hors du dossier de l'outil.

Vous avez trouvé une faille ? Ouvrez plutôt un [avis de sécurité privé](https://github.com/Aprilox/Athanor/security/advisories/new) qu'une issue publique.

---

## Laisser tourner plusieurs jours

- Les pilotes tournent en arrière-plan, sans fenêtre (dans WSL sous Windows, sauf en mode natif) : vous pouvez fermer l'application.
- Le PC reste éveillé tant qu'un pilote tourne (`KEEP_AWAKE`), même application fermée ; pour Roblox Studio et Unreal, l'écran reste allumé aussi, car une session Windows verrouillée n'affiche rien (ni captures, ni tests en jeu). Ne verrouillez pas vous-même la session (Win + L) avant une nuit de Roblox ou d'Unreal, et vérifiez que l'écran de veille ne demande pas de reconnexion à la reprise.
- *Réglages → Options* : « Ouvrir Athanor au démarrage de l'ordinateur » et « Relancer les pilotes qui tournaient, à l'ouverture de l'application ».
- Arrêt de sécurité au bout de `MAX_HOURS` (depuis le premier démarrage du projet) ; pour continuer, augmentez la valeur dans *Réglages*.

## Projet terminé, suite, nouveau projet

- **Demandes à la suite** : page *Demandes*. Dès que la demande en cours est terminée, le pilote prend la première de la file, l'ajoute à `REQUEST.md` et rouvre le projet avec un jalon de reprise.
- **Remarque en cours de route** : page *Demandes*, lue au début du cycle suivant, sans redémarrage.
- **Rouvrir à la main** : remplacez `STATUS: DONE` par `STATUS: REOPENED` dans `.athanor/FINAL_REPORT.md`, puis *Démarrer*.
- **Nouveau projet** : sélecteur → *+ Nouveau projet*. **Nettoyer un projet fini** : supprimez son dossier `.athanor/`.

---

## Les commandes à la main (sans l'application)

Tout ce que fait l'application est une commande de l'exécutable (`Athanor.exe` sous Windows, le `./Athanor-Linux-x64` téléchargé, `Athanor.app/Contents/MacOS/athanor` sur le Mac, ou `athanor` dans le dossier d'Athanor ; `Athanor.exe wsl COMMANDE …` la lance dans WSL) :

| Commande | Pour |
|---|---|
| `installer` | tout installer (Linux, macOS, le côté WSL) ; Windows : `installer-windows` (WSL) ou `installer-natif` |
| `configurer` | jeton, modèles, options (la langue se règle dans l'application) |
| `demarrer [--background] DOSSIER` | lancer le pilote sur un projet |
| `suivre DOSSIER` | suivre en direct |
| `arreter --apres-cycle DOSSIER` | arrêter proprement à la fin du jalon en cours |
| `arreter DOSSIER` | arrêter tout de suite |
| `tester-cage` | prouver la barrière avec une vraie session |
| `autoriser-admin [--retirer]` | autoriser ou retirer les installations administrateur (Windows) |
| `reseau-unreal` | réseau WSL pour le MCP d'Unreal (Windows, une fois) |
| `tui` / `gui` | l'application dans le terminal / dans sa fenêtre (Linux, macOS ; voir [Dans un terminal](#dans-un-terminal)) |
| `aide` | toutes les commandes |

Sans dossier, les commandes utilisent `projet` dans le dossier d'Athanor. La demande se lit et s'écrit dans `DOSSIER\.athanor\REQUEST.md`, les demandes suivantes dans `DOSSIER\.athanor\queue\` (un fichier par demande, prises dans l'ordre).

## Où regarder

- `.athanor/STATE.json` : phase, cycle, modèle, jalon en cours, reprise prévue ; `stats.tsv` : une ligne par cycle.
- `.athanor/PLAN.md`, `LOG.md` ; `pilot.log` (cycles, quotas, bascules) ; `logs/` (brut de chaque cycle).
- À la fin : `.athanor/FINAL_REPORT.md`, `TEST_GUIDE.md`, et le livrable dans `deliverable/`.
- Dans le dossier d'Athanor : `logs/` (réparations, test de la barrière, `TODO_FOR_YOU.md`).

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
app/                   les sources Go, écrites seulement quand une réparation en a besoin (Recompiler Athanor, Claude)
```

## Organisation du dépôt

```
app/                   les sources Go de l'application, l'interface (HTML, CSS, JS sans dépendance), ressources Windows ;
                       go run . construire construit Athanor.exe et athanor, qui portent chacun template/, .env.example,
                       LICENSE et ces sources (app/embarque.go)
template/              CLAUDE.md (consignes de Claude), guides/ (les parties dont seuls certains cycles ont besoin), PC_TOOLS_GUIDE.md, .claude/agents/ (l'équipe)
docs/images/           images du README
docs/releases/         les nouveautés de chaque version (affichées sur la page de la version)
.env.example           tous les réglages, documentés
```

---

## FAQ

<details>
<summary><b>Combien ça coûte ?</b></summary>

Seulement votre abonnement Claude : Athanor enchaîne des sessions Claude Code, rien d'autre. Les modèles les plus puissants consomment le quota le plus vite ; Sonnet dure plus longtemps. `FALLBACK_MODEL` prend le relais quand le quota du modèle principal est épuisé, et `CHEAP_MODEL` s'occupe du travail mécanique. La page *Suivi* montre le coût de chaque cycle au prix catalogue, c'est-à-dire ce que paie le quota de votre abonnement.
</details>

<details>
<summary><b>Que peut-il construire ?</b></summary>

Tout ce que Claude Code peut construire et vérifier seul dans un dossier : sites et applications web, outils de bureau et en ligne de commande, jeux (Unreal Engine, Godot, web), applications Android, bibliothèques. Les projets qui ont besoin de vos logiciels Windows passent par les passerelles. Les vérifications sont écrites par Claude pour chaque projet (`check.sh`), avec des captures pour tout ce qui est visuel.
</details>

<details>
<summary><b>Puis-je le guider pendant qu'il travaille ?</b></summary>

Oui. *Demandes → Remarque en cours de route* est lue au début du cycle suivant. Les nouvelles demandes vont dans la file et se réordonnent. *Arrêter après le cycle* termine le jalon en cours, l'enregistre, puis s'arrête : rien n'est perdu.
</details>

<details>
<summary><b>Va-t-il me gêner pendant que j'utilise le PC ?</b></summary>

Les pilotes tournent en arrière-plan, sans fenêtre. Les programmes qu'ils ouvrent (un éditeur, un jeu en test) vont sur le bureau « Claude », jamais sur votre écran. Le PC reste éveillé tout seul tant qu'un pilote tourne.
</details>

<details>
<summary><b>Le projet reste-t-il propre ?</b></summary>

Oui : Athanor n'ajoute que `.athanor/`, tenu hors de git par `.git/info/exclude` (jamais par votre `.gitignore`). Supprimez-le à la fin et le projet est tel que vous l'avez laissé. Pour partager l'atelier avec ceux qui travaillent avec vous, cochez *Partager l'atelier (.athanor/) sur GitHub* dans les réglages GitHub du projet : qui clone le dépôt et l'ajoute à Athanor reprend le travail là où il s'était arrêté.
</details>

<details>
<summary><b>Puis-je l'utiliser sur plusieurs PC ?</b></summary>

Oui. Rien n'est lié à un nom d'utilisateur ni à une lettre de disque : les chemins viennent de votre profil et les logiciels sont détectés sur tous les disques. Téléchargez Athanor sur chaque PC et lancez-y *Tout installer*. Pour continuer le même projet depuis un autre PC : liez-le à GitHub avec *Partager l'atelier (.athanor/) sur GitHub* ; sur l'autre PC, connectez-vous à GitHub, créez un projet dans un dossier vide lié au même dépôt (*Utiliser un de mes dépôts*) et *Récupérer le travail de GitHub* (*Démarrer* le propose aussi) : le plan suit et le pilote le reprend là où il s'était arrêté. Les deux en même temps : [Plusieurs ordinateurs](#plusieurs-ordinateurs).
</details>

---

## Dépannage

| Symptôme | Solution |
|---|---|
| *WSL et la distribution Linux* en défaut dans Installation & santé | *Tout installer* de l'onglet WSL n'a pas encore préparé la distribution Linux (ouvrez « Ubuntu » une fois après le premier redémarrage, puis relancez-le) |
| Le pilote s'arrête aussitôt, message sur la barrière | *Réparer* sur la ligne *Barrière du système* : bubblewrap, socat ou le filtre seccomp manquent |
| Test de la barrière : « cmd.exe lancé depuis la barrière » | le filtre seccomp manque : *Réparer* sur la ligne *Barrière du système* |
| Unreal : « MCP indisponible pour ce cycle » | éditeur 5.8 avec les plugins Unreal MCP, *Auto Start Server* coché, réseau WSL « mirrored » (`Athanor.exe reseau-unreal`) |
| `tool` : « Docker introuvable » | lancez Docker Desktop, activez *WSL integration* |
| « Quota atteint, Claude patiente » | normal ; avec `FALLBACK_MODEL`, Claude continue sur le modèle de secours, sauf si la limite atteinte est celle du compte (5 heures, la semaine), que le modèle de secours partage |
| « Problème de connexion » | jeton absent ou faux : *Réglages*, ou *Réparer* sur la ligne *Jeton et accès au modèle* |
| Une installation demande les droits administrateur | *Installation & santé* → *Droits administrateur pour les installations* → *Réparer* (une seule fois) |

---

## Publier une version

Poussez un tag de version : GitHub lance d'abord toutes les vérifications de la CI sur le commit du tag (tests sous Windows, Linux et macOS, exécutable unique, toutes les cibles), puis compile les applications et publie la version ([`.github/workflows/release.yml`](.github/workflows/release.yml)). Si une seule vérification échoue (ou la suite), rien n'est publié : le tag est supprimé, ainsi que la version si elle avait déjà été créée, et les mises à jour d'Athanor ne la proposent jamais. Corrigez, commitez, puis poussez à nouveau le même tag (`git tag -f v2.42.0 && git push origin v2.42.0`).

```bash
git tag v2.43.0 && git push origin v2.43.0
```

Gardez la version de [`app/winres/athanor.rc`](app/winres/athanor.rc) et `versionAthanor` dans [`app/commandes.go`](app/commandes.go) alignées sur le tag (un test vérifie qu'elles concordent), et écrivez les nouveautés dans `docs/releases/VERSION.md` (par ex. [`docs/releases/2.31.0.md`](docs/releases/2.31.0.md)) : elles ouvrent la page de la version.

Chaque push est vérifié sous Windows, Linux et macOS ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)), l'exécutable unique compris (rien d'écrit avant l'accord, dossier d'Athanor installé, rien à côté du fichier) ; `cd app && go run . construire --all` compile toutes les cibles dans `dist/`. Une version publie `Athanor-Windows-x64.exe`, `Athanor-Windows-ARM64.exe`, `Athanor-macOS.zip` (Athanor.app, signé ad hoc sur une machine Mac de GitHub), `Athanor-Linux-x64`, `Athanor-Linux-ARM64` et `SHA256SUMS.txt`, sous les mêmes noms à chaque version ; les mises à jour d'Athanor téléchargent ces mêmes fichiers.

Après une modification de `app/` sur un PC (une réparation par Claude, ou la vôtre : `athanor reparer --diff` écrit d'abord les sources dans le dossier d'Athanor si elles n'y sont pas), `./athanor reparer --recompiler` (`.\Athanor.exe reparer --recompiler` sous Windows) y recompile les exécutables du kit : il montre la modification par rapport aux sources de l'exécutable en cours, installe un Go portable hors du kit s'il n'en trouve pas, lance `go vet`, compile, lance l'autotest du nouvel exécutable et seulement ensuite le met en place, en gardant l'ancien (`reparer --annuler` le remet).

## Contribuer

Les issues et pull requests sont les bienvenues. Avant d'en ouvrir une :

- `cd app && go vet . && GOOS=windows go vet .` doit passer, ainsi que `go test ./...`.
- Les commentaires et les textes de l'interface sont en anglais. Certains identifiants internes sont en français, parce que le projet a commencé en français ; gardez les noms existants quand vous les touchez. Les nouveaux textes d'interface passent par `t()` et reçoivent leurs traductions dans [`app/static/i18n.js`](app/static/i18n.js).

## Remerciements

- [Claude Code](https://claude.com/claude-code), qui fait le vrai travail.
- [Ponytail](https://github.com/dietrichgebert/ponytail) de Dietrich Gebert (MIT), dont Athanor embarque les règles (`app/ponytail.go`), et [RTK](https://github.com/rtk-ai/rtk), téléchargé à l'installation, qui compresse les sorties de commandes.
- [VirtualDesktop](https://github.com/MScholtes/VirtualDesktop) de Markus Scholtes (MIT), compilé au premier usage : solution de secours quand le bureau « Claude » ne peut pas être créé (fenêtres déplacées sur un bureau virtuel).
- [go-webview2](https://github.com/jchv/go-webview2) (MIT) pour la fenêtre Windows, [purego](https://github.com/ebitengine/purego) (Apache 2.0) pour l'icône du Dock sur le Mac, et [bubblewrap](https://github.com/containers/bubblewrap) pour la barrière.
- [cloudflared](https://github.com/cloudflare/cloudflared) (Apache 2.0), téléchargé au premier usage, pour le tunnel de l'accès à distance, et [rsc.io/qr](https://github.com/rsc/qr) (BSD-3-Clause) pour ses QR codes.

## Licence

[MIT](LICENSE) © Aprilox
