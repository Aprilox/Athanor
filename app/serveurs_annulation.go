package main

// Automatic rollback (M93): before a pare_feu allow/deny/delete or a write under /etc/ssh/, the state before is
// kept on the server and a rollback is scheduled there (systemd-run --on-active, else a detached nohup sh -c);
// Athanor then opens a new connection to see the server still answers. confirmer_changement (agent) or Keep this
// change (server page) cancels the rollback; without it, the server puts the state back by itself when the delay
// ends, even if Athanor is gone.

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// delaiAnnulation: how long a change waits for its confirmation (tests shorten it).
var delaiAnnulation = 5 * time.Minute

// margeVerifRetour (C49): how long after the deadline Athanor reads what the server's rollback did (tests shorten it).
var margeVerifRetour = 30 * time.Second

var reIDAnnulation = regexp.MustCompile(`^[0-9a-f]{8}$`)

// AnnulationServeur: a change that waits for its confirmation (shown on the server page with its countdown).
type AnnulationServeur struct {
	ID       string    `json:"id"`
	Serveur  string    `json:"serveur"`
	Quoi     string    `json:"quoi"`
	Echeance time.Time `json:"echeance"`
	genre    string    // ufw or ssh
}

// RetourNonVerifie (C49): a rollback Athanor could not see done; the bell, with the command to check it by hand.
type RetourNonVerifie struct{ ID, Serveur, Quoi, Pourquoi, Verifier string }

// retoursNonVerifies: the bell's rollbacks to check, by id.
// shortcut: in memory, like the pending changes; after a restart of Athanor only the audit log keeps them.
var retoursNonVerifies = struct {
	sync.Mutex
	m map[string]RetourNonVerifie
}{m: map[string]RetourNonVerifie{}}

// annulations: the pending ones, in memory (after a restart of Athanor the server still undoes them by itself).
var annulations = struct {
	sync.Mutex
	m map[string]AnnulationServeur
}{m: map[string]AnnulationServeur{}}

// annulationsDe: the pending changes of server id, soonest first.
func annulationsDe(id string) []AnnulationServeur {
	annulations.Lock()
	defer annulations.Unlock()
	l := []AnnulationServeur{}
	for _, a := range annulations.m {
		if a.Serveur == id {
			l = append(l, a)
		}
	}
	slices.SortFunc(l, func(a, b AnnulationServeur) int { return a.Echeance.Compare(b.Echeance) })
	return l
}

// comme root: script run by sh as root (directly, or through sudo -n), with its arguments quoted.
func commeRoot(script string, args ...string) string {
	return scriptShell(false, preludeRoot+`R=$1; shift; exec $S sh -c "$R" sh "$@"`, append([]string{script}, args...)...)
}

const preludeAnnulation = `export LC_ALL=C PATH="$PATH:/usr/sbin:/sbin:/usr/local/sbin"; umask 077
`

// scriptRetour ($1 the saved state's folder): takes the folder (a confirmation that took it first wins), puts the
// state back, logs to <folder>.log; a step that fails is logged as ROLLBACK FAILED and exits 6 with the log (C49).
const scriptRetour = preludeAnnulation + `D=$1; mv -- "$D" "$D.run" 2>/dev/null || exit 0; D=$D.run; e=0
{ case $(cat "$D/type") in
  ufw) for f in user.rules user6.rules; do [ ! -e "$D/$f" ] || cp -p "$D/$f" /etc/ufw/ || e=1; done
    if ufw status | grep -q '^Status: active'; then ufw reload || e=1; fi ;;
  ssh) p=$(cat "$D/chemin"); if [ -e "$D/prev" ]; then cp -p -- "$D/prev" "$p"; else rm -f -- "$p"; fi || e=1
    if ` + rechargerSSHD + `; then echo "sshd reloaded"; else e=1; fi ;;
esac; if [ $e = 0 ]; then echo "rolled back $(date)"; else echo "ROLLBACK FAILED $(date)"; fi; } > "${D%.run}.log" 2>&1
rm -rf -- "$D"; [ $e = 0 ] || { cat -- "${D%.run}.log"; exit 6; }`

