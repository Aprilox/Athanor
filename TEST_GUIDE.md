# Guide de test — Athanor Hub (branche `hub`)

Ce guide sert à essayer à la main ce que la branche `hub` ajoute à Athanor : le hub (accueil, *Projets*, cloche, Guide), le coffre et les accès, Serveurs, les missions et approbations, E-mail, MCP et Athanor distants. Le numéro de version n'a pas changé (2.52.1) : c'est au propriétaire de le changer avant une publication.

## 1. Lancer

### L'exécutable de test (Windows)

L'exécutable de test est `deliverable/Athanor-hub.exe` à la racine du dépôt (il n'est pas dans git). Il embarque le kit, l'icône et le manifeste, comme une version publiée.

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
| Studio | *Studio* | Les projets construits par Claude, comme avant la branche, avec l'onglet *Accès* en plus |
| Coffre et accès | *Coffre*, *Studio › Accès* | Clés chiffrées, reconnues au collage, testées chez le fournisseur, données par environnement (*dev*, *staging*, *prod*) |
| Serveurs | *Serveurs* | Fiche du serveur en une minute, plans de l'agent à accepter, DNS et HTTPS Cloudflare, déploiements et retour arrière, santé toutes les 15 minutes |
| Missions | *Accueil* (*Nouvelle mission*), *Missions*, *Pour vous*, *Journal d'audit* | Une phrase → une carte → *Lancer* → couloirs, approbations, bouton rouge |
| E-mail | *E-mail* | Comptes SMTP, e-mail de test, réglages donnés à l'environnement d'un projet |
| MCP et distants | *Réglages › Athanor distants*, *Réglages › Clients MCP* | Appairage par code **et** accord à l'écran |

Chaque partie a sa page dans *Guide* et un tutoriel dans *Guide › Tutoriels* ; le « ? » en haut à droite ouvre l'aide de la page.

## 3. À tester à la main

Cochez au fur et à mesure. En clair puis en sombre, sur ordinateur puis sur téléphone (accès à distance), et dans au moins une autre langue que le français.

### En premier (dix minutes)

- [ ] **Accueil.** L'accueil du hub s'affiche, avec la tuile Studio et vos projets ; la visite d'une ligne montre où ils sont.
- [ ] **Menu.** Chaque entrée du menu ouvre sa page sans erreur ; la version affichée (*Réglages*, pied de page) est 2.52.1.
- [ ] **Projets.** Les filtres changent la liste ; un projet en cours dit son jalon ; le lien mène au bon endroit.
- [ ] **Cloche.** Une alerte (par exemple un e-mail de test qui échoue) y apparaît une seule fois et disparaît quand c'est réglé.
- [ ] **Accès.** *Studio › Accès* d'un projet : glisser une clé sur *dev* fait apparaître le badge « N accès » avant *Démarrer* ; la phrase du badge dit ce qui est donné.
- [ ] **Chat général.** L'assistant de l'accueil répond, connaît vos modules et vos projets, et ne donne jamais la valeur d'une clé.
- [ ] **Fiche d'accord.** Une demande d'un agent (accès, déploiement) arrive dans *Pour vous* sous forme de fiche : *Accepter*, *Refuser*, ce qui sera fait, en mots simples ; une action en *prod* attend toujours votre accord.

### Coffre

- [ ] Sans mot de passe du coffre : le bandeau propose « Définissez un mot de passe » et son bouton.
- [ ] *Coller* un bloc `.env` (fausses valeurs) : chaque clé est reconnue avec son type ; une fois enregistrée, la valeur ne se réaffiche jamais.
- [ ] *Tester* une vraie clé (par exemple un jeton Cloudflare en lecture seule) : les droits réels sont affichés.
- [ ] *Installation & santé → Tester le coffre* : la session d'agent échoue à lire un secret *infra* et à appeler un outil interdit (verdict vert).

### Serveurs

- [ ] *Installation & santé → Tester l'agent Serveurs* (Docker Desktop requis) : verdict en six lignes, serveur et clé retirés à la fin.
- [ ] Ajouter un vrai serveur de test (voir `docs/SERVEUR_DE_TEST.md`) : la fiche se remplit, le mot de passe n'est pas gardé.
- [ ] Demander une tâche à l'agent : il propose un plan ; rien ne s'exécute avant *Accepter*.
- [ ] Déployer un petit site, vérifier l'adresse, puis *Revenir à la version d'avant* et revérifier.

### Missions

- [ ] *Nouvelle mission* depuis l'accueil : une phrase donne une carte à vérifier ; *Lancer* crée les couloirs.
- [ ] Un échec de déploiement (serveur éteint, par exemple) : la cloche sonne, la mission dit ce qui a échoué et ce que l'agent a tenté, avec *Réessayer*, *Revenir à la version d'avant*, *Je prends la main*.
- [ ] Le bouton rouge arrête tout ; le *Journal d'audit* garde chaque étape, chaînée.
- [ ] Depuis le téléphone (accès à distance appairé) : la notification « Mise en prod de … ? » arrive et s'approuve à distance.

### E-mail, avec un vrai compte

- [ ] Ajouter un vrai compte (Gmail, Outlook ou Yahoo : un *mot de passe d'application*, pas le mot de passe du compte ; OVH, Brevo, Mailgun, SendGrid : les identifiants SMTP du service).
- [ ] *Envoyer un e-mail de test* à votre adresse : le message arrive (regardez aussi les indésirables).
- [ ] Un mauvais mot de passe : l'échec est dit en mots simples sur la carte et reste dans la cloche jusqu'à un test réussi ; « Retapez le mot de passe » apparaît.
- [ ] *Configurer l'envoi* pour un projet en *dev* : les variables `SMTP_*` apparaissent dans ses accès ; le *×* les retire ; supprimer le compte demande confirmation en nommant les projets.
- [ ] Le fichier `modules\email\comptes.json` du dossier d'Athanor ne contient aucun mot de passe.

### MCP et Athanor distants

- [ ] Relier un second Athanor (un autre PC, ou un second lancement avec d'autres données à part) : code affiché, accord à l'écran ; ses projets arrivent sous la forme `nom@instance`.
- [ ] Un client MCP (Claude Desktop ou Claude Code) pilote Athanor après appairage, et rien avant.

### Sécurité, en passant

- [ ] Athanor n'écoute que sur `127.0.0.1` tant que l'accès à distance n'est pas activé (`netstat -ano | findstr LISTENING` avec le PID d'Athanor).
- [ ] Aucune page, journal ou capture n'affiche la valeur d'une clé, d'un jeton ou d'un mot de passe.

## 4. Ce qui n'a pas été vérifié ici

- La CI sur les 3 systèmes après le push de la branche (macOS en particulier : pas de Mac sur ce PC).
- Le parcours de mission de bout en bout sur un vrai VPS de test, et l'envoi SMTP avec un vrai compte.

## 5. Signaler un bug

Ouvrez une issue sur le dépôt GitHub d'Athanor avec : ce que vous avez fait, ce que vous attendiez, ce qui s'est passé, une capture si possible, et les dernières lignes du journal d'Athanor (*Réglages → Installation & santé*, ou le dossier `logs` du dossier d'Athanor). Retirez toute clé, adresse de serveur ou e-mail personnel avant d'envoyer. Une faille de sécurité passe par un avis de sécurité privé, jamais une issue publique.
