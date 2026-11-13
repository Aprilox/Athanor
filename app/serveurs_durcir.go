package main

// Hardening a new server (M138, ARCHITECTURE.md §Déploiements, decision 6): the button "Harden this server"
// reads the server's state, then proposes a fixed plan written here (never by an agent) of the steps not in
// place yet: a non-root user with sudo and Athanor's key, key logins only, PermitRootLogin no, ufw denying
// everything but SSH, 80 and 443, unattended-upgrades, time synchronisation, Caddy. The owner accepts it as any
// plan_serveur approval; Athanor then runs it through the Servers tools (an SSH or firewall change keeps its
// automatic rollback and is kept only once a new connection with sudo works).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	agentDurcir        = "durcissement"
	utilisateurDurci   = "athanor" // the user Athanor creates when it reaches the server as root
	resumeDurcissement = "Harden this server: Athanor's fixed plan, each step skipped when it is already in place."
)

// etapesDurcir: the steps' keys, in the order they run (the user first: PermitRootLogin no needs it).
var etapesDurcir = []string{"utilisateur", "cles", "root", "maj", "heure", "pare_feu", "docker", "caddy"}

// scriptEtatDurci: read only; prints sudo=non (no root, no sudo -n), apt=non, include=non when hardening
// cannot run, then key=oui|non for each step.
const scriptEtatDurci = preludeRoot + `[ "$(id -u)" = 0 ] || [ -n "$S" ] || { echo sudo=non; exit 0; }
command -v apt-get >/dev/null 2>&1 || echo apt=non
grep -Eqi '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d' /etc/ssh/sshd_config 2>/dev/null || echo include=non
T=$($S sshd -T 2>/dev/null | tr 'A-Z' 'a-z'); U=$($S ufw status verbose 2>/dev/null)
a() { printf '%s\n' "$T" | grep -qx "$1"; }
paquet() { dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'ok installed'; }
c_utilisateur() { [ "$(id -u)" != 0 ]; }
c_cles() { a 'passwordauthentication no' && { a 'kbdinteractiveauthentication no' || a 'challengeresponseauthentication no'; }; }
c_root() { a 'permitrootlogin no'; }
c_maj() { paquet unattended-upgrades && apt-config dump 2>/dev/null | grep -q 'APT::Periodic::Unattended-Upgrade "1"'; }
c_heure() { for u in systemd-timesyncd chrony chronyd ntp ntpsec openntpd; do systemctl is-active --quiet "$u" 2>/dev/null && return 0; done
  [ ! -d /run/systemd/system ] && paquet systemd-timesyncd; }
c_pare_feu() { printf '%s\n' "$U" | grep -q '^Status: active' && printf '%s\n' "$U" | grep -q 'deny (incoming)' || return 1
  for p in "$@"; do printf '%s\n' "$U" | grep -Eq "^$p(/tcp)? +ALLOW IN" || return 1; done; }
c_ufw() { command -v ufw; }
c_docker() { $S grep -q '^# BEGIN ATHANOR DOCKER-USER' /etc/ufw/after.rules; }
for k in utilisateur cles root maj heure pare_feu docker ufw caddy; do
  if [ $k = caddy ]; then f() { command -v caddy; }; else f() { c_$k "$@"; }; fi
  if ( f "$@" ) >/dev/null 2>&1; then echo "$k=oui"; else echo "$k=non"; fi
done`