// scriptLireRetour ($1 id): what the server's rollback logged (exit 7: it has not run, or left no trace).
const scriptLireRetour = preludeAnnulation + `d=` + dossierSauvegardes + `
if [ -f "$d/annul-$1.log" ]; then cat -- "$d/annul-$1.log"
elif [ -e "$d/annul-$1" ] || [ -e "$d/annul-$1.run" ]; then echo "the rollback has not run (the server restarted?)"; exit 7
else echo "the server kept no trace of the rollback"; exit 7; fi`

const rechargerSSHD = `{ systemctl reload ssh || systemctl reload sshd || rc-service sshd reload || kill -HUP "$(cat /run/sshd.pid || cat /var/run/sshd.pid)"; }`

// scriptVerifierSSHD (C48): after a change under /etc/ssh/, sshd -t then a reload, inside the rollback's window, so
// the new connection tests the new configuration (exit 4: refused by sshd -t, 5: not reloaded).
const scriptVerifierSSHD = preludeAnnulation + `command -v sshd >/dev/null 2>&1 || { echo "no sshd here: nothing tested nor reloaded"; exit 0; }
sshd -t 2>&1 || exit 4
` + rechargerSSHD + ` >/dev/null 2>&1 || { echo "sshd could not be reloaded"; exit 5; }
echo "sshd -t ok, sshd reloaded"`

// reRechargeSSH: a command that reloads or restarts sshd (it applies whatever /etc/ssh/ holds).
// shortcut: the usual systemctl and service forms only; rc-service or kill -HUP stay without a rollback.
var reRechargeSSH = regexp.MustCompile(`(^|[\s;&|(])(systemctl\s+(-\S+\s+)*(reload|restart|reload-or-restart|try-restart|try-reload-or-restart)\s+(\S+\s+)*sshd?(\.service)?|service\s+sshd?\s+(reload|restart|force-reload))($|[\s;&|)])`)

// scriptProgrammer ($1 id, $2 ufw|ssh, $3 delay in s, $4 the ssh path, $5 scriptRetour): one pending change of
// a kind at a time; the state saved, then the rollback scheduled; any failure leaves nothing and exits non-zero.
const scriptProgrammer = preludeAnnulation + `d=` + dossierSauvegardes + `; D="$d/annul-$1"
for x in "$d"/annul-*/type; do [ -f "$x" ] && [ "$(cat "$x")" = "$2" ] && { echo "a change of the same kind still waits for its confirmation (${x%/type}): confirm it or let it be undone first"; exit 3; }; done
mkdir -p -m 700 "$d" && mkdir -m 700 "$D" || exit 1
{ echo "$2" > "$D/type" && case $2 in
  ufw) for f in user.rules user6.rules; do [ ! -e "/etc/ufw/$f" ] || cp -p "/etc/ufw/$f" "$D/" || { rm -rf -- "$D"; exit 1; }; done ;;
  ssh) p=$(readlink -f -- "$4") && [ -n "$p" ] || p=$4; printf '%s\n' "$p" > "$D/chemin" && { [ ! -e "$p" ] || cp -p -- "$p" "$D/prev"; } ;;
esac && if command -v systemd-run >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
  systemd-run --quiet --unit="athanor-annul-$1" --on-active="$3" /bin/sh -c "$5" sh "$D"
else
  N=; command -v setsid >/dev/null 2>&1 && N=setsid
  $N nohup sh -c 'sleep "$1"; sh -c "$2" sh "$3"' sh "$3" "$5" "$D" </dev/null >/dev/null 2>&1 &
  echo $! > "$D/pid"
fi; } || { rm -rf -- "$D"; exit 1; }
echo ok`

// scriptFinir ($1 id, $2 garder|annuler, $3 scriptRetour): the timer stopped, then the change kept (exit 3: already
// undone) or undone now.
const scriptFinir = preludeAnnulation + `D=` + dossierSauvegardes + `/annul-$1
systemctl stop "athanor-annul-$1.timer" 2>/dev/null; [ -f "$D/pid" ] && kill "$(cat "$D/pid")" 2>/dev/null
case $2 in
  garder) mv -- "$D" "$D.ok" 2>/dev/null || exit 3; rm -rf -- "$D.ok" ;;
  *) [ -d "$D" ] || exit 3; sh -c "$3" sh "$D" ;;
esac`

// sousSSH: p is /etc/ssh or under it.
func sousSSH(p string) bool {
	p = path.Clean(p)
	return p == "/etc/ssh" || strings.HasPrefix(p, "/etc/ssh/")
}

