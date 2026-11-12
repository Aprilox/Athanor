#!/bin/sh
# Faux docker du serveur SSH de test : journalise chaque appel, répond aux versions (la fiche le voit avec compose).
# « compose … up » note aussi son dossier, et échoue quand ce dossier contient un fichier ECHEC (M134).
echo "docker $*" >> /var/log/faux-docker.log
case "$*" in
  --version) echo "Docker version 27.3.1, build essai" ;;
  "compose version"*) echo "2.29.7" ;;
  compose*" up"*) echo "  dans $PWD" >> /var/log/faux-docker.log
    [ -e ECHEC ] && { echo "failed to solve: essai d'échec" >&2; exit 1; } ;;
esac
exit 0
