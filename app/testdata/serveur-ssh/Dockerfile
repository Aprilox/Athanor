# Serveur SSH de test d'Athanor (étapes 5 et 8) : jamais un vrai serveur.
# Utilisateur athanor (sudo sans mot de passe), connexion par mot de passe permise au départ, ufw, faux docker qui journalise,
# git et Caddy (paquet Debian, site par défaut sur :80) démarré sans systemd : `sudo caddy reload --config /etc/caddy/Caddyfile`.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends openssh-server sudo ufw iproute2 procps git caddy \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -m -s /bin/bash athanor && echo 'athanor:essai-athanor' | chpasswd \
 && echo 'athanor ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/athanor && chmod 440 /etc/sudoers.d/athanor \
 && mkdir -p /run/sshd /etc/ssh/sshd_config.d \
 && printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication no\n' > /etc/ssh/sshd_config.d/10-essai.conf \
 && touch /var/log/faux-docker.log && chmod 666 /var/log/faux-docker.log
COPY faux-docker.sh /usr/local/bin/docker
RUN chmod 755 /usr/local/bin/docker
EXPOSE 22 80
# Caddy sous l'utilisateur caddy comme son service systemd (API d'administration sur localhost:2019), puis sshd au premier plan
CMD ["/bin/sh", "-c", "runuser -u caddy -- env HOME=/var/lib/caddy caddy start --config /etc/caddy/Caddyfile --adapter caddyfile >/var/log/caddy-start.log 2>&1; exec /usr/sbin/sshd -D -e"]
