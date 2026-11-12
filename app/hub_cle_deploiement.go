package main

// Stage 7: a project's GitHub deployment key. Athanor makes an ed25519 key, keeps its private half in the
// vault (infra, source deploiement:<project>) and registers the public half READ-ONLY on the project's
// repository with this computer's access to GitHub (athanor github cle-deploiement, on the pilot's side,
// where the GitHub link lives). Unlinking or deleting the project takes it off the repository (delierGH)
// and out of the vault (oublierCleDeploiement). Stage 8 installs it on the server for git fetch.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"
)

const titreCleDeploiementGH = "Athanor deploy key (read-only)"

var (
	errCleDeploiementNonLie = erreurEntree("This project is not linked to GitHub: link it in its GitHub panel first, then create its deployment key.")
	errCleDeploiementGH     = "GitHub refused the deployment key: {0}"
)

// cleDeploiementGH: athanor github cle-deploiement PROJECT ajouter PUBLIC-KEY → {"id","depot"} on its
// last line. The repository's previous deployment key made by Athanor, if any, is removed first.
func cleDeploiementGH(args []string, w io.Writer) int {
	projet, ok := cheminProjetGH(argGH(args, 0))
	if !ok {
		return 2
	}
	pub := strings.TrimSpace(argGH(args, 2))
	champs := strings.Fields(pub)
	if argGH(args, 1) != "ajouter" || len(champs) < 2 || !reCleSSHGH.MatchString(champs[0]+" "+champs[1]) || champs[0] != "ssh-ed25519" {
		fmt.Fprintln(w, "Usage: athanor github cle-deploiement PROJECT ajouter 'ssh-ed25519 KEY'")
		return 2
	}
	d := dossierGH(projet)
	r, _ := lireReglagesGH(d)
	if !estDossierGH(d) || !depotValideGH(r.Depot) {
		fmt.Fprintln(w, "Not linked to GitHub.")
		return 1
	}
	gh, fermer := ghCleDeploiement()
	defer fermer()
	out, erreur, _ := gh.api("-X", "POST", "repos/"+r.Depot+"/keys", "-f", "title="+titreCleDeploiementGH,
		"-f", "key="+champs[0]+" "+champs[1], "-F", "read_only=true")
	id := idCleGH(out)
	if id == "" {
		fmt.Fprintln(w, "GitHub refused the deployment key: "+firstNonEmpty(strings.TrimSpace(coupeValide(erreur, 300)), "no answer"))
		return 1
	}
	if ancien := lireIDCleDeploiement(d); ancien != "" && ancien != id {
		_, _, _ = gh.api("-X", "DELETE", "repos/"+r.Depot+"/keys/"+ancien)
	}
	if err := ecrireFichierPrive(d+"/cle_deploiement", id+"\n"); err != nil {
		fmt.Fprintln(w, "Cannot save the deployment key's id: "+err.Error())
		return 1
	}
	fmt.Fprintf(w, "{\"id\":%q,\"depot\":%q}\n", id, r.Depot)
	return 0
}

// ghCleDeploiement: gh with the access kept on this computer when there is one, else gh's own sign-in.
func ghCleDeploiement() (ghLier, func()) {
	if ses := sessionGardeeGH(); ses != nil {
		return ses.gh, ses.fermer
	}
	return ghLier{}, func() {}
}

func idCleGH(out []byte) string {
	var k struct {
		ID json.Number `json:"id"`
	}
	if json.Unmarshal(out, &k) != nil || !reCleIDGH.MatchString(k.ID.String()) {
		return ""
	}
	return k.ID.String()
}

func lireIDCleDeploiement(d string) string {
	b, _ := os.ReadFile(d + "/cle_deploiement")
	if id := strings.TrimSpace(string(b)); reCleIDGH.MatchString(id) {
		return id
	}
	return ""
}

// retirerCleDeploiementGH: the deployment key taken off the repository while unlinking (best effort).
func retirerCleDeploiementGH(d, depot string, w io.Writer) {
	id := lireIDCleDeploiement(d)
	if id == "" || !depotValideGH(depot) {
		return
	}
	gh, fermer := ghCleDeploiement()
	defer fermer()
	if _, _, err := gh.api("-X", "DELETE", "repos/"+depot+"/keys/"+id); err == nil {
		fmt.Fprintf(w, "Deployment key removed from %s.\n", depot)
	}
}

