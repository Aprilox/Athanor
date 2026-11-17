package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// M299: a project deleted with or without its folder; its deployment, its relay and its settings go with it.
func TestSupprimerProjetDossier(t *testing.T) {
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin)
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	var corbeille []string
	ancienne := corbeilleProjet
	corbeilleProjet = func(d string) error { corbeille = append(corbeille, d); return os.RemoveAll(d) }
	t.Cleanup(func() { corbeilleProjet = ancienne })

	base := t.TempDir()
	home, _ := os.UserHomeDir()
	projet := func(nom, dossier string) Projet {
		p := Projet{Nom: nom, Dossier: dossier}
		s.mu.Lock()
		s.config.Projets = append(s.config.Projets, p)
		s.mu.Unlock()
		if dossier != home {
			ecrireFichier(t, filepath.Join(dossier, "src", "a.txt"), "a", 0o644)
		}
		if err := s.acces.modifier(func(f *fichierAcces) error {
			f.Projets[nom] = &ProjetAcces{Env: "dev", Environnements: map[string]*Environnement{"dev": {}}}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		ecrireRelais(nom, RelaisJalon{Jalon: "M1", Etat: "lance"})
		reglagesProjetsMu.Lock()
		m := lireReglagesProjets()
		m[nom] = map[string]string{"modele": "sonnet"}
		err := ecrireReglagesProjets(m)
		reglagesProjetsMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// reste: what Athanor still holds about the project
	reste := func(nom string) string {
		var r []string
		if _, ok := s.projetParNom(nom); ok {
			r = append(r, "project")
		}
		_ = s.acces.modifier(func(f *fichierAcces) error {
			if f.Projets[nom] != nil {
				r = append(r, "deployment")
			}
			return nil
		})
		if _, ok := lireRelais()[nom]; ok {
			r = append(r, "relay")
		}
		if _, ok := lireReglagesProjets()[nom]; ok {
			r = append(r, "settings")
		}
		return strings.Join(r, ",")
	}
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(cookieSessionTest)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}

	garde := projet("garde", filepath.Join(base, "garde"))
	if got := reste("garde"); got != "project,deployment,relay,settings" {
		t.Fatalf("bench: %s", got)
	}
	if c, v := appel("DELETE", "/api/projets/garde", ""); c != 200 || reste("garde") != "" {
		t.Fatalf("without the folder: %d %v, left: %s", c, v, reste("garde"))
	}
	if _, err := os.Stat(filepath.Join(garde.Dossier, "src", "a.txt")); err != nil || len(corbeille) != 0 {
		t.Fatalf("without the folder, the folder must stay: %v %v", err, corbeille)
	}

	part := projet("part", filepath.Join(base, "part"))
	if c, v := appel("DELETE", "/api/projets/part", `{"dossier":true}`); c != 200 || reste("part") != "" {
		t.Fatalf("with the folder: %d %v, left: %s", c, v, reste("part"))
	}
	if _, err := os.Stat(part.Dossier); !os.IsNotExist(err) || !slices.Equal(corbeille, []string{part.Dossier}) {
		t.Fatalf("with the folder, it goes to the recycle bin: %v %v", err, corbeille)
	}

	// the same route as the page (projets/gestion) refuses the same folders: the personal folder, a link
	projet("perso", home)
	lien := filepath.Join(base, "lien")
	if os.Symlink(garde.Dossier, lien) == nil {
		projet("lien", lien)
	}
	for _, nom := range []string{"perso", "lien"} {
		if _, ok := s.projetParNom(nom); !ok {
			continue // no right to create a link on this computer
		}
		if c, _ := appel("DELETE", "/api/projets/"+nom, `{"dossier":true}`); c != http.StatusBadRequest || reste(nom) != "project,deployment,relay,settings" {
			t.Errorf("%s: forbidden folder accepted by DELETE: %d, left: %s", nom, c, reste(nom))
		}
		if c, _ := appel("POST", "/api/projets/gestion", `{"nom":"`+nom+`","action":"supprimer","mode":"dossier"}`); c != http.StatusBadRequest || reste(nom) != "project,deployment,relay,settings" {
			t.Errorf("%s: forbidden folder accepted by the page: %d, left: %s", nom, c, reste(nom))
		}
	}
	if _, err := os.Stat(filepath.Join(garde.Dossier, "src", "a.txt")); err != nil || len(corbeille) != 1 {
		t.Fatalf("a refused folder went: %v %v", err, corbeille)
	}

	actif := projet("actif", filepath.Join(base, "actif"))
	ecrireFichier(t, filepath.Join(actif.Atelier(), "pilot.pid"), "1", 0o644)
	for _, corps := range []string{"", `{"dossier":true}`} {
		if c, _ := appel("DELETE", "/api/projets/actif", corps); c != http.StatusBadRequest || reste("actif") != "project,deployment,relay,settings" {
			t.Fatalf("active pilot %q: %d, left: %s", corps, c, reste("actif"))
		}
	}
	if _, err := os.Stat(actif.Dossier); err != nil || len(corbeille) != 1 {
		t.Fatalf("active pilot: the folder went: %v %v", err, corbeille)
	}
}