// genreAnnulation: the rollback a call needs ("ufw", or "ssh" with the path written), "" for none; a write is
// checked as given and as the server resolves it (a link into /etc/ssh/).
func genreAnnulation(ctx context.Context, c *ssh.Client, srv ServeurInventaire, outil string, a argsOutilServeur) (string, string) {
	p := a.Chemin
	switch outil {
	case "pare_feu": // refused arguments schedule nothing: the tool refuses them before reaching the server
		if spec, err := specPareFeu(srv, a); err == nil && spec != "" {
			return "ufw", ""
		}
		return "", ""
	case "ssh_commande", "service": // a reload applies /etc/ssh/: sshd_config is kept, put back and sshd reloaded
		cmd := a.Commande
		if outil == "service" {
			cmd = "systemctl " + a.Action + " " + a.Nom
		}
		if reRechargeSSH.MatchString(cmd) {
			return "ssh", "/etc/ssh/sshd_config"
		}
		return "", ""
	case "sftp_envoyer":
		p = a.Distant
	case "fichier_ecrire":
	default:
		return "", ""
	}
	if sousSSH(p) {
		return "ssh", p
	}
	if r, err := resoudreDistant(ctx, c, a.Sudo, "chemin", p, false); err == nil && sousSSH(r) {
		return "ssh", r
	}
	return "", ""
}

// programmerAnnulation keeps the state on the server and schedules its rollback; an error: the change must not run.
func programmerAnnulation(ctx context.Context, c *ssh.Client, srv ServeurInventaire, outil, genre, chemin string, a argsOutilServeur) (AnnulationServeur, error) {
	an := AnnulationServeur{ID: aleatoire(4), Serveur: srv.ID, Echeance: time.Now().Add(delaiAnnulation), genre: genre}
	switch {
	case genre == "ufw":
		an.Quoi = strings.Join(strings.Fields(fmt.Sprintf("pare_feu %s %s %d %s", a.Action, a.Regle, a.Port, a.Proto)), " ")
	case outil == "ssh_commande":
		an.Quoi = "ssh_commande " + couper(a.Commande, 120)
	case outil == "service":
		an.Quoi = "service " + a.Action + " " + a.Nom
	default:
		an.Quoi = outil + " " + chemin
	}
	s := strconv.Itoa(max(1, int(delaiAnnulation.Seconds()))) + "s"
	r, err := executerSSH(ctx, c, commeRoot(scriptProgrammer, an.ID, genre, s, chemin, scriptRetour), nil, delaiCommande)
	if err == nil && (r.Code != 0 || strings.TrimSpace(string(r.Sortie)) != "ok") {
		err = errors.New(couper(strings.TrimSpace(string(r.Sortie)), 500))
	}
	if err != nil {
		return an, fmt.Errorf("the automatic rollback could not be scheduled, the change did not run: %w", err)
	}
	return an, nil
}

