# Serveur SSH de test d'Athanor (étape 5) : jamais un vrai serveur.
# Utilisateur athanor (sudo sans mot de passe), connexion par mot de passe permise au départ, ufw, faux docker qui journalise.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends openssh-server sudo ufw iproute2 procps \
 && rm -rf /var/lib/apt/lists/* \
 && useradd -m -s /bin/bash athanor && echo 'athanor:essai-athanor' | chpasswd \
 && echo 'athanor ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/athanor && chmod 440 /etc/sudoers.d/athanor \
 && mkdir -p /run/sshd /etc/ssh/sshd_config.d \
 && printf 'PasswordAuthentication yes\nKbdInteractiveAuthentication no\n' > /etc/ssh/sshd_config.d/10-essai.conf \
 && printf '#!/bin/sh\necho "docker $*" >> /var/log/faux-docker.log\n' > /usr/local/bin/docker && chmod 755 /usr/local/bin/docker \
 && touch /var/log/faux-docker.log && chmod 666 /var/log/faux-docker.log
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D", "-e"]