// The steps' scripts, run as root (commeRoot): apt output is kept for a failure only.
const (
	preludeApt = `export DEBIAN_FRONTEND=noninteractive
apt() { o=$(apt-get -q -y -o DPkg::Lock::Timeout=120 "$@" 2>&1) || { printf '%s\n' "$o" | tail -n 20; exit 1; }; }
`
	// $1 the user, $2 Athanor's public key (type and base64)
	scriptUtilisateurDurci = preludeApt + `command -v sudo >/dev/null 2>&1 || { apt update; apt install sudo; }
id -u "$1" >/dev/null 2>&1 || useradd -m -s /bin/bash "$1" || exit 1
H=$(getent passwd "$1" | cut -d: -f6); G=$(id -gn "$1")
install -d -m 700 -o "$1" -g "$G" "$H/.ssh" && touch "$H/.ssh/authorized_keys" &&
{ grep -qF "$2" "$H/.ssh/authorized_keys" || printf '%s athanor\n' "$2" >> "$H/.ssh/authorized_keys"; } &&
chown "$1:$G" "$H/.ssh/authorized_keys" && chmod 600 "$H/.ssh/authorized_keys" || exit 1
F=/etc/sudoers.d/90-athanor-$1; printf '%s ALL=(ALL) NOPASSWD:ALL\n' "$1" > "$F.tmp" && chmod 440 "$F.tmp" &&
visudo -cf "$F.tmp" >/dev/null && mv -f "$F.tmp" "$F" || { rm -f "$F.tmp"; exit 1; }
echo "user $1 ready: sudo without a password, Athanor's key"`
	scriptMajDurci = preludeApt + `apt update; apt install unattended-upgrades
printf 'APT::Periodic::Update-Package-Lists "1";\nAPT::Periodic::Unattended-Upgrade "1";\n' > /etc/apt/apt.conf.d/20auto-upgrades || exit 1
echo "security updates are installed automatically"`
	scriptHeureDurci = preludeApt + `dpkg-query -W -f='${Status}' systemd-timesyncd 2>/dev/null | grep -q 'ok installed' || { apt update; apt install systemd-timesyncd; }
if [ -d /run/systemd/system ]; then systemctl enable --now systemd-timesyncd && timedatectl set-ntp true || exit 1; fi
echo "the clock is synchronised by systemd-timesyncd"`
	scriptUfwDurci = preludeApt + `apt update; apt install ufw; echo "ufw installed"`
	// C70: Docker publishes its ports past ufw; ufw's after.rules fills DOCKER-USER at each start or reload,
	// refusing new connections from outside into Docker's bridges (Caddy reaches them on 127.0.0.1).
	// shortcut: IPv4 only (after6.rules untouched), Docker's IPv6 is off by default.
	scriptDockerDurci = `f=/etc/ufw/after.rules
[ -f "$f" ] || { echo "ufw is not installed: $f is missing"; exit 1; }
grep -q '^# BEGIN ATHANOR DOCKER-USER' "$f" || printf '%s\n' '# BEGIN ATHANOR DOCKER-USER: containers reached from outside only through Caddy' '*filter' ':DOCKER-USER - [0:0]' \
  '-A DOCKER-USER -m conntrack --ctstate RELATED,ESTABLISHED -j RETURN' '-A DOCKER-USER -i docker+ -j RETURN' '-A DOCKER-USER -i br-+ -j RETURN' \
  '-A DOCKER-USER -o docker+ -j DROP' '-A DOCKER-USER -o br-+ -j DROP' '-A DOCKER-USER -j RETURN' 'COMMIT' '# END ATHANOR DOCKER-USER' >> "$f" || exit 1
if ufw status 2>/dev/null | grep -q '^Status: active'; then o=$(ufw reload 2>&1) || { printf '%s\n' "$o" | tail -n 4; exit 1; }; fi
echo "Docker's published ports are refused from outside (DOCKER-USER)"`
	scriptCaddyDurci = preludeApt + `apt update; apt install debian-keyring debian-archive-keyring apt-transport-https curl gnupg
curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/gpg.key | gpg --batch --yes --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg &&
curl -1sLf https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt > /etc/apt/sources.list.d/caddy-stable.list &&
chmod o+r /usr/share/keyrings/caddy-stable-archive-keyring.gpg /etc/apt/sources.list.d/caddy-stable.list || exit 1
apt update; apt install caddy; echo "Caddy installed from its official repository"`
)

const (
	fichierClesDurci = "/etc/ssh/sshd_config.d/00-athanor.conf" // the same file as "Cut password logins"
	fichierRootDurci = "/etc/ssh/sshd_config.d/01-athanor-root.conf"
	contenuClesDurci = "# Athanor: password logins cut. To allow them again, remove this file, then reload the SSH service.\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n"
	contenuRootDurci = "# Athanor: no root login (hardening). To allow it again, remove this file, then reload the SSH service.\nPermitRootLogin no\n"
)

