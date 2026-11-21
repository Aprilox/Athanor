# Guide de test — Athanor Hub

Ce guide sert à essayer à la main ce qu'Athanor Hub (3.x) ajoute à Athanor : le hub (accueil, *Projets*, cloche, Guide), le coffre et les accès, Serveurs, le déploiement des projets et les approbations, E-mail, MCP et Athanor distants. La version est 3.0.0 : elle est prête, mais rien n'est poussé ni publié avant la relecture du propriétaire.

## 1. Lancer

### L'exécutable de test (Windows)

L'exécutable de test est `deliverable/Athanor-hub.exe` à la racine du dépôt (il n'est pas dans git ni dans le zip du code ; c'est le même fichier que `Athanor-Windows-x64.exe`). Il embarque le kit, l'icône et le manifeste, comme une version publiée.

**N'abîmez pas votre Athanor installé.** Une seule instance d'Athanor tourne à la fois par dossier de données : lancé tel quel, le nouvel exécutable parlerait à votre Athanor ouvert au lieu de démarrer. Deux façons de faire :

- **Avec des données à part (conseillé).** Copiez `Athanor-hub.exe` dans un dossier vide, par exemple `C:\AthanorTest\`, puis dans PowerShell :
  ```powershell
  New-Item -ItemType Directory -Force C:\AthanorTest\donnees | Out-Null
  $env:LOCALAPPDATA = "C:\AthanorTest\donnees"
  C:\AthanorTest\Athanor-hub.exe
  ```
  Tout ce qu'il écrit va dans `C:\AthanorTest\donnees\Athanor` ; votre Athanor installé, ses projets et son coffre ne sont pas touchés. Pour tout effacer ensuite : fermez-le et supprimez `C:\AthanorTest`.
- **À la place de l'installé.** Fermez d'abord complètement votre Athanor, puis lancez `Athanor-hub.exe`. Il reprend alors vos vrais projets et votre vrai coffre : c'est le test de la mise à jour (parcours P1), à faire seulement après une sauvegarde de `%LOCALAPPDATA%\Athanor`.

**Message Windows SmartScreen.** Le binaire n'est pas signé : Windows affiche « Windows a protégé votre ordinateur ». Cliquez sur *Informations complémentaires*, vérifiez le nom du fichier, puis *Exécuter quand même*. C'est normal pour un exécutable construit sur votre PC ; ne désactivez pas SmartScreen pour autant.

Au premier lancement avec des données à part, Athanor dit où il va ranger ses fichiers et demande avant d'écrire (*Non* : il se ferme sans rien écrire), puis le guide du premier démarrage (langue, raccourci, thème) s'ouvre.

### Depuis le code source

```bash
cd app
go run .                 # l'application
go run . tui             # la même dans le terminal
go run . construire      # Athanor.exe et athanor dans le dossier Athanor
```

Lancez vos essais avec `LOCALAPPDATA` (Windows) ou `HOME` et `XDG_*` (Linux, macOS) pointés vers un dossier temporaire, pour la même raison qu'au-dessus.

### Les tests automatiques

```bash
cd app
gofmt -l . | grep -v third_party     # doit être vide
go vet ./...                          # aussi avec GOOS=linux, windows, darwin
go test -count=1 ./...                # 3 à 4 minutes
```

La CI (`.github/workflows/ci.yml`) refait tout cela sur Windows, Linux et macOS, plus le serveur SSH en conteneur, le faux Cloudflare, deux Athanor appairés, `govulncheck` et le scan de secrets.

## 2. Ce que fait chaque module, et où le trouver

| Module | Où | Ce qu'il faut voir |
|---|---|---|
| Hub | *Accueil*, *Projets*, la cloche, *Guide* | L'assistant de l'accueil, une tuile par module avec ses chiffres, ce qui vous attend ; *Projets* liste tout ce qui tourne, avec les filtres *Tous*, *Besoin de moi*, *En cours* |
| Studio | *Studio* | Les projets construits par Claude, comme avant la branche, avec les onglets *Accès* et *Déploiement* en plus |
| Coffre et accès | *Coffre*, *Studio › Accès* | Clés chiffrées, reconnues au collage, testées auprès de leur service, rangées par catégorie, données projet par projet |
| Serveurs | *Serveurs* | Fiche du serveur en une minute, plans de l'agent à accepter, DNS et HTTPS Cloudflare, déploiements et retour arrière, santé toutes les 15 minutes |
| Déploiement et approbations | *Accueil*, *Studio › Déploiement*, *Pour vous*, *Journal d'audit* | Une phrase → une carte projet ou serveur → *Lancer* ; le serveur préparé, le site en ligne, les approbations, le bouton rouge |
| MCP et distants | *Réglages › Athanor distants*, *Réglages › Clients MCP* | Appairage par code **et** accord à l'écran |

Chaque partie a sa page dans *Guide* et un tutoriel dans *Guide › Tutoriels* ; le « ? » en haut à droite ouvre l'aide de la page.

## 3. À tester à la main

Cochez au fur et à mesure. En clair puis en sombre, sur ordinateur puis sur téléphone (accès à distance), et dans au moins une autre langue que le français.

### En premier (dix minutes)

- [ ] **Accueil.** L'accueil du hub s'affiche, avec la tuile Studio et vos projets ; la visite d'une ligne montre où ils sont.
- [ ] **Menu.** Chaque entrée du menu ouvre sa page sans erreur ; la version affichée (« v3.1.1 », en haut du menu) est 3.1.1.
- [ ] **Projets.** Les filtres changent la liste ; un projet en cours dit son jalon ; le lien mène au bon endroit.
- [ ] **Cloche.** Une alerte (par exemple un serveur injoignable) y apparaît une seule fois et disparaît quand c'est réglé.
- [ ] **Accès.** *Studio › Accès* d'un projet : cocher une clé puis *Enregistrer les modifications* fait apparaître le badge « N accès » à côté de *Démarrer* ; la phrase du badge dit ce qui est donné.
- [ ] **Chat général.** L'assistant de l'accueil répond, connaît vos modules et vos projets, et ne donne jamais la valeur d'une clé.
- [ ] **Fiche d'accord.** Une demande d'un agent (accès, déploiement) arrive dans *Pour vous* sous forme de fiche : *Accepter*, *Refuser*, ce qui sera fait, en mots simples ; une action destructrice attend toujours votre accord.

### Missions

- [ ] **Nouvelle mission à la main.** Accueil → *Nouvelle mission* : tout est visible : le but de la mission en haut (avec *Améliorer avec Claude*), les blocs *Projet Studio* et *Gestion des serveurs* à cocher côte à côte (l'un sous l'autre sur téléphone), le *Coffre* en dessous. But, nom du projet (proposé depuis le but), cochez *Gestion des serveurs*, un ou plusieurs serveurs avec leur rôle (le projet est déployé ici, ou autre rôle : dites alors ce qu'il fait, *Lancer* le demande sinon), des clés API (tapez aussi @ dans le plan des serveurs : le serveur nommé se coche), « A besoin de moi », « Autonome avec l'IA » ou « Autonomie totale » (le mot de passe d'Athanor est demandé une fois), le plan pour les serveurs (nginx s'il le faut) ; *Améliorer avec Claude* sous chaque plan. *Lancer* : un clic, la page *Missions* s'ouvre.
- [ ] **Sans projet.** *Nouvelle mission* → « Pas de projet Studio » : plusieurs serveurs, un lien GitHub et « installe ça et relie-les » ; le cadre suit la tâche, *Relancer* la refait.
- [ ] **nginx.** Un plan pour les serveurs qui dit nginx : la première mise en ligne marche sans Caddy (aucun `/etc/caddy/sites/…` écrit).
- [ ] **Depuis l'assistant.** Demandez « Fais une boutique en Next.js avec Stripe, nginx sur @web, PostgreSQL sur @db » : un résumé s'affiche sous la réponse (le code dans le plan du projet, nginx et PostgreSQL dans le plan des serveurs) ; *Vérifier et lancer* ouvre le formulaire rempli ; corrigez si vous voulez, puis *Lancer*. Dans la conversation, la proposition dit « Mission lancée ».
- [ ] **Le cadre.** Projet et son état, barre des jalons, serveur et son état (préparation, mise en ligne, en ligne, échec et sa raison), clé API, choix de permission ; *Détails* replie l'historique et le lien vers le journal complet.
- [ ] **Le serveur depuis le cadre.** *Terminal* ouvre le terminal du serveur ; *Arrêter le travail sur le serveur* coupe une préparation en cours ; *Relancer le travail sur le serveur* refait la mise en ligne.
- [ ] **Arrêter, modifier, reprendre.** *Pause* ou *Arrêter* pendant un jalon, *Modifier* (un texte changé devient une nouvelle demande du projet), *Reprendre* : le projet repart et finit.
- [ ] **Supprimer.** Une mission qui travaille s'arrête d'abord ; « Retirer aussi le projet… » n'est jamais coché d'avance ; le serveur et la clé restent.

### Coffre

- [ ] Sans mot de passe du coffre : le bandeau propose « Définissez un mot de passe » et son bouton.
- [ ] *Coller* un bloc `.env` (fausses valeurs) : chaque clé est reconnue avec son type ; une fois enregistrée, la valeur ne se réaffiche jamais.
- [ ] *Tester* une vraie clé (par exemple un jeton Cloudflare en lecture seule) : les droits réels sont affichés.
- [ ] *Installation & santé → Tester le coffre* : la session d'agent échoue à lire un secret *infra* et à appeler un outil interdit (verdict vert).

### Serveurs

- [ ] *Installation & santé → Tester l'agent Serveurs* (Docker Desktop requis) : verdict en six lignes, serveur et clé retirés à la fin.
- [ ] Ajouter un vrai serveur de test (voir `docs/SERVEUR_DE_TEST.md`) : la fiche se remplit, le mot de passe n'est pas gardé.
- [ ] Demander une tâche à l'agent : il propose un plan ; rien ne s'exécute avant *Accepter*.

### Déploiement et approbations

- [ ] Depuis l'accueil, une phrase comme « Fais un petit site et mets-le en ligne sur @serveur » donne une carte **projet** avec son déploiement à vérifier ; rien ne démarre avant *Lancer*.
- [ ] Un projet dont l'onglet *Déploiement* n'a pas de serveur se construit sans rien demander d'un serveur, et rien ne s'en plaint.
- [ ] *Studio › Déploiement* : le serveur, l'adresse, votre plan, puis *Après chaque jalon* ou *À la livraison*. L'onglet dit *Premier déploiement en cours*, jamais un échec rouge, puis l'adresse répond.
- [ ] Une demande ajoutée après la livraison : le projet est relivré, puis redéployé une seule fois ; *Revenir à la version d'avant* remet l'ancienne version, à revérifier à l'adresse.
- [ ] *Gérer › Réglages de ce projet* : un mode de permission choisi ici ne vaut que pour ce projet ; *Ignorer les permissions* demande une confirmation et le mot de passe d'Athanor.
- [ ] Un échec de déploiement (serveur éteint, par exemple) : la cloche sonne et dit ce qui a échoué et ce qui a été tenté, avec *Réessayer*, *Revenir à la version d'avant*, *Je prends la main*.
- [ ] Le bouton rouge arrête tout ; le *Journal d'audit* garde chaque étape, chaînée.
- [ ] Depuis le téléphone (accès à distance appairé) : une demande d'accord (le plan d'une tâche de serveur, par exemple) arrive en notification et s'approuve à distance.
- [ ] Avec les données d'une version précédente : la page *Anciennes missions* les liste, en lecture seule (voir `docs/MISSIONS.fr.md`) ; un ancien projet garde ses sites sous *Autres sites de ce projet* et aucun mot de passe n'est redemandé.

### Un compte SMTP dans le coffre

- [ ] *Coffre › Ajouter*, type *SMTP (envoi d'e-mails)* : choisir un service (Gmail, Brevo…) remplit le serveur, le port et le chiffrement ; donner l'utilisateur, l'expéditeur et le mot de passe comme valeur (Gmail, Outlook, Yahoo : un *mot de passe d'application*).
- [ ] La carte montre `SMTP_HOST`, `SMTP_PORT`… et leurs valeurs, jamais le mot de passe.
- [ ] *Modifier* : changer le serveur sans retaper le mot de passe est refusé (« Les réglages ont changé : retapez la valeur… ») ; avec le mot de passe, c'est enregistré.
- [ ] Donner la clé à un projet (*Studio › Accès*) : son pilote reçoit `SMTP_HOST`, `SMTP_PORT`, `SMTP_SECURE`, `SMTP_USER`, `SMTP_FROM` et `SMTP_PASSWORD`.

### MCP et Athanor distants

- [ ] Relier un second Athanor (un autre PC, ou un second lancement avec d'autres données à part) : code affiché, accord à l'écran ; ses projets arrivent sous la forme `nom@instance`.
- [ ] Un client MCP (Claude Desktop ou Claude Code) pilote Athanor après appairage, et rien avant.

### Sécurité, en passant

- [ ] Athanor n'écoute que sur `127.0.0.1` tant que l'accès à distance n'est pas activé (`netstat -ano | findstr LISTENING` avec le PID d'Athanor).
- [ ] Aucune page, journal ou capture n'affiche la valeur d'une clé, d'un jeton ou d'un mot de passe.

## 4. Ce qui n'a pas été vérifié ici

- La CI sur les 3 systèmes après le push de la branche (macOS en particulier : pas de Mac sur ce PC).
- Le déploiement d'un projet de bout en bout sur un vrai VPS de test, et l'envoi SMTP avec un vrai compte.

## 5. Signaler un bug

Ouvrez une issue sur le dépôt GitHub d'Athanor avec : ce que vous avez fait, ce que vous attendiez, ce qui s'est passé, une capture si possible, et les dernières lignes du journal d'Athanor (*Réglages → Installation & santé*, ou le dossier `logs` du dossier d'Athanor). Retirez toute clé, adresse de serveur ou e-mail personnel avant d'envoyer. Une faille de sécurité passe par un avis de sécurité privé, jamais une issue publique.
