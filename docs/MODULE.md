# Athanor as a module without a window (a server, a VPS)

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

## Example systemd unit

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
