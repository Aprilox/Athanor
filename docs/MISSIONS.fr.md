# Anciennes missions

*English: [MISSIONS.md](MISSIONS.md)*

Avant la version 3.0, une mission était une phrase que Studio et Serveurs réalisaient ensemble. Athanor ne crée plus de missions et n'en suit plus : un projet a maintenant son propre déploiement, réglé dans son onglet *Déploiement* ([README](../README.fr.md#le-déploiement-dun-projet), [DEPLOY.fr.md](DEPLOY.fr.md)). Cette page dit ce que deviennent les missions laissées par une version précédente.

## Ce qui les remplace

- **Une phrase sur la page d'accueil** fait toujours le travail : l'assistant de l'accueil propose une carte **projet** (Studio seul, ou avec son déploiement) ou une carte **serveur**, et rien ne démarre avant *Lancer*.
- **Le projet porte son déploiement** : le serveur, l'adresse, les API du coffre, votre plan et le moment du déploiement sont dans *Studio › Déploiement* ; le mode de permission est dans *Gérer › Réglages de ce projet*.
- **Plus d'orchestrateur** entre vous et le projet : le pilote du projet construit, Athanor prépare le serveur et déploie, avec les mêmes garde-fous qu'avant.

## La page Anciennes missions

*Anciennes missions* n'est dans le menu que tant qu'il en reste une. Elle liste chaque mission avec l'état où elle a été laissée, sa phrase et sa date ; une mission s'ouvre sur son **journal**, en lecture seule (*Voir plus* montre les lignes plus anciennes). Une mission qui n'était pas finie dit *Plus suivie : son projet continue seul* : rien n'est repris, mis en pause ni relancé depuis cette page.

Rien n'a été perdu à la mise à jour, et rien n'est réécrit à la lecture : les fichiers des missions restent tels qu'ils étaient, et aucun mot de passe n'est redemandé.

## Leurs projets

Un projet qu'une mission a créé ou mené est un projet Studio comme un autre : vous le *Démarrez*, vous lui ajoutez des demandes, vous réglez son déploiement.

- Un projet qui avait été réglé avec plusieurs sites les garde : son onglet *Déploiement* en montre et en change un, et *Autres sites de ce projet* liste les autres par leur adresse, chacun avec *En faire le déploiement*.
- Un site qu'une mission a mis en ligne le dit jusqu'à ce que vous en fassiez le déploiement du projet. D'ici là, rien ne s'y déploie tout seul.
- Un site de production de cette époque vous demande encore avant chaque déploiement (« Mise en ligne de example.com ? », dans la cloche, sur *Pour vous* et sur votre téléphone).

## Supprimer une mission

*Supprimer la mission*, sur la liste (la croix) ou sur la page de la mission, montre d'abord ce qui part : la mission, son journal et ses fichiers de suivi. *Retirer aussi le projet « … » de Studio et mettre son dossier à la corbeille* est décochée par défaut, et grisée, avec la raison, pour un dossier qu'Athanor ne peut pas supprimer. Le dépôt GitHub reste, comme ce que la mission a mis en ligne sur les serveurs ; aucune clé, aucun serveur ni aucune entrée du coffre n'est retiré.

## Clients MCP

Pour un client branché sur le serveur MCP d'Athanor ([MCP.fr.md](MCP.fr.md)), `missions` liste les anciennes missions qu'il peut voir et `proposer_mission` répond que les missions ne se créent plus : utilisez `ajouter_demande` pour ajouter une demande à un projet.

## Où elles sont gardées

Un fichier par mission dans le dossier d'Athanor, `hub/missions/<id>.json` (lisible par votre compte seulement) : la phrase, la carte, les voies, l'historique et le journal. Athanor ne fait que lire ces fichiers, et en retire un quand vous supprimez sa mission.