// lireEtatDurci: each step's state on the server (true: in place) and why hardening cannot run there.
func (s *Serveur) lireEtatDurci(ctx context.Context, srv ServeurInventaire) (map[string]bool, error) {
	c, err := s.connecterServeur(ctx, srv)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	args := []string{}
	for _, p := range portsDurci(ctx, c, srv) {
		args = append(args, strconv.Itoa(p))
	}
	r, err := executerSSH(ctx, c, scriptShell(false, scriptEtatDurci, args...), nil, delaiCommande)
	if err != nil {
		return nil, err
	}
	etat := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(r.Sortie)), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			etat[k] = v == "oui"
		}
	}
	switch {
	case strings.Contains(string(r.Sortie), "sudo=non"):
		return nil, erreurEntree("Hardening needs root or sudo without a password on the server")
	case strings.Contains(string(r.Sortie), "apt=non"):
		return nil, erreurEntree("Hardening knows Debian and Ubuntu servers (apt) only")
	case strings.Contains(string(r.Sortie), "include=non"):
		return nil, erreurEntree("This server's sshd_config does not read /etc/ssh/sshd_config.d/: hardening needs a recent Debian or Ubuntu")
	case len(etat) < len(etapesDurcir):
		return nil, fmt.Errorf("the server's state could not be read: %s", couper(strings.TrimSpace(string(r.Sortie)), 300))
	}
	return etat, nil
}

// portsDurci: the TCP ports the firewall keeps open: 22, the ports sshd listens on (else the one Athanor
// uses), 80 and 443.
func portsDurci(ctx context.Context, c *ssh.Client, srv ServeurInventaire) []int {
	l := portsSSHD(ctx, c)
	if len(l) == 0 {
		l = []int{srv.Port}
	}
	l = append(l, 22, 80, 443)
	slices.Sort(l)
	return slices.Compact(l)
}

// planDurcissement: the steps not in place yet (etat), as Athanor runs them.
func (s *Serveur) planDurcissement(srv ServeurInventaire, etat map[string]bool) ([]EtapePlan, error) {
	var l []EtapePlan
	ajouter := func(cle, outil, pourquoi string, a argsOutilServeur) {
		b, _ := json.Marshal(a)
		v := map[string]json.RawMessage{}
		_ = json.Unmarshal(b, &v)
		et, err := classerEtape(outil, v)
		e := EtapePlan{Outil: outil, Arguments: b, Classe: ClasseChangement.String(), Raison: "Athanor's fixed hardening step", Pourquoi: pourquoi, Durcir: cle}
		if err == nil && outil != "ssh_commande" { // a script Athanor wrote is not an agent's command: its own class
			e.Classe, e.Raison = et.Classe.String(), et.Raison
		}
		if e.Classe == ClasseDestructrice.String() {
			e.SansSauvegarde = "The server puts the state before back by itself unless Athanor keeps the change."
		}
		l = append(l, e)
	}
	root := func(script string, args ...string) argsOutilServeur {
		return argsOutilServeur{Commande: commeRoot(script, args...), DelaiS: int(delaiCommandeMax / time.Second)}
	}
	if !etat["utilisateur"] {
		cle, err := s.coffre.Valeur(srv.Cle)
		if err != nil {
			return nil, err
		}
		signer, err := ssh.ParsePrivateKey([]byte(cle))
		if err != nil {
			return nil, fmt.Errorf("unreadable SSH key: %w", err)
		}
		pub := strings.Join(strings.Fields(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))[:2], " ")
		ajouter("utilisateur", "ssh_commande", "Create the user athanor with sudo and Athanor's key: Athanor stops connecting as root.", root(scriptUtilisateurDurci, utilisateurDurci, pub))
	}
	if !etat["cles"] {
		ajouter("cles", "fichier_ecrire", "Allow SSH keys only: password logins are cut.", argsOutilServeur{Chemin: fichierClesDurci, Contenu: &[]string{contenuClesDurci}[0], Sudo: true})
	}
	if !etat["root"] {
		ajouter("root", "fichier_ecrire", "Refuse root logins over SSH (PermitRootLogin no).", argsOutilServeur{Chemin: fichierRootDurci, Contenu: &[]string{contenuRootDurci}[0], Sudo: true})
	}
	if !etat["maj"] {
		ajouter("maj", "ssh_commande", "Install security updates automatically (unattended-upgrades).", root(scriptMajDurci))
	}
	if !etat["heure"] {
		ajouter("heure", "ssh_commande", "Keep the server's clock synchronised (systemd-timesyncd).", root(scriptHeureDurci))
	}
	if !etat["pare_feu"] {
		if !etat["ufw"] {
			ajouter("ufw", "ssh_commande", "Install the ufw firewall.", root(scriptUfwDurci))
		}
		ajouter("pare_feu", "pare_feu", "Firewall: refuse every incoming connection except SSH, 80 and 443.", argsOutilServeur{Action: "tout_refuser"})
	}
	if !etat["docker"] {
		ajouter("docker", "ssh_commande", "Firewall: refuse outside connections to Docker's published ports (DOCKER-USER rule); Caddy reaches them on 127.0.0.1.", root(scriptDockerDurci))
	}
	if !etat["caddy"] {
		ajouter("caddy", "ssh_commande", "Install Caddy from its official repository: it serves the sites over HTTPS.", root(scriptCaddyDurci))
	}
	return l, nil
}

