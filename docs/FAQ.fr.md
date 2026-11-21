# FAQ et dépannage

<sub>Fait partie du [README d'Athanor Hub](../README.fr.md) · [English](FAQ.md)</sub>

## FAQ

<details>
<summary><b>Combien ça coûte ?</b></summary>

Seulement votre abonnement Claude : Athanor enchaîne des sessions Claude Code, rien d'autre. Les modèles les plus puissants consomment le quota le plus vite ; Sonnet dure plus longtemps. `FALLBACK_MODEL` prend le relais quand le quota du modèle principal est épuisé, et `CHEAP_MODEL` s'occupe du travail mécanique. La page *Suivi* montre le coût de chaque cycle au prix catalogue, c'est-à-dire ce que paie le quota de votre abonnement. Avec une clé API à la place (*Réglages → Claude → Connexion*), chaque passage est facturé à ce prix au compte API : *Suivi* affiche alors la dépense face au budget que vous avez fixé, et le pilote attend une fois qu'il est dépensé.
</details>

<details>
<summary><b>Que peut-il construire ?</b></summary>

Tout ce que Claude Code peut construire et vérifier seul dans un dossier : sites et applications web, outils de bureau et en ligne de commande, jeux (Unreal Engine, Godot, web), applications Android, bibliothèques. Les projets qui ont besoin de vos logiciels Windows passent par les passerelles. Les vérifications sont écrites par Claude pour chaque projet (`check.sh`), avec des captures pour tout ce qui est visuel.
</details>

<details>
<summary><b>Puis-je le guider pendant qu'il travaille ?</b></summary>

Oui. Une demande ajoutée avec *Demandes → Tout de suite* est lue au début du cycle suivant. Les nouvelles demandes vont dans la file et se réordonnent. *Arrêter › Terminer le jalon en cours* l'achève, l'enregistre, puis s'arrête : rien n'est perdu. *Mettre en pause* arrête le pilote tout de suite et fait attendre le déploiement du projet ; *Arrêter immédiatement* coupe le cycle et annule les tâches serveur de son déploiement.
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

Oui. Rien n'est lié à un nom d'utilisateur ni à une lettre de disque : les chemins viennent de votre profil et les logiciels sont détectés sur tous les disques. Téléchargez Athanor sur chaque PC et lancez-y *Tout installer*. Pour continuer le même projet depuis un autre PC : liez-le à GitHub avec *Partager l'atelier (.athanor/) sur GitHub* ; sur l'autre PC, connectez-vous à GitHub, créez un projet dans un dossier vide lié au même dépôt (*Utiliser un de mes dépôts*) et *Récupérer le travail de GitHub* (*Démarrer* le propose aussi) : le plan suit et le pilote le reprend là où il s'était arrêté. Les deux en même temps : [Plusieurs ordinateurs](STUDIO.fr.md#plusieurs-ordinateurs).
</details>

<details>
<summary><b>Ai-je besoin d'un serveur ou de Cloudflare ?</b></summary>

Non. Studio fonctionne seul, comme Athanor l'a toujours fait : les autres modules attendent que vous ajoutiez un serveur ou un jeton Cloudflare. Un projet sans serveur dans son onglet *Déploiement* est seulement construit.
</details>

<details>
<summary><b>Claude peut-il voir les clés de mes serveurs ou déployer en production sans moi ?</b></summary>

Non. La clé d'un serveur est une entrée *infra* du coffre : elle ne quitte jamais le processus d'Athanor, et un agent ne fait qu'appeler des outils qu'Athanor exécute avec elle. Les étapes destructrices attendent toujours votre accord, avec le mot de passe d'Athanor retapé (un téléphone appairé et déverrouillé peut aussi accepter), comme les déploiements du site de production d'un ancien projet. Seule exception : un projet que vous avez vous-même mis en *Ignorer les permissions*, dans la fenêtre d'Athanor avec son mot de passe : il agit sans demander, et le journal d'audit le dit. Voir le [modèle de sécurité](SECURITY.fr.md#modèle-de-sécurité).
</details>

<details>
<summary><b>Et si j'oublie le mot de passe d'Athanor ?</b></summary>

Les clés des projets se rouvrent toujours sur ce PC grâce au coffre du système. Les clés *infra* (celles de vos serveurs, de Cloudflare) sont enveloppées par le mot de passe seul : elles ne peuvent donc pas être récupérées, ajoutez-les de nouveau (la clé d'un serveur peut être refaite par Athanor et posée avec le mot de passe du serveur). Rien d'autre n'est perdu : les projets, les réglages et les fiches des serveurs restent.
</details>

<details>
<summary><b>J'utilisais Athanor avant le hub. Qu'est-ce qui change pour mes projets ?</b></summary>

Rien que vous ayez à faire. La mise à jour ne déplace rien : projets, réglages, file des demandes, équipe, accès à distance et anciens liens restent où ils étaient. Au premier démarrage, les clés du `.athanor/secrets.env` de chaque projet, de *Skills & MCP* et du `.env` d'Athanor sont copiées dans le coffre (ces fichiers restent tels quels), et chaque clé de projet est accordée de nouveau à son projet : les pilotes reçoivent donc exactement ce qu'ils avaient. *Annuler l'import* retire ce qui a été ajouté.
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
| « Le coffre est verrouillé » sur une action de Serveurs ou un déploiement | déverrouillez Athanor (son mot de passe, dans la fenêtre ou sur un téléphone appairé) : la clé *infra* ne s'ouvre qu'avec lui |
| Un serveur refusé avec « la clé d'hôte a changé » | le serveur a été réinstallé, ou quelqu'un s'est glissé entre les deux : vérifiez la nouvelle empreinte sur le serveur lui-même (`for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done`), puis retirez et ajoutez de nouveau le serveur |