// apresChangement: a failed change is undone at once; else Athanor checks the server by a new connection, and the
// change waits for its confirmation.
func (s *Serveur) apresChangement(ctx context.Context, c *ssh.Client, srv ServeurInventaire, de string, an AnnulationServeur, r ResultatOutil, err error) ResultatOutil {
	audit := func(res string) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + srv.ID, Action: "serveurs.annulation", Resultat: an.ID + " · " + an.Quoi + " · " + res})
	}
	if err != nil || r.Code != 0 {
		r.Sortie += "\n(the change failed; " + s.defaireMaintenant(ctx, c, srv, an, audit, "échec du changement") + ")"
		return r
	}
	if an.genre == "ssh" {
		x, e := executerSSH(ctx, c, commeRoot(scriptVerifierSSHD), nil, delaiCommande)
		if e != nil || x.Code != 0 {
			pourquoi := "sshd could not be tested or reloaded"
			if e == nil && x.Code == 4 {
				pourquoi = "sshd -t refused the new configuration"
			}
			if e == nil {
				pourquoi += ": " + couper(strings.TrimSpace(string(x.Sortie)), 500)
			}
			r.Code = cmp.Or(r.Code, 1)
			r.Sortie += "\n(" + pourquoi + "; " + s.defaireMaintenant(ctx, c, srv, an, audit, pourquoi) + ")"
			return r
		}
		r.Sortie += "\n" + strings.TrimSpace(string(x.Sortie))
	}
	nouvelle := ""
	c2, e := s.connecterServeur(ctx, srv)
	if e == nil {
		var x ResultatSSH
		x, e = executerSSH(ctx, c2, "true", nil, delaiCommande)
		if e == nil && x.Code != 0 {
			e = fmt.Errorf("code %d", x.Code)
		}
		c2.Close()
	}
	if e != nil {
		nouvelle = fmt.Sprintf("\nAthanor could not open a new connection to the server after this change (%s): do not confirm it; the server undoes it by itself at %s.", s.masquerSecrets(e.Error()), an.Echeance.Format("15:04:05"))
	} else {
		nouvelle = fmt.Sprintf("\nA new connection to the server works. This change is undone automatically at %s (in %s) unless it is confirmed: check what it should do, then call confirmer_changement with serveur %q and id %q.",
			an.Echeance.Format("15:04:05"), delaiAnnulation, srv.ID, an.ID)
	}
	annulations.Lock()
	annulations.m[an.ID] = an
	annulations.Unlock()
	time.AfterFunc(time.Until(an.Echeance), func() {
		annulations.Lock()
		_, encore := annulations.m[an.ID]
		delete(annulations.m, an.ID)
		annulations.Unlock()
		if encore {
			time.AfterFunc(margeVerifRetour, func() { s.verifierRetour(srv, an, audit, "non confirmé") })
		}
	})
	audit(fmt.Sprintf("retour arrière programmé, échéance %s (nouvelle connexion : %v)", an.Echeance.Format("15:04:05"), e == nil))
	r.Annulation = an.ID
	r.Sortie += nouvelle
	return r
}

// defaireMaintenant puts the state before back at once and says how it went (audit, and the bell if it failed); when
// the server cannot do it now, its own rollback at the deadline is checked afterwards.
func (s *Serveur) defaireMaintenant(ctx context.Context, c *ssh.Client, srv ServeurInventaire, an AnnulationServeur, audit func(string), cause string) string {
	x, e := executerSSH(ctx, c, commeRoot(scriptFinir, an.ID, "annuler", scriptRetour), nil, delaiCommande)
	switch {
	case e == nil && x.Code == 0:
		audit(cause + " : état d'avant remis")
		if an.genre == "ssh" {
			return "the state before was put back and sshd reloaded"
		}
		return "the state before was put back"
	case e == nil && x.Code == 6:
		pourquoi := "the rollback failed: " + couper(s.masquerSecrets(strings.TrimSpace(string(x.Sortie))), 300)
		audit(cause + " : " + pourquoi)
		s.noterRetourNonVerifie(srv, an, pourquoi)
		return pourquoi + "; check the server"
	}
	audit(cause + " : retour arrière à l'échéance")
	time.AfterFunc(time.Until(an.Echeance)+margeVerifRetour, func() { s.verifierRetour(srv, an, audit, cause) })
	return "the state before will be put back by the server at " + an.Echeance.Format("15:04:05")
}

// verifierRetour (C49): after the deadline, a new connection reads the server's annul-<id>.log; the audit log says
// what really happened, and a failed, missing or unreadable rollback rings the bell.
func (s *Serveur) verifierRetour(srv ServeurInventaire, an AnnulationServeur, audit func(string), cause string) {
	ctx, fin := context.WithTimeout(context.Background(), 2*time.Minute)
	defer fin()
	pourquoi := ""
	if c, err := s.connecterServeur(ctx, srv); err != nil {
		pourquoi = "server unreachable: " + s.masquerSecrets(err.Error())
	} else {
		x, e := executerSSH(ctx, c, commeRoot(scriptLireRetour, an.ID), nil, delaiCommande)
		c.Close()
		log := couper(s.masquerSecrets(strings.TrimSpace(string(x.Sortie))), 300)
		switch {
		case e != nil:
			pourquoi = "the server's log could not be read: " + s.masquerSecrets(e.Error())
		case x.Code != 0 || strings.Contains(log, "ROLLBACK FAILED") || !strings.Contains(log, "rolled back "):
			pourquoi = "the rollback failed: " + log
		}
	}
	if pourquoi == "" {
		audit(cause + " : remis par le serveur (vérifié)")
		return
	}
	audit(cause + " : retour arrière non vérifié : " + pourquoi)
	s.noterRetourNonVerifie(srv, an, pourquoi)
}

