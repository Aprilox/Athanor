# Athanor as a module without a window — Athanor en module sans fenêtre

[English](#english) · [Français](#français)

## English

On a server, Athanor can run as its **Servers** module only: no window, no terminal interface, its API on
`127.0.0.1` alone, and the remote access tunnel started so that your hub (the Athanor on your computer) can pair with it.

```sh
athanor module servers --nom vps-paris     # runs until stopped (Ctrl+C, or systemd)
athanor module appairer                    # in a second terminal: shows the 6-digit code
```

On your hub: *Settings › Remote Athanors › Add*, with the module's remote access address and the code. The
module's terminal then asks `Accept? [y/N]`: type `y`. The hub sees the module's servers, their tasks, and can
ask the Servers module for a server's sheet or its refresh. A refresh on a **prod** server waits for an approval
of the module's owner (a phone paired with the module), never decided by the hub.

### Example systemd unit

`/etc/systemd/system/athanor-module.service` (replace `deploy` and the path to the executable):

```ini
[Unit]
Description=Athanor (Servers module, no window)
After=network-online.target
Wants=network-online.target

[Service]
User=deploy
ExecStart=/home/deploy/athanor/athanor module servers --nom %H
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload && sudo systemctl enable --now athanor-module
sudo -u deploy /home/deploy/athanor/athanor module appairer   # once, to pair your hub
```

## Français

Sur un serveur, Athanor peut tourner comme son seul module **Serveurs** : pas de fenêtre, pas d'interface en
terminal, son API sur `127.0.0.1` seulement, et le tunnel d'accès à distance démarré pour que votre hub (l'Athanor de
votre ordinateur) puisse s'appairer avec lui.

```sh
athanor module servers --nom vps-paris     # tourne jusqu'à l'arrêt (Ctrl+C, ou systemd)
athanor module appairer                    # dans un second terminal : affiche le code à 6 chiffres
```

Sur votre hub : *Réglages › Athanor distants › Ajouter*, avec l'adresse d'accès à distance du module et le code. Le
terminal du module demande alors `Accept? [y/N]` : tapez `y`. Le hub voit les serveurs du module et leurs tâches, et
peut demander au module Serveurs la fiche d'un serveur ou sa mise à jour. Une mise à jour d'un serveur de **prod**
attend l'accord du propriétaire du module (un téléphone appairé au module), jamais décidé par le hub.

### Exemple d'unité systemd

`/etc/systemd/system/athanor-module.service` (remplacez `deploy` et le chemin de l'exécutable) :

```ini
[Unit]
Description=Athanor (Servers module, no window)
After=network-online.target
Wants=network-online.target

[Service]
User=deploy
ExecStart=/home/deploy/athanor/athanor module servers --nom %H
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```sh
sudo systemctl daemon-reload && sudo systemctl enable --now athanor-module
sudo -u deploy /home/deploy/athanor/athanor module appairer   # une fois, pour appairer votre hub
```
