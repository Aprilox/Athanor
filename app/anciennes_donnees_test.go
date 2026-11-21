package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestAnciennesDonneesLisibles (M321, verification §5): what an older installation left, all at once (the config,
// acces.json with its three environments and an e-mail password granted per environment, the servers' inventory
// with roles, a vault with a two-value entry and no "Fournisseurs" category, fournisseurs.json, missions running, paused and finished) is read whole by a start of today's server and
// its reading pages. No file is rewritten, the vault apart: its provider token is sorted once, and nothing of it
// is lost. No value of the vault shows in an answer nor in a file.
func TestAnciennesDonneesLisibles(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	_ = os.Remove(s.acces.chemin) // files left next to the test binary by an interrupted run
	for _, f := range []string{s.acces.chemin, dossierMissions(), dossierModule("servers")} {
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	_ = os.RemoveAll(dossierMissions())

	// the vault, made by today's code then put back in its older form (as vieillir of essai_m312.mjs)
	sfx := aleatoire(6)
	tEssai, tProd, tCF, tSMTP := "sk_test_temoin"+sfx, "sk_live_temoin"+sfx, "cf-temoin-"+sfx, "smtp-temoin-"+sfx
	stripe := ajouter(t, s.coffre, "STRIPE_SECRET_KEY", "secret", "stripe", "code", tEssai)
	if err := s.coffre.PoserValeurProd(stripe.ID, tProd); err != nil {
		t.Fatal(err)
	}
	cf := ajouter(t, s.coffre, "CF_TOKEN", "secret", "cloudflare", "infra", tCF)
	smtp := ajouter(t, s.coffre, "SMTP_PASSWORD", "secret", "smtp", "code", tSMTP)
	var brut map[string]any
	b, err := os.ReadFile(s.coffre.chemin)
	if err != nil || json.Unmarshal(b, &brut) != nil {
		t.Fatal("the vault file:", err)
	}
	delete(brut, "fournisseurs_migres")
	delete(brut, "categories")
	for _, e := range brut["entrees"].([]any) {
		delete(e.(map[string]any), "categorie")
	}
	b, _ = json.MarshalIndent(brut, "", "  ")
	if err := os.WriteFile(s.coffre.chemin, b, 0o600); err != nil {
		t.Fatal(err)
	}
	var vieux fichierCoffre
	if err := json.Unmarshal(b, &vieux); err != nil || len(vieux.Entrees) != 3 || vieux.FournisseursMigres || vieux.Entrees[0].Prod == "" {
		t.Fatalf("the older vault: %v, %d entries", err, len(vieux.Entrees))
	}

	// every other file, written as text in its older form
	projet := fauxProjet(t, t.TempDir(), "boutique", 3, 1)
	config, _ := json.MarshalIndent(map[string]any{"kit": s.config.Kit, "projets": []Projet{projet}, "projet_actif": "boutique",
		"lancer_pilote_au_demarrage": false, "bureau_virtuel": true, "programmes_supplementaires": "blender.exe",
		"mot_de_passe_hash": s.config.MotDePasseHash, "port": 8765, "langue": "fr", "theme": "sombre", "accent": "vert", "bienvenue_vue": true}, "", "  ")
	grant := func(entree, env, variable string) string {
		return `{"entree":"` + entree + `","env":"` + env + `","variable":"` + variable + `","cree":"2026-10-02T08:15:00Z"}`
	}
	fichiers := map[string]string{
		cheminConfig(): string(config),
		s.acces.chemin: `{"version":1,"projets":{"boutique":{"env":"staging","forcer_infra_natif":false,"acces":[` +
			grant(stripe.ID, "dev", "STRIPE_SECRET_KEY") + "," + grant(stripe.ID, "prod", "STRIPE_SECRET_KEY") + "," +
			grant(smtp.ID, "dev", "SMTP_PASSWORD") + "," + grant(smtp.ID, "prod", "SMTP_PASSWORD") + `],` +
			`"environnements":{"dev":{"url":"https://dev.exemple.fr","serveurs":["b2"],"protection":"auth_proxy","politique":"auto","deployer_au_jalon":true},` +
			`"staging":{"url":"https://pre.exemple.fr","serveurs":["a4"],"protection":"access","politique":"accord"},` +
			`"prod":{"url":"https://exemple.fr","serveurs":["a4"],"protection":"aucune","politique":"accord","fournisseur":"cf1","deploiement":{"methode":"statique","source":"artefact"}}}}},` +
			`"auto_faits":["` + stripe.ID + `","` + cf.ID + `","` + smtp.ID + `"]}`,
		fichierInventaire(): `{"version":1,"serveurs":[` +
			`{"id":"a4","nom":"vps-prod","hote":"203.0.113.5","port":22,"utilisateur":"deploy","cle":"0a1b2c","empreinte_hote":"SHA256:temoin","roles":["prod"],"cree":"2026-10-01T08:00:00Z","etat":"ok","mot_de_passe_coupe":true},` +
			`{"id":"b2","nom":"vps-dev","hote":"203.0.113.6","port":2222,"utilisateur":"dev","cle":"0a1b2d","empreinte_hote":"SHA256:temoin2","roles":["dev"],"cree":"2026-10-01T08:05:00Z","etat":"inconnu","mot_de_passe_coupe":false}]}`,
		fichierFournisseursChemin(): `{"version":1,"fournisseurs":[{"id":"cf1","type":"cloudflare","nom":"Cloudflare","cle":"` + cf.ID + `","compte":"compte-temoin",` +
			`"zones":[{"id":"z1","nom":"exemple.fr","droits":["dns_lire","dns_ecrire"]}],"droits":["dns_lire","dns_ecrire"],"teste":"2026-11-01T10:00:00Z","etat":"ok"}]}`,
	}
	etats := map[string]string{"encours1": "lancee", "enpause1": "en_pause", "finie001": "terminee"}
	for id, etat := range etats {
		fichiers[filepath.Join(dossierMissions(), id+".json")] = missionAncienne(id, etat, "boutique", "2026-11-10T08:00:00.000Z")
	}
	for f, contenu := range fichiers {
		ecrireFichier(t, f, contenu, 0o600)
	}

	// a start of today's server, then its reading pages
	configNeuve.Store(true) // as in TestHubAncienneConfig: this read must not count as a first launch
	c := lireConfig()
	if c.Kit != s.config.Kit || len(c.Projets) != 1 || c.Projets[0] != projet || c.ProjetActif != "boutique" || c.Port != 8765 || c.Langue != "fr" ||
		c.Theme != "sombre" || c.Accent != "vert" || !c.BienvenueVue || !c.BureauVirtuel || c.ProgrammesSupplementaires != "blender.exe" || c.MotDePasseHash != s.config.MotDePasseHash {
		t.Fatalf("the config read: %+v", c)
	}
	s2 := nouveauServeur(c)
	s2.mu.Lock()
	s2.csrf, s2.cleAcces, s2.session = "jeton", "cle", "session"
	s2.mu.Unlock()
	mux := http.NewServeMux()
	s2.routes(mux)
	temoins := []string{tEssai, tProd, tCF, tSMTP}
	sansTemoin := func(ou, texte string) {
		t.Helper()
		for _, v := range temoins {
			if strings.Contains(texte, v) {
				t.Errorf("%s holds a value of the vault in clear", ou)
			}
		}
	}
	for _, u := range []string{"/api/etat", "/api/hub", "/api/projets", "/api/projets/boutique/deploiement", "/api/acces?projet=boutique", "/api/coffre",
		"/api/coffre/categories", "/api/serveurs", "/api/missions"} {
		r := httptest.NewRequest("GET", u, nil)
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: "session"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
			t.Fatalf("GET %s: %d %s", u, w.Code, w.Body.String())
		}
		sansTemoin("GET "+u, w.Body.String())
	}

	// grants, environments (an older prod, and a site on a server with the older prod role, keep their caution)
	p, e := s2.acces.Projet("boutique"), s2.acces.Environnements("boutique")
	if p.Env != "staging" || len(p.Acces) != 4 || p.Acces[1].Entree != stripe.ID || p.Acces[1].Env != "prod" || len(e) != 3 {
		t.Fatalf("grants: %+v, environments: %+v", p, e)
	}
	if d, pr := e["dev"], e["prod"]; d.URL != "https://dev.exemple.fr" || d.Protection != "auth_proxy" || d.Politique != "auto" || !d.DeployerAuJalon || d.Production ||
		!pr.Production || pr.Fournisseur != "cf1" || pr.Deploiement == nil || pr.Deploiement.Methode != "statique" ||
		!e["staging"].Production || !slices.Equal(e["staging"].Serveurs, []string{"a4"}) {
		t.Errorf("environments lost on reading: %+v", e)
	}
	// the e-mail password granted per environment stays an ordinary grant of the vault
	if p.Acces[2].Entree != smtp.ID || p.Acces[3].Variable != "SMTP_PASSWORD" || p.Acces[3].Env != "prod" {
		t.Errorf("e-mail per environment: %+v", p.Acces)
	}
	// the inventory: roles kept, a server from before 3.0.0 is reached with its key
	if l, err := lireServeurs(); err != nil || len(l) != 2 || !slices.Equal(l[0].Roles, []string{"prod"}) || !slices.Equal(l[1].Roles, []string{"dev"}) ||
		l[0].Cle != "0a1b2c" || l[0].EmpreinteHote != "SHA256:temoin" || !l[0].MotDePasseCoupe || l[1].Port != 2222 || l[0].parMotDePasse() || l[0].Autorisation != "" {
		t.Errorf("inventory: %+v, %v", l, err)
	}
	if l, err := lireFournisseurs(); err != nil || len(l) != 1 || l[0].Cle != cf.ID || l[0].Compte != "compte-temoin" || len(l[0].Zones) != 1 ||
		l[0].Zones[0].Nom != "exemple.fr" || !slices.Equal(l[0].Droits, []string{"dns_lire", "dns_ecrire"}) || l[0].Etat != "ok" || l[0].Teste.IsZero() {
		t.Errorf("providers: %+v, %v", l, err)
	}
	for id, etat := range etats {
		m, err := lireMission(id)
		if err != nil || m.Etat != etat || m.Relais == nil || m.Relais.Etape != "accord_prod" || m.Relais.DeploiementDev != "d16" || len(m.Journal) != 1 ||
			len(m.Voies) != 2 || len(m.Historique) != 2 || m.Session == "" || !strings.Contains(string(m.Carte), "Mise en ligne") {
			t.Errorf("mission %s: %v, %+v", id, err, m)
		}
	}

	// the vault: both values and the others still open with the same password; only the sorting is added
	if err := s2.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	for env, attendu := range map[string]string{"dev": tEssai, "staging": tEssai, "prod": tProd} {
		if v, err := s2.coffre.ValeurPour(stripe.ID, env); err != nil || v != attendu {
			t.Errorf("two-value entry for %s: %v", env, err)
		}
	}
	valeurAttendue(t, s2.coffre, cf.ID, tCF)
	valeurAttendue(t, s2.coffre, smtp.ID, tSMTP)
	var neuf fichierCoffre
	if b, err := os.ReadFile(s2.coffre.chemin); err != nil || json.Unmarshal(b, &neuf) != nil {
		t.Fatal("the vault after the start:", err)
	}
	if !neuf.FournisseursMigres || len(neuf.Categories) != 1 || neuf.Categories[0].Nom != categorieFournisseurs || neuf.Entrees[1].Categorie != neuf.Categories[0].ID {
		t.Errorf("the provider token is not sorted once: %+v", neuf.Categories)
	}
	neuf.Entrees[1].Categorie = ""
	if !reflect.DeepEqual(neuf.Entrees, vieux.Entrees) || !reflect.DeepEqual(neuf.KDF, vieux.KDF) || !reflect.DeepEqual(neuf.Cles, vieux.Cles) {
		t.Error("the vault lost or changed an entry or a key at the start")
	}

	// nothing rewritten, no value in clear in any file of Athanor's folder (audit log included)
	for f, contenu := range fichiers {
		if lu, err := os.ReadFile(f); err != nil || string(lu) != contenu {
			t.Errorf("%s was rewritten or lost (%v)", f, err)
		}
	}
	for f, contenu := range fichiersTest(t, dossierAthanor()) {
		sansTemoin(f, string(contenu))
	}
}