// noterRetourNonVerifie: the bell for a rollback to check, with the command that shows what the server did.
func (s *Serveur) noterRetourNonVerifie(srv ServeurInventaire, an AnnulationServeur, pourquoi string) {
	v := "sudo cat " + dossierSauvegardes + "/annul-" + an.ID + ".log; sudo ufw status verbose"
	if an.genre == "ssh" {
		v = "sudo cat " + dossierSauvegardes + "/annul-" + an.ID + ".log; sudo sshd -t"
	}
	retoursNonVerifies.Lock()
	retoursNonVerifies.m[an.ID] = RetourNonVerifie{ID: an.ID, Serveur: srv.ID, Quoi: an.Quoi, Pourquoi: pourquoi, Verifier: v}
	retoursNonVerifies.Unlock()
}

// retoursNonVerifiesDe: the rollbacks to check on server id.
func retoursNonVerifiesDe(id string) []RetourNonVerifie {
	retoursNonVerifies.Lock()
	defer retoursNonVerifies.Unlock()
	l := []RetourNonVerifie{}
	for _, r := range retoursNonVerifies.m {
		if r.Serveur == id {
			l = append(l, r)
		}
	}
	slices.SortFunc(l, func(a, b RetourNonVerifie) int { return strings.Compare(a.ID, b.ID) })
	return l
}

// confirmerChangement keeps change idAn of server id: its rollback is cancelled on the server, by a new connection.
func (s *Serveur) confirmerChangement(ctx context.Context, de, id, idAn string) (string, error) {
	if !reIDAnnulation.MatchString(idAn) {
		return "", errors.New("id must be the change's id given by the tool that made it (8 hexadecimal characters)")
	}
	l, err := lireServeurs()
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		return "", errServeurInconnu
	}
	c, err := s.connecterServeur(ctx, l[i])
	if err != nil {
		return "", fmt.Errorf("Athanor cannot reach the server: the change will be undone at its deadline (%w)", err)
	}
	defer c.Close()
	r, err := executerSSH(ctx, c, commeRoot(scriptFinir, idAn, "garder", scriptRetour), nil, delaiCommande)
	annulations.Lock()
	an, connu := annulations.m[idAn]
	if err == nil && (r.Code == 0 || r.Code == 3) {
		delete(annulations.m, idAn)
	}
	annulations.Unlock()
	if !connu {
		an = AnnulationServeur{ID: idAn}
	}
	audit := func(res string) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + id, Action: "serveurs.confirmer_changement", Resultat: idAn + " · " + an.Quoi + " · " + res})
	}
	switch {
	case err != nil:
		audit("erreur : " + couper(s.masquerSecrets(err.Error()), auditMaxOutil))
		return "", err
	case r.Code == 3:
		audit("déjà remis (aucun changement en attente sous cet id)")
		return "", errors.New("no change waits under this id on the server: it was already undone (or confirmed)")
	case r.Code != 0:
		audit(fmt.Sprintf("code %d", r.Code))
		return "", fmt.Errorf("the confirmation failed (code %d): %s", r.Code, couper(s.masquerSecrets(strings.TrimSpace(string(r.Sortie))), 500))
	}
	audit("gardé")
	return "Change " + idAn + " kept: its automatic rollback is cancelled.", nil
}

func init() {
	outilsHub["confirmer_changement"] = OutilHub{Description: "Keep a firewall or SSH change: until then, the server undoes it by itself when its delay ends. Confirm only once a new connection works and the change does what it should.",
		Schema: schemaObjet(map[string]any{"serveur": schemaTexte("The server's id, from serveurs_liste"), "id": schemaTexte("The change's id, given by the tool that made it")}, "serveur", "id"),
		Faire: func(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
			var a struct {
				Serveur string `json:"serveur"`
				ID      string `json:"id"`
			}
			if err := lireArguments(args, &a); err != nil {
				return "", err
			}
			if err := s.serveurPerimetre(x, "confirmer_changement", a.Serveur); err != nil {
				return "", err
			}
			txt, err := s.confirmerChangement(context.Background(), x.Agent, a.Serveur, a.ID)
			if err != nil {
				return "", errors.New(s.masquerSecrets(err.Error()))
			}
			return txt, nil
		}}
}