// apiServeurDurcir: POST /api/serveurs/{id} {durcir:true}: {deja:true} when every step is in place, else the
// plan waiting for the owner's approval.
func (s *Serveur) apiServeurDurcir(w http.ResponseWriter, r *http.Request, id string) {
	if !s.coffrePret(w) {
		return
	}
	ctx, fin := context.WithTimeout(context.WithoutCancel(r.Context()), delaiCommande)
	defer fin()
	srv, prod := serveurProd(id)
	if srv.Hote == "" {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	etat, err := s.lireEtatDurci(ctx, srv)
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	if etat["cles"] && !srv.MotDePasseCoupe {
		s.noterMotDePasseCoupe(id)
	}
	etapes, err := s.planDurcissement(srv, etat)
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	if len(etapes) == 0 {
		s.ecrireJSON(w, map[string]any{"deja": true})
		return
	}
	p := PlanServeur{Serveur: id, Nom: srv.Nom, Env: "dev", Projet: prefixeServeur + id, Agent: agentDurcir, Resume: resumeDurcissement, Etapes: etapes}
	if prod {
		p.Env = "prod"
	}
	ap, err := s.demanderApprobation(Approbation{Projet: p.Projet, Env: p.Env, Agent: agentDurcir, Action: actionPlanServeur, Raison: resumeDurcissement,
		Details: map[string]string{"serveur": id, "nom": srv.Nom, "classe": classeHaute(etapes), "etapes": strconv.Itoa(len(etapes)), "regle": "non"}})
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	p.ID, p.Cree, p.Expire = ap.ID, ap.Cree, ap.Expire
	if err := s.enregistrerPlan(p); err != nil {
		_, _, _ = s.deciderApprobation("hub", ap.ID, "refuser", "", "", "")
		s.erreurServeurs(w, err)
		return
	}
	cles := []string{}
	for _, e := range etapes {
		cles = append(cles, e.Durcir)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "serveur:" + id, Projet: p.Projet, Env: p.Env, Agent: agentDurcir,
		Action: "serveurs.durcir", Resultat: "plan " + p.ID + " → attente : " + strings.Join(cles, ", ")})
	s.ecrireJSON(w, map[string]any{"plan": p.ID, "etapes": cles})
}

func classeHaute(l []EtapePlan) string {
	if slices.ContainsFunc(l, func(e EtapePlan) bool { return e.Classe == ClasseDestructrice.String() }) {
		return ClasseDestructrice.String()
	}
	return ClasseChangement.String()
}

// planDurcissementAccepte: approval x is a hardening plan Athanor wrote (an agent's plan has no Durcir step).
func (s *Serveur) planDurcissementAccepte(x Approbation) bool {
	if x.Action != actionPlanServeur || x.Agent != agentDurcir || x.Etat != "acceptee" {
		return false
	}
	p, err := s.lirePlan(x.ID)
	return err == nil && p.Projet == prefixeServeur+p.Serveur && len(p.Etapes) > 0 &&
		!slices.ContainsFunc(p.Etapes, func(e EtapePlan) bool { return e.Durcir == "" })
}

// appliquerDurcissement runs the accepted plan id step by step; a failed step stops it (the steps after it do
// not run). An SSH or firewall change is kept only once a new connection with sudo works, else the server
// undoes it by itself.
func (s *Serveur) appliquerDurcissement(id string) {
	ctx, fin := context.WithTimeout(context.Background(), 45*time.Minute)
	defer fin()
	p, err := s.lirePlan(id)
	if err != nil {
		return
	}
	audit := func(res string) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: agentDurcir, Vers: "serveur:" + p.Serveur, Projet: p.Projet, Env: p.Env, Agent: agentDurcir, Action: "serveurs.durcir", Resultat: res})
	}
	for i, e := range p.Etapes {
		s.marquerEtapeLancee(id, i)
		err := s.etapeDurcissement(ctx, p.Serveur, e)
		s.noterEtape(id, i+1, err, nil)
		if err != nil {
			audit(fmt.Sprintf("plan %s : étape %d (%s) en échec, arrêt : %s", id, i+1, e.Durcir, couper(s.masquerSecrets(err.Error()), 300)))
			return
		}
	}
	srv, _ := serveurProd(p.Serveur)
	etat, err := s.lireEtatDurci(ctx, srv)
	manque := []string{}
	for _, k := range etapesDurcir {
		if err == nil && !etat[k] {
			manque = append(manque, k)
		}
	}
	switch {
	case err != nil:
		audit("plan " + id + " appliqué ; état relu impossible : " + couper(s.masquerSecrets(err.Error()), 300))
	case len(manque) > 0:
		audit("plan " + id + " appliqué ; pas encore en place : " + strings.Join(manque, ", "))
	default:
		audit("plan " + id + " appliqué : tout est en place")
	}
	if _, e := s.rafraichirFiche(ctx, srv); e != nil {
		audit("fiche non relue : " + e.Error())
	}
}

