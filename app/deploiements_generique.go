package main

// What the agent chooses for a project's deployment, from the owner's plan and the conventions of the stack, rather
// than anything written in Athanor: where it lives on the servers (racine), a command that prepares each new release
// before it goes online (avant: install, build), a command run once it is online and after going back (apres:
// reload a service, a process manager, a platform), and a method that sends nothing (commande: each server updates
// itself with apres, a stack that deploys on push or pulls its code; retour its way back). DEPLOYMENT.md, written in
// the project when the method is recorded, says it all.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var reRacine = regexp.MustCompile(`^/[A-Za-z0-9._@+-]+(/[A-Za-z0-9._@+-]+)+$`)

// racineValide: an absolute folder of its own, two levels at least, never a system folder, a home or a temporary one.
func racineValide(r string) bool {
	if !reRacine.MatchString(r) || strings.Contains(r, "/.") || strings.Contains(r, "/-") {
		return false
	}
	for _, x := range []string{"/etc", "/bin", "/sbin", "/usr", "/lib", "/lib32", "/lib64", "/boot", "/proc", "/sys", "/dev", "/run", "/tmp", "/var/tmp", "/var/log", "/var/lib"} {
		if r == x || strings.HasPrefix(r, x+"/") {
			return false
		}
	}
	return !regexp.MustCompile(`^/home/[^/]+$`).MatchString(r)
}

// commandeLigne: "" or one line of maxCommande characters at most.
func commandeLigne(c string) bool {
	return len([]rune(c)) <= maxCommande && !strings.ContainsFunc(c, func(r rune) bool { return r < ' ' })
}

// validerChoixAgent: the fields the agent chooses, checked with the method's own rules.
func validerChoixAgent(c ConfigDeploiement) error {
	switch {
	case c.Racine != "" && !racineValide(c.Racine):
		return errors.New("racine: an absolute folder of its own (/srv/<app>, /var/www/<site>, /opt/<app>, /home/<user>/<app>…), without spaces, hidden parts nor a system, home or temporary folder")
	case !commandeLigne(c.Avant) || !commandeLigne(c.Apres) || !commandeLigne(c.Retour):
		return fmt.Errorf("avant, apres and retour: one line of %d characters at most", maxCommande)
	case c.Methode == "commande" && strings.TrimSpace(c.Apres) == "":
		return errors.New("commande: apres is the command each server runs to update (nothing is sent)")
	case c.Methode == "commande" && (c.Avant != "" || c.Construire != "" || c.Dossier != "" || c.Racine != ""):
		return errors.New("commande sends nothing: no avant, construire, dossier nor racine (apres does it all)")
	case c.Methode == "k3s" && (c.Avant != "" || c.Apres != ""):
		return errors.New("avant and apres are for statique, compose and commande")
	case c.Retour != "" && c.Methode != "commande":
		return errors.New("retour is the way back of commande; the other methods go back to the release kept on the servers")
	}
	return nil
}

// baseDe: the site's folder on its servers: the agent's choice, else Athanor's older one (sites deployed before).
func baseDe(cfg *ConfigDeploiement, projet, env string) string {
	if cfg != nil && cfg.Racine != "" {
		return cfg.Racine
	}
	return baseDeploiement(projet, env)
}

// scriptDans ($1 a folder, $2 a command): the command run in the folder (/ when it does not exist), its output kept.
const scriptDans = `cd -- "$1" 2>/dev/null || cd /
sh -c "$2" 2>&1`

// lancerSur runs commande in dossier as root on each server, stopping at the first failure; cle is its message
// ({0} the server, {1} why).
func (s *Serveur) lancerSur(ctx context.Context, serveurs []serveurPret, dossier, commande, cle string, delai time.Duration) error {
	for _, x := range serveurs {
		if err := echecSSH(executerSSH(ctx, x.c, commeRoot(scriptDans, dossier, commande), nil, delai)); err != nil {
			return errMoteur(cle, nomServeur(x.id), err)
		}
	}
	return nil
}

const (
	errAvant  = "{0}: the preparation command failed: {1}"
	errApres  = "{0}: the command after going online failed: {1}"
	errMaj    = "{0}: the update command failed: {1}"
	errRetour = "{0}: the way back failed: {1}"
)

// connecterTous: a connection to each server of ids (all closed by the caller).
func (s *Serveur) connecterTous(ctx context.Context, ids []string) ([]serveurPret, error) {
	l, err := lireServeurs()
	if err != nil {
		return nil, err
	}
	var prets []serveurPret
	for _, id := range ids {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
		if i < 0 {
			return prets, errServeurInconnu
		}
		c, err := s.connecterServeur(ctx, l[i])
		if err != nil {
			return prets, errMoteur("{0}: {1}", nomServeur(id), err)
		}
		prets = append(prets, serveurPret{id, c})
	}
	return prets, nil
}

func fermerTous(l []serveurPret) {
	for _, x := range l {
		x.c.Close()
	}
}

