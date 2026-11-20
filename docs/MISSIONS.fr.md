# Missions

*English: [MISSIONS.md](MISSIONS.md)*

Une mission relie ce qu'Athanor gère : un **projet Studio**, au plus un **serveur** et au plus une **clé API** du coffre, avec un seul choix de permission pour le serveur. Le [README](../README.fr.md#missions) décrit le formulaire et le cadre ; cette page dit de quoi une mission est faite et ce que deviennent les missions des versions précédentes.

## Ce qu'est une mission

- **Un seul formulaire.** *Nouvelle mission* (sur l'accueil ou dans *Missions*), ou l'assistant de l'accueil, qui ouvre le même formulaire déjà rempli d'après votre phrase. Ce qu'il faut faire, le projet (un nom nouveau le crée dans Studio, le nom d'un des vôtres lui ajoute la mission comme prochaine demande), le serveur et son choix (*A besoin de moi* = le mode *Manuel* du projet, *Autonome avec l'IA* = son mode *Auto*), la clé API. Un plan, comment et quand le mettre en ligne, le dossier, les modes de travail, GitHub et les réglages propres du projet sont repliés sous *Plus d'options*. *Lancer* : un clic.
- **Rien de nouveau dessous.** Lancer écrit ce que vous régleriez à la main : le projet dans Studio, son onglet *Déploiement* (serveur, clé, adresse, plan, moment), son mode de permission, puis démarre son pilote. Changez-les dans les onglets du projet et le cadre de la mission le montre ; changez-les dans la mission (*Modifier*) et les onglets du projet le montrent.
- **Son état se lit, il n'est jamais recopié.** Le cadre lit le projet (en cours, en pause, arrêté, terminé, ses jalons) et son déploiement (en préparation, mise en ligne, en ligne, échec et pourquoi) à chaque affichage.
- **Arrêter, reprendre, modifier, supprimer dans tout état.** *Pause* et *Arrêter* sont les arrêts du projet (un arrêt annule aussi ses tâches serveur) ; *Reprendre* redémarre son pilote ; *Modifier* change le serveur, la clé et le choix, et un texte modifié devient une nouvelle demande du projet. *Supprimer* arrête d'abord une mission qui travaille, puis retire la mission ; son projet et son dossier ne partent que si vous le cochez, jamais une clé, un serveur ni une entrée du coffre.
- **Le serveur depuis le cadre.** *Terminal* ouvre le terminal du serveur ; *Arrêter le travail sur le serveur* annule la préparation ou la première mise en ligne en cours ; *Relancer le travail sur le serveur* fait la première mise en ligne, ou redéploie le dernier commit du projet.

## Missions des versions précédentes

Les missions faites par Athanor 3.0.0 et avant apparaissent dans *Missions* en cadres à lire seulement (*Plus suivie*) : une mission s'ouvre sur son **journal**, et *Supprimer* la retire (son projet et son dossier seulement si c'est coché). Rien n'a été perdu à la mise à jour et rien n'est réécrit à la lecture ; leurs projets continuent seuls.

## Clients MCP

Pour un client branché sur le serveur MCP d'Athanor ([MCP.fr.md](MCP.fr.md)), `missions` liste les missions qu'il peut voir et `proposer_mission` répond que seul le propriétaire lance une mission, dans la fenêtre d'Athanor : utilisez `ajouter_demande` pour ajouter une demande à un projet.

## Où elles sont gardées

Un fichier par mission dans le dossier d'Athanor, `hub/missions/<id>.json` (lisible par votre compte seulement) : son texte (une valeur du coffre écrite dedans est masquée), son projet, son serveur et sa clé par identifiant, son choix et son historique. Athanor le retire quand vous supprimez sa mission.