// executerCleDeploiement: the pilot's side (a variable: the tests run it in this process).
var executerCleDeploiement = func(s *Serveur, p Projet, args ...string) (string, error) {
	return s.executerGitHub(p, 2*time.Minute, "cle-deploiement", args...)
}

// apiCleDeploiement: POST /api/projets/{nom}/cle-deploiement: the key made once; already there = nothing.
func (s *Serveur) apiCleDeploiement(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	if !s.accesPret(w, projet) {
		return
	}
	p, ok := s.projetParNom(projet)
	if !ok {
		s.erreur(w, http.StatusNotFound, "Unknown project")
		return
	}
	if id := s.acces.CleDeploiementProjet(projet); id != "" && slices.ContainsFunc(s.coffre.Liste(), func(e EntreeCoffre) bool { return e.ID == id }) {
		s.ecrireJSON(w, map[string]any{"ok": true, "deja": true})
		return
	}
	prive, public, err := cleGeneree("athanor-deploiement-" + coupeValide(reNonNomCle.ReplaceAllString(projet, "-"), 40))
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	// in the vault first: a locked vault stops here, before anything is registered on GitHub
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "DEPLOY_KEY " + projet, Famille: "secret", Type: "ssh", Portee: "infra",
		Source: "deploiement:" + projet, Note: "GitHub deployment key of " + projet + " (read-only on its repository)"}, prive)
	if err != nil {
		s.erreurCoffre(w, err)
		return
	}
	out, err := executerCleDeploiement(s, p, "ajouter", public)
	var v struct{ ID, Depot string }
	if err == nil {
		err = lireJSONFinal(out, &v)
	}
	if err != nil || v.ID == "" {
		_ = s.coffre.Supprimer(e.ID)
		switch raison := strings.TrimSpace(out); {
		case strings.Contains(raison, "Not linked to GitHub."):
			s.erreur(w, http.StatusBadRequest, errCleDeploiementNonLie.Error())
		case strings.Contains(raison, "GitHub refused the deployment key: "):
			_, apres, _ := strings.Cut(raison, "GitHub refused the deployment key: ")
			s.erreurArgs(w, http.StatusBadGateway, errCleDeploiementGH, coupeValide(strings.SplitN(apres, "\n", 2)[0], 300))
		default:
			s.erreurArgs(w, http.StatusBadGateway, errCleDeploiementGH, coupeValide(firstNonEmpty(raison, fmt.Sprint(err)), 300))
		}
		return
	}
	if err := s.acces.NoterCleDeploiement(projet, e.ID); err != nil {
		s.erreur(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Action: "clé de déploiement créée",
		Resultat: "lecture seule sur " + v.Depot})
	s.ecrireJSON(w, map[string]any{"ok": true, "depot": v.Depot})
}

// oublierCleDeploiement: the project unlinked or deleted: its key leaves the vault (GitHub: delierGH).
func (s *Serveur) oublierCleDeploiement(projet string) {
	if s.acces == nil || s.coffre == nil {
		return
	}
	if id := s.acces.CleDeploiementProjet(projet); id != "" {
		if err := s.coffre.Supprimer(id); err == nil || errors.Is(err, errCoffreInconnu) {
			_ = s.acces.NoterCleDeploiement(projet, "")
		}
	}
}

// CleDeploiementProjet: the vault entry of the project's deployment key ("" when none).
func (a *Acces) CleDeploiementProjet(projet string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if p := a.f.Projets[projet]; p != nil {
		return p.CleDeploiement
	}
	return ""
}

// NoterCleDeploiement records (or with "" forgets) the project's deployment key.
func (a *Acces) NoterCleDeploiement(projet, id string) error {
	return a.modifier(func(f *fichierAcces) error {
		if id == "" && f.Projets[projet] == nil {
			return nil
		}
		projetDe(f, projet).CleDeploiement = id
		return nil
	})
}
