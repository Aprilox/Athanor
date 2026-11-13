# Missions

*English: [MISSIONS.md](MISSIONS.md)*

Une mission est une phrase que plusieurs modules réalisent ensemble : **Studio** construit le projet, **Serveurs** prépare l'endroit où il tourne. Cette page dit ce qui se passe entre votre phrase et les voies au travail, ce que l'orchestrateur peut et ne peut pas faire, et ce que vous approuvez. Dans l'application, *Guide › Lancer une mission* la parcourt ; les déploiements eux-mêmes sont dans [DEPLOY.fr.md](DEPLOY.fr.md).

## De la phrase à la carte

1. **La phrase.** Dans la barre *Que voulez-vous faire ?* de la page d'accueil, par exemple « Fais un site pour la boulangerie Martin et mets-le en ligne sur martin.fr, avec une dev protégée sur @vps ». Taper `@` liste vos projets, vos serveurs et vos entrées du coffre **par leur nom** (jamais une valeur) ; un choix devient une puce. Les puces sous la barre ajoutent un projet récent, un serveur ou *Nouveau projet*. Athanor revérifie chaque mention (une inconnue est refusée) et garde la phrase avec tout texte qui ressemble à un secret masqué.
2. **L'orchestrateur.** *Envoyer* crée la mission et démarre l'orchestrateur ; la carte affiche « L'orchestrateur prépare la carte… ». Il lit l'état des modules, peut vous poser une question, puis propose une carte.
3. **La carte.** Ce que Studio construira (un nouveau projet et son dossier, ou l'un des vôtres), ce que Serveurs préparera (DNS, certificats, proxy, protection, pare-feu), les environnements (*dev*, *staging*, *prod* : adresse, protection, serveurs) et les entrées du coffre que chacun reçoit. Athanor vérifie la carte avant de l'afficher : un projet existant ou un nom neuf valide, des serveurs de votre inventaire, des adresses qu'aucun autre environnement n'utilise, des entrées du coffre qui existent. Une entrée que le coffre n'a pas s'affiche *à remplir* ; l'orchestrateur n'en crée jamais.

Ce que la carte montre ensuite est **calculé par Athanor, pas par l'orchestrateur** :

| Partie | Comment |
|---|---|
| *Je vous demanderai votre accord pour* | la production toujours ; puis selon la politique ci-dessous ; le durcissement d'un serveur pas encore durci ; le déverrouillage du coffre quand une entrée *infra* est utilisée |
| *Ce qui sera touché* | les serveurs, les noms DNS et leurs zones, les fichiers `/etc/caddy/sites/<hôte>.caddy` et `/srv/athanor/<projet>-<env>/`, le dossier du projet |
| *Estimation* | les jalons prévus par l'orchestrateur × la durée moyenne d'un cycle et le quota moyen des projets de ce PC (25 minutes et « quota inconnu » avant un premier cycle mesuré), affichée en fourchette |

## La politique

| Choix | Déploiements *dev* et *staging* | Plans Serveurs | *prod* |
|---|---|---|---|
| *Me demander pour la prod* (par défaut) | automatiques quand rien n'est détruit | vous attendent, sauf votre *Ne plus demander* pour ce serveur | vous attend |
| *Me demander pour tout* | vous attendent (le lancement retire vos *Ne plus demander* pour eux) | chaque plan vous attend, même sous *Ne plus demander* | vous attend |
| *Autonome* | automatiques quand rien n'est détruit | vous attendent, sauf votre *Ne plus demander* pour ce serveur | vous attend |

Un déploiement en *prod* attend votre accord quel que soit le choix, de même que les étapes destructrices sur un serveur et tout ce qui touche un serveur *prod* (voir les garde-fous de l'agent Serveurs dans le README). La liste *Je vous demanderai votre accord pour* de la carte est calculée avec ces mêmes règles, vos *Ne plus demander* compris : ce qu'elle annonce est ce qui vous sera demandé.

Si la session de l'orchestrateur échoue après vous avoir montré une carte, la carte reste et peut toujours être lancée ; l'erreur va à l'historique de la mission. Tant que trois missions sont en préparation, une quatrième attend : renvoyez-la quand une carte est prête. Envoyer deux fois la même phrase pendant sa préparation donne la même mission.

## Modifier, lancer, annuler

- **Modifier.** Décochez une entrée du coffre ou changez la politique : la carte est recalculée aussitôt, sans agent. Écrivez un changement (« mets la dev sur le serveur de la maison ») : l'orchestrateur reçoit la carte et vos mots et en propose une nouvelle.
- **Lancer.** Une seule fois, sous verrou, dans cet ordre : (1) la voie Studio : un nouveau projet est créé (mêmes vérifications que *Nouveau projet*) avec le brief Studio comme demande, ou le brief est ajouté à la file d'un projet existant ; son pilote démarre ; (2) les entrées cochées sont données (entrées *code* au projet, *infra* à l'environnement) ; (3) les environnements sont posés (*dev* et *staging* avec la politique de la mission, *prod* toujours sur accord ; *dev* se déploie après chaque jalon) ; (4) la voie Serveurs : une tâche par environnement à préparer, dont les plans attendent votre accord comme d'habitude. Une étape qui échoue arrête les suivantes, sonne la cloche et s'inscrit à l'historique de la mission ; ce qui est déjà fait reste fait. Si une entrée *infra* est utilisée, le coffre doit d'abord être déverrouillé, et en mode Windows natif elle est refusée (la carte le dit : décochez-la, passez à WSL, ou *Forcez* un projet existant sur sa page Accès). Sur l'ordinateur, *Lancer* demande le mot de passe d'Athanor, puisqu'il donne des clés. Le dossier du nouveau projet est vérifié avant que rien ne démarre (chemin complet local, parent inscriptible, pas déjà pris). Les environnements d'un projet existant ne sont jamais affaiblis : leur politique reste la plus stricte entre la leur et celle de la mission, et leur protection et leurs serveurs restent tels quels sauf si vous les changez vous-même ; la carte montre « Avant : » sous chaque environnement que la mission change. Depuis un téléphone, les environnements et le projet se changent sur l'ordinateur seulement.
- **Annuler.** Avant le lancement, *Annuler la mission* l'abandonne : rien n'a été construit ni déployé.

La page *Missions* liste chaque mission avec son état et ses voies (lues en direct sur le pilote du projet et la tâche Serveurs) ; la page d'une mission montre sa carte et les liens vers son projet et sa tâche. Les trois dernières sont sur la page d'accueil.

## Depuis votre téléphone

Un téléphone appairé et déverrouillé peut lire une mission, répondre à l'orchestrateur, modifier, lancer et annuler. Quand l'orchestrateur vous demande de lancer, « Lancer la mission … ? » arrive dans la cloche, sur *Pour vous* et en notification : elle montre les environnements de la carte (prod comprise) et les accès cochés, et son lien ouvre la mission. Accepter la lance exactement comme *Lancer*, et seulement la carte pour laquelle elle a été demandée : tant qu'elle attend, ni l'orchestrateur ni *Modifier* ne changent la carte, et une carte changée malgré tout refuse la demande (relancez depuis la carte). Sur l'ordinateur, accepter demande le mot de passe d'Athanor. Un tel accord n'est jamais « toujours ». Les cases que vous décochez le restent quand l'orchestrateur propose une nouvelle carte. Les questions de l'orchestrateur arrivent aussi dans la cloche, avec leurs choix en boutons ; votre réponse le relance là où il s'était arrêté.

## Ce que l'orchestrateur peut et ne peut pas faire

L'orchestrateur est une courte session Claude lancée par Athanor : un dossier de travail vide, aucun outil intégré (`--tools ""`), seulement le serveur MCP d'Athanor, aucun secret dans son environnement, un jeton valable pour cette mission seulement et révoqué à son arrêt, dix minutes au plus. Ses outils :

| Outil | Ce qu'il fait |
|---|---|
| `modules_etat` | un résumé des modules : projets avec leur état et leur plan en une ligne, serveurs avec leur nom, leur état et leur durcissement, environnements avec leur adresse et leur protection, noms et types des entrées du coffre — jamais une valeur ni un fichier |
| `proposer_mission` | remet la carte ; Athanor la vérifie et calcule le reste, ou répond ce qu'il faut corriger |
| `demander_module` | une question en lecture seule à un module, sur liste blanche (aujourd'hui : l'état d'un projet) |
| `question_proprietaire` | vous pose une question avec 4 choix au plus ; une seule question ouverte à la fois |
| `lancer_mission` | vous demande de lancer (l'approbation ci-dessus) ; il ne lance jamais lui-même |

Tout autre outil — ceux de Studio, de l'agent Serveurs, de Cloudflare, les fichiers, SSH — est refusé à son jeton, et chaque appel est écrit au journal d'audit. Il ne peut ni lire les fichiers d'un projet, ni joindre un serveur, ni voir une clé, ni construire, ni déployer : une fois la mission lancée, le travail est fait par le pilote du projet et l'agent Serveurs, sous leurs règles habituelles.

## Où c'est gardé

Un fichier par mission dans le dossier d'Athanor, `hub/missions/<id>.json` (lisible par votre compte seulement) : la phrase, les mentions, la carte, la politique, les voies, les questions et l'historique. L'atelier de l'orchestrateur (`hub/missions/<id>-session/`) est supprimé à la fin de sa session.
