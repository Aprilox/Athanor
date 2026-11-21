# A test server for the Servers agent — Un serveur de test pour l'agent Serveurs

[English](#english) · [Français](#français)

## English

Before you let the Servers agent touch a server that matters, try it on one that does not. Three ways, from the cheapest to the most realistic.

### 1. Athanor's own test container (no server needed)

*Settings › Setup & health › Test the Servers agent* builds a small Debian container with Docker Desktop (or Docker on Linux/macOS), adds it as a server with its test password, gives a real Claude session a task (read the sheet, two forbidden attempts that must be refused, one file to write through a plan), accepts the plan for you if it does exactly what was asked, then removes the server and its key. About 5 minutes; a verdict in six lines. Without Docker the test is skipped and says why.

### 2. A virtual machine on your computer

Any Debian 12 or Ubuntu 24.04 machine in Hyper-V, VirtualBox, UTM, Multipass or WSL with `systemd` will do. Install `openssh-server`, `sudo` and `ufw`, create a user with `sudo`, and add the machine with its local address. Take a snapshot first: you can then let the agent break things and go back in one click.

### 3. A small rented VPS

To see the whole path — public address, certificates, the external check — rent the smallest VPS of any host you like (no brand is needed; most have an hourly price and a delete button):

- **Size**: 1 vCPU, 1 to 2 GB of memory, 10 to 25 GB of disk is plenty.
- **System**: Debian 12 or Ubuntu 24.04 LTS, freshly installed.
- **Access**: `root` or a user with `sudo`, SSH on port 22 (or any port you give Athanor). Note the fingerprint shown by the host's console (`for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done`) to compare it when adding.
- **Caution**: never put a real site on it. Its changes can then be accepted once for all (*Never ask again for this server*); a server that carries an older project's production site always waits for you.
- **Nothing of value on it**, and a host snapshot if the host offers one. Delete it when you are done.

Then *Servers › + Add a server*, cut password logins when Athanor offers it, and give a first task such as “Install nginx and serve a test page”. What Athanor does on such a new machine is listed in [MACHINE_NEUVE.md](MACHINE_NEUVE.md).

## Français

Avant de laisser l'agent Serveurs toucher un serveur qui compte, essayez-le sur un qui ne compte pas. Trois façons, de la moins chère à la plus réaliste.

### 1. Le conteneur de test d'Athanor (aucun serveur à fournir)

*Réglages › Installation & santé › Tester l'agent Serveurs* construit un petit conteneur Debian avec Docker Desktop (ou Docker sous Linux et macOS), l'ajoute comme serveur avec son mot de passe d'essai, confie une tâche à une vraie session Claude (lire la fiche, deux tentatives interdites qui doivent être refusées, un fichier à écrire par un plan), accepte le plan pour vous s'il fait exactement ce qui est demandé, puis retire le serveur et sa clé. Environ 5 minutes ; un verdict en six lignes. Sans Docker, le test est sauté et dit pourquoi.

### 2. Une machine virtuelle sur votre ordinateur

N'importe quelle machine Debian 12 ou Ubuntu 24.04 dans Hyper-V, VirtualBox, UTM, Multipass ou WSL avec `systemd` convient. Installez `openssh-server`, `sudo` et `ufw`, créez un utilisateur qui a `sudo`, et ajoutez la machine avec son adresse locale. Prenez d'abord un instantané : vous pourrez laisser l'agent casser des choses et revenir en un clic.

### 3. Un petit VPS loué

Pour voir tout le chemin — adresse publique, certificats, vérification externe — louez le plus petit VPS de l'hébergeur de votre choix (aucune marque n'est nécessaire ; la plupart facturent à l'heure et se suppriment d'un bouton) :

- **Taille** : 1 vCPU, 1 à 2 Go de mémoire, 10 à 25 Go de disque suffisent largement.
- **Système** : Debian 12 ou Ubuntu 24.04 LTS, fraîchement installé.
- **Accès** : `root` ou un utilisateur qui a `sudo`, SSH sur le port 22 (ou celui que vous donnez à Athanor). Notez l'empreinte affichée par la console de l'hébergeur (`for f in /etc/ssh/ssh_host_*_key.pub; do ssh-keygen -lf $f; done`) pour la comparer à l'ajout.
- **Prudence** : n'y mettez jamais un vrai site. Ses changements peuvent alors être acceptés une fois pour toutes (*Ne plus demander pour ce serveur*) ; un serveur qui porte le site de production d'un ancien projet vous attend toujours.
- **Rien de précieux dessus**, et un instantané chez l'hébergeur s'il en propose. Supprimez-le une fois l'essai fini.

Puis *Serveurs › + Ajouter un serveur*, coupez la connexion par mot de passe quand Athanor le propose, et donnez une première tâche comme « Installer nginx et servir une page de test ». Ce qu'Athanor fait sur une telle machine neuve est listé dans [MACHINE_NEUVE.md](MACHINE_NEUVE.md).
