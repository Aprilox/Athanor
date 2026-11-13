package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestArchiveLiensEnChaine (B75): links are followed as the server will, chains included; each one must stay
// inside the release, and so must every entry reached through one.
func TestArchiveLiensEnChaine(t *testing.T) {
	sain := [][2]string{{"site/index.html", ""}, {"site/x/y/b", "../.."}, {"site/c", "x/y/b/index.html"}, {"site/d", "x/y/b/x/y/b/c"}}
	if err := verifierArchive(archiveDeploiementTest(t, sain), "site"); err != nil {
		t.Fatalf("sound chain refused: %v", err)
	}
	for _, c := range []struct {
		nom      string
		entrees  [][2]string
		attendus string
	}{
		// the case of B75: each link alone looks inside, together c is /
		{"B75 chain", [][2]string{{"site/index.html", ""}, {"site/x/y/z/w/b", "../../../../.."}, {"site/c", "x/y/z/w/b/../../../../../.."}}, "points out"},
		{"chain in two links", [][2]string{{"site/a", "."}, {"site/b", "a/../.."}, {"site/c", "b/.."}}, "points out"},
		{"link declared after its use", [][2]string{{"site/c", "l/../.."}, {"site/l", "x/y/../../.."}}, "points out"},
		{"loop", [][2]string{{"site/a", "b"}, {"site/b", "a"}}, "points out"},
		{"file through a link", [][2]string{{"site/l/f", ""}, {"site/l", "../.."}}, "leaves the release through a link"},
		{"absolute link", [][2]string{{"site/l", "/etc"}}, "points out"},
	} {
		if err := verifierArchive(archiveDeploiementTest(t, c.entrees), ""); err == nil || !strings.Contains(err.Error(), c.attendus) {
			t.Errorf("%s: %v", c.nom, err)
		}
	}
	// the servers check again once extracted (extraireVersion): every link resolved under the release
	if !strings.Contains(extraireVersion, `find "$r" -type l -exec readlink -m`) || !strings.Contains(extraireVersion, `"$rr"|"$rr"/*`) {
		t.Error("extraireVersion lost its check of the links")
	}
}

// TestDossierPublieBorne (B82): 300 characters at most (Go and the deployer tool's schema), never a part
// starting with "-".
func TestDossierPublieBorne(t *testing.T) {
	cfg := func(d string) ConfigDeploiement {
		return ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: d}
	}
	for _, d := range []string{"", "site", "a/b_c/d-e.f", strings.Repeat("a", maxDossierPublie)} {
		if err := validerConfigDeploiement(cfg(d)); err != nil {
			t.Errorf("%q refused: %v", d, err)
		}
	}
	for _, d := range []string{strings.Repeat("a", maxDossierPublie+1), strings.Repeat("a/", 3000) + "a", "-rf", "--x", "-", "site/-x", "../x", ".git", "a b"} {
		if err := validerConfigDeploiement(cfg(d)); err == nil {
			t.Errorf("%.40q accepted", d)
		}
	}
	var schema struct {
		Properties map[string]struct {
			MaxLength int `json:"maxLength"`
		} `json:"properties"`
	}
	b, _ := json.Marshal(outilsHub["deployer"].Schema)
	if err := json.Unmarshal(b, &schema); err != nil || schema.Properties["dossier"].MaxLength != maxDossierPublie {
		t.Errorf("deployer's schema: %v %s", err, b)
	}
}

// TestHistoriqueMasque (B77): the error a deployment ends with is masked (vault values) before the history,
// which the environment cards and the paired phone read.
func TestHistoriqueMasque(t *testing.T) {
	s := serveurBusTest(t)
	if err := s.coffre.ChangerMotDePasse("", "mdp-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	temoin := "temoin" + aleatoire(6)
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "DEPLOY_TOKEN", Famille: "secret", Type: "autre"}, temoin); err != nil {
		t.Fatal(err)
	}
	depot, c := depotEssai(t, map[string]string{"site/index.html": "v1"})
	s.config.Projets[0].Dossier = depot
	if err := s.acces.DefinirEnvironnement("demo", "dev", Environnement{URL: "https://dev.m148.test", Serveurs: []string{"s1"}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	// the published folder named as the witness: the engine's error repeats it, as a server repeats its input
	d, err := s.deployer(context.Background(), "demo", "dev", c[0], OptionsDeploiement{Auteur: "agent", Accorde: true,
		Config: &ConfigDeploiement{Methode: "statique", Source: "artefact", Dossier: temoin}})
	if err == nil || !strings.Contains(err.Error(), temoin) {
		t.Fatalf("the witness's failure: %v", err)
	}
	if d.Etat != "echec" || strings.Contains(d.Erreur, temoin) {
		t.Errorf("deployment returned with the value: %+v", d)
	}
	h := historiqueDeploiements("demo", "dev") // its Dossier is the configuration, not what a server said
	if len(h) != 1 || !strings.Contains(h[0].Erreur, "no folder") || strings.Contains(h[0].Erreur, temoin) {
		t.Errorf("history: %+v", h)
	}
}