// deployerCommande: the method that sends nothing: each server runs apres (it pulls, builds, restarts, or tells
// its platform), in turn; a server that fails stops the others.
func (s *Serveur) deployerCommande(ctx context.Context, cfg ConfigDeploiement, d *Deploiement) error {
	prets, err := s.connecterTous(ctx, d.Serveurs)
	defer fermerTous(prets)
	if err == nil {
		err = s.lancerSur(ctx, prets, "/", cfg.Apres, errMaj, delaiConstruction)
	}
	if err != nil {
		return err
	}
	d.Bascule = true
	return nil
}

// revenirCommande: commande's way back: retour on each server, then the external check.
func (s *Serveur) revenirCommande(ctx context.Context, projet, env, auteur string, e Environnement) (Deploiement, error) {
	if strings.TrimSpace(e.Deploiement.Retour) == "" {
		return Deploiement{}, errors.New("no way back was recorded for this deployment: its servers update themselves")
	}
	d := Deploiement{ID: aleatoire(8), Projet: projet, Env: env, Methode: "commande", Version: time.Now().UTC().Format("20060102-150405"), Etat: "en_cours",
		Auteur: auteur, Retour: true, Serveurs: e.Serveurs, Debut: time.Now().UTC().Format(time.RFC3339)}
	if err := noterHistorique(d); err != nil {
		return d, errMoteur("the deployment history cannot be written: {0}", err)
	}
	prets, err := s.connecterTous(ctx, e.Serveurs)
	defer fermerTous(prets)
	if err == nil {
		err = s.lancerSur(ctx, prets, "/", e.Deploiement.Retour, errRetour, delaiConstruction)
	}
	d.Etat = "ok"
	if err == nil {
		d.Bascule = true
		err = s.verifierApres(ctx, e, &d)
	} else {
		d.Etat, d.Message = "echec", messageDe(err)
	}
	d.Fin = time.Now().UTC().Format(time.RFC3339)
	s.noterMessage(&d)
	_ = noterHistorique(d)
	s.ecrireAudit(LigneAudit{Type: "evenement", De: auteur, Vers: "hub", Projet: projet, Env: env, Action: "deploiement.retour", Resultat: d.Etat + " " + couper(d.Erreur, 160)})
	return d, err
}

// texteDeploiementMD: DEPLOYMENT.md, what the project's deployment is, as recorded (no value of the vault).
func texteDeploiementMD(p Projet, e Environnement, cfg ConfigDeploiement, env string) string {
	var b strings.Builder
	b.WriteString("# Deployment\n\nWritten by Athanor when this project's deployment method was recorded; the servers keep their own README beside what was set up.\n\n")
	ligne := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			b.WriteString("- **" + k + ":** " + v + "\n")
		}
	}
	ligne("Address", e.URL)
	ligne("Servers", strings.Join(nomsServeurs(e.tousServeurs()), ", "))
	methodes := map[string]string{"statique": "a folder served by the web server (each version in releases/, the one online behind current)",
		"compose": "Docker Compose, each version in releases/, the one online behind current",
		"k3s":     "kubectl apply of the manifests on the k3s server", "commande": "nothing is sent: each server updates itself with the update command"}
	ligne("Method", cfg.Methode+": "+methodes[cfg.Methode])
	if cfg.Methode != "commande" {
		ligne("Folder on the servers", "`"+baseDe(&cfg, p.Nom, env)+"` (versions in `releases/`, the one online: `current`)")
		ligne("Published folder of the repository", "`"+ouSinon(cfg.Dossier, ".")+"`")
		ligne("Built on the owner's computer before it is sent", enCode(cfg.Construire))
		ligne("Prepared in each new version before it goes online", enCode(cfg.Avant))
	}
	ligne("Run once a version is online and after going back", enCode(cfg.Apres))
	retour := "the previous version stays on the servers: *Put the previous version back online* switches back to it"
	if cfg.Methode == "commande" {
		retour = ouSinon(enCode(cfg.Retour), "none recorded")
	}
	ligne("Way back", retour)
	if t := strings.TrimSpace(e.Plan); t != "" {
		b.WriteString("\n## The owner's plan for the servers\n\n" + t + "\n")
	}
	return b.String()
}

func ouSinon(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

func enCode(c string) string {
	if strings.TrimSpace(c) == "" {
		return ""
	}
	return "`" + strings.ReplaceAll(c, "`", "'") + "`"
}

// ecrireDeploiementMD: DEPLOYMENT.md in the project's folder (a link there is never followed).
func (s *Serveur) ecrireDeploiementMD(p Projet, e Environnement, cfg ConfigDeploiement, env string) {
	f := filepath.Join(p.Dossier, "DEPLOYMENT.md")
	if fi, err := os.Lstat(f); err == nil && !fi.Mode().IsRegular() {
		return
	}
	_ = ecrireAtomique(f, []byte(s.masquerSecrets(texteDeploiementMD(p, e, cfg, env))), 0o644)
}
