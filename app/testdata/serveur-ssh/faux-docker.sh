#!/bin/sh
# Faux docker du serveur SSH de test : journalise chaque appel, répond aux versions (la fiche le voit avec compose).
echo "docker $*" >> /var/log/faux-docker.log
case "$*" in
  --version) echo "Docker version 27.3.1, build essai" ;;
  "compose version"*) echo "2.29.7" ;;
esac
exit 0