// marquerEtapeLancee: step i of plan id starts now (shown on its card).
func (s *Serveur) marquerEtapeLancee(id string, i int) {
	verrouPlans.Lock()
	defer verrouPlans.Unlock()
	if p, err := s.lirePlan(id); err == nil && i < len(p.Etapes) {
		p.Etapes[i].Executee, p.Etapes[i].Essais = time.Now().UTC().Format(time.RFC3339), p.Etapes[i].Essais+1
		_ = s.enregistrerPlan(p)
	}
}

// etapeDurcissement runs one step, then checks that Athanor still gets in (with sudo) before keeping it.
func (s *Serveur) etapeDurcissement(ctx context.Context, id string, e EtapePlan) error {
	r, err := s.appelerOutilServeur(ctx, agentDurcir, id, e.Outil, e.Arguments, Sauvegarde{})
	if err == nil && r.Code != 0 {
		err = fmt.Errorf("code %d: %s", r.Code, couper(strings.TrimSpace(r.Sortie), 500))
	}
	if err != nil {
		return err
	}
	if e.Durcir == "utilisateur" { // Athanor connects as the new user from now on, or stays as it was
		var avant string
		if err := s.changerUtilisateurServeur(id, utilisateurDurci, &avant); err != nil {
			return err
		}
		if err := s.accesSudo(ctx, id); err != nil {
			_ = s.changerUtilisateurServeur(id, avant, nil)
			return fmt.Errorf("the new user %s does not get in with sudo, Athanor keeps connecting as %s: %w", utilisateurDurci, avant, err)
		}
	}
	if e.Outil == "ssh_commande" {
		if srv, _ := serveurProd(id); srv.Hote != "" {
			_, _ = s.rafraichirFiche(ctx, srv) // the software list changed (the firewall step reads it)
		}
	}
	if r.Annulation != "" {
		if err := s.accesSudo(ctx, id); err != nil {
			return fmt.Errorf("Athanor no longer gets in with sudo after this change: the server undoes it by itself: %w", err)
		}
		if _, err := s.confirmerChangement(ctx, agentDurcir, id, r.Annulation); err != nil {
			return err
		}
	}
	if e.Durcir == "cles" {
		s.noterMotDePasseCoupe(id)
	}
	return nil
}

// accesSudo: a new connection to server id works and sudo -n there too (or it is root).
func (s *Serveur) accesSudo(ctx context.Context, id string) error {
	srv, _ := serveurProd(id)
	var err error
	for essai := 0; essai < 5; essai++ { // sshd may be reloading
		var c *ssh.Client
		if c, err = s.connecterServeur(ctx, srv); err == nil {
			var r ResultatSSH
			r, err = executerSSH(ctx, c, `[ "$(id -u)" = 0 ] || sudo -n true`, nil, delaiCommande)
			c.Close()
			if err == nil && r.Code != 0 {
				err = errors.New("sudo -n refused")
			}
			if err == nil {
				return nil
			}
		}
		time.Sleep(attenteRechargeSSH)
	}
	return err
}

// changerUtilisateurServeur: Athanor connects to server id as u (the one before in *avant).
func (s *Serveur) changerUtilisateurServeur(id, u string, avant *string) error {
	return modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		if i < 0 {
			return nil, errServeurInconnu
		}
		if avant != nil {
			*avant = l[i].Utilisateur
		}
		l[i].Utilisateur = u
		return l, nil
	})
}

func (s *Serveur) noterMotDePasseCoupe(id string) {
	_ = modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		if i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id }); i >= 0 {
			l[i].MotDePasseCoupe = true
		}
		return l, nil
	})
}
