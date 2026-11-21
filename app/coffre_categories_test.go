package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// appelCategoriesTest: the routes of s called as the page does; every answer is kept in reponses.
func appelCategoriesTest(t *testing.T, s *Serveur) (appel func(methode, chemin, corps string) (int, map[string]any), reponses *strings.Builder) {
	t.Helper()
	mux := http.NewServeMux()
	s.routes(mux)
	reponses = &strings.Builder{}
	return func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if s.session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: s.session})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		reponses.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}, reponses
}

// nomsMentions: the names of the "mentions" of an answer of GET /api/mentions, sorted.
func nomsMentions(v map[string]any) []string {
	l := []string{}
	for _, m := range v["mentions"].([]any) {
		l = append(l, m.(map[string]any)["type"].(string)+":"+m.(map[string]any)["nom"].(string))
	}
	slices.Sort(l)
	return l
}

// TestCategoriesAPI: a category made, renamed and removed through the routes; a bad name is refused with a fixed
// message, and removing a category removes no API (its entries go back to "no category").
func TestCategoriesAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	appel, reponses := appelCategoriesTest(t, s)
	temoin := "TEMOIN-" + aleatoire(12)

	if code, v := appel("GET", "/api/coffre/categories", ""); code != 200 || len(v["categories"].([]any)) != 0 {
		t.Fatalf("no category yet: %d %v", code, v)
	}
	code, v := appel("POST", "/api/coffre/categories", `{"nom":"  DNS "}`)
	dns, _ := v["id"].(string)
	if code != 200 || dns == "" || v["nom"] != "DNS" {
		t.Fatalf("made: %d %v", code, v)
	}
	for corps, attendu := range map[string]erreurEntree{
		`{"nom":"dns"}`: errCategoriePrise, `{"nom":"   "}`: errCategorieNom, `{}`: errCategorieNom,
		`{"nom":"` + strings.Repeat("é", maxNomCategorie+1) + `"}`: errCategorieNomLong, `{`: errCoffreRequete,
	} {
		if code, v := appel("POST", "/api/coffre/categories", corps); code != http.StatusBadRequest || v["erreur"] != attendu.Error() {
			t.Errorf("%s: %d %v, expected %q", corps, code, v, attendu)
		}
	}
	code, v = appel("POST", "/api/coffre/categories", `{"nom":"`+strings.Repeat("é", maxNomCategorie)+`"}`)
	longue, _ := v["id"].(string)
	if code != 200 || longue == "" || longue == dns {
		t.Fatalf("50 characters: %d %v", code, v)
	}

	// two API in the category, one outside; an unknown category adds nothing
	ajouter := func(nom, categorie string) (int, map[string]any) {
		b, _ := json.Marshal(map[string]string{"nom": nom, "famille": "secret", "type": "jeton", "portee": "code", "valeur": temoin, "categorie": categorie})
		return appel("POST", "/api/coffre", string(b))
	}
	for _, nom := range []string{"CF_TOKEN", "OVH_TOKEN"} {
		if code, v := ajouter(nom, dns); code != 200 || v["categorie"] != dns {
			t.Fatalf("%s in the category: %d %v", nom, code, v)
		}
	}
	if code, v := ajouter("LIBRE", ""); code != 200 || v["categorie"] != nil {
		t.Fatalf("without category: %d %v", code, v)
	}
	if code, v := ajouter("PERDUE", "0123456789abcdef"); code != http.StatusBadRequest || v["erreur"] != errCategorieInconnue.Error() || len(s.coffre.Liste()) != 3 {
		t.Fatalf("unknown category: %d %v, %d entries", code, v, len(s.coffre.Liste()))
	}

	// renamed: its own name in another case is fine, another category's name is not
	if code, v := appel("POST", "/api/coffre/categories/"+dns, `{"nom":"dns"}`); code != 200 || v["id"] != dns || v["nom"] != "dns" {
		t.Fatalf("renamed to its own name: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/coffre/categories/"+dns, `{"nom":"Hébergement"}`); code != 200 || v["nom"] != "Hébergement" {
		t.Fatalf("renamed: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/coffre/categories/"+longue, `{"nom":"hébergement"}`); code != http.StatusBadRequest || v["erreur"] != errCategoriePrise.Error() {
		t.Fatalf("renamed to a taken name: %d %v", code, v)
	}
	if code, v := appel("POST", "/api/coffre/categories/0123456789abcdef", `{"nom":"x"}`); code != http.StatusBadRequest || v["erreur"] != errCategorieInconnue.Error() {
		t.Fatalf("unknown category renamed: %d %v", code, v)
	}
	if l := s.coffre.Categories(); len(l) != 2 || l[0] != (CategorieCoffre{dns, "Hébergement"}) {
		t.Fatalf("categories: %+v", l)
	}

	// removed: the three API stay, none has a category left; the file on disk says the same
	if code, _ := appel("DELETE", "/api/coffre/categories/"+dns, ""); code != 200 {
		t.Fatalf("removed: %d", code)
	}
	if code, _ := appel("DELETE", "/api/coffre/categories/"+dns, ""); code != http.StatusBadRequest {
		t.Fatalf("removed twice: %d", code)
	}
	relu, err := ouvrirCoffre(s.coffre.chemin, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []*Coffre{s.coffre, relu} {
		l := c.Liste()
		if len(l) != 3 || slices.ContainsFunc(l, func(e EntreeCoffre) bool { return e.Categorie != "" }) || len(c.Categories()) != 1 || c.Categories()[0].ID != longue {
			t.Fatalf("after the removal: %+v %+v", l, c.Categories())
		}
	}
	if v, err := s.coffre.Valeur(s.coffre.Liste()[0].ID); err != nil || v != temoin {
		t.Fatalf("value after the removal: %v", err)
	}
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("the witness value is in an answer")
	}
}

// TestAPIDansCategorie: an API put in a category, moved and taken out through POST /api/coffre/{id}, with the
// vault closed (no key is needed); its value and its other fields do not move, an unknown category changes nothing.
func TestAPIDansCategorie(t *testing.T) {
	s := serveurCoffreTest(t)
	appel, reponses := appelCategoriesTest(t, s)
	temoin := "TEMOIN-" + aleatoire(12)
	e, err := s.coffre.Ajouter(EntreeCoffre{Nom: "STRIPE_KEY", Famille: "secret", Type: "stripe", Portee: "code", Note: "paiement"}, temoin)
	if err != nil {
		t.Fatal(err)
	}
	dns, _ := s.coffre.PoserCategorie("", "DNS")
	paiement, err := s.coffre.PoserCategorie("", "Paiement")
	if err != nil {
		t.Fatal(err)
	}
	lue := func() EntreeCoffre {
		l := s.coffre.Liste()
		return l[slices.IndexFunc(l, func(x EntreeCoffre) bool { return x.ID == e.ID })]
	}

	s.coffre.Verrouiller() // sorting the vault asks for no key
	if code, v := appel("POST", "/api/coffre/"+e.ID, `{"categorie":"`+paiement.ID+`"}`); code != 200 || v["categorie"] != paiement.ID || v["nom"] != "STRIPE_KEY" || v["valeur"] != "" {
		t.Fatalf("put in a category: %d %v", code, v)
	}
	if x := lue(); x.Categorie != paiement.ID || !x.Modifie.Equal(e.Modifie) || x.Note != "paiement" {
		t.Fatalf("entry: %+v", x)
	}
	if code, v := appel("POST", "/api/coffre/"+e.ID, `{"categorie":"`+dns.ID+`"}`); code != 200 || v["categorie"] != dns.ID {
		t.Fatalf("moved: %d %v", code, v)
	}
	// an unknown category: refused before anything changes, the note sent with it included
	if code, v := appel("POST", "/api/coffre/"+e.ID, `{"note":"autre","categorie":"0123456789abcdef"}`); code != http.StatusBadRequest || v["erreur"] != errCategorieInconnue.Error() {
		t.Fatalf("unknown category: %d %v", code, v)
	}
	if x := lue(); x.Categorie != dns.ID || x.Note != "paiement" {
		t.Fatalf("after the refusal: %+v", x)
	}
	if code, _ := appel("POST", "/api/coffre/0123456789abcdef", `{"categorie":"`+dns.ID+`"}`); code != http.StatusNotFound {
		t.Fatalf("unknown entry: %d", code)
	}

	// the key open again: the value is the same, and changing the name or the note keeps the category
	if err := s.coffre.OuvrirCode(); err != nil {
		t.Fatal(err)
	}
	if v, err := s.coffre.Valeur(e.ID); err != nil || v != temoin {
		t.Fatalf("value: %v", err)
	}
	if code, v := appel("POST", "/api/coffre/"+e.ID, `{"nom":"STRIPE_LIVE","note":"autre"}`); code != 200 || v["categorie"] != dns.ID || lue().Categorie != dns.ID {
		t.Fatalf("renamed entry: %d %v", code, v)
	}
	if v, err := s.coffre.Valeur(e.ID); err != nil || v != temoin {
		t.Fatalf("value after the change: %v", err)
	}
	if code, v := appel("POST", "/api/coffre/"+e.ID, `{"categorie":""}`); code != 200 || v["categorie"] != nil || lue().Categorie != "" {
		t.Fatalf("taken out: %d %v", code, v)
	}
	if code, v := appel("GET", "/api/coffre", ""); code != 200 || len(v["entrees"].([]any)) != 1 {
		t.Fatalf("list: %d %v", code, v)
	}
	if strings.Contains(reponses.String(), temoin) {
		t.Fatal("the witness value is in an answer")
	}
}

// apiRechercheTest: a vault with API in two categories and without one, and two ssh keys, one of them a server's.
func apiRechercheTest(t *testing.T, s *Serveur, temoin string) (ids map[string]string, dns, hebergement CategorieCoffre) {
	t.Helper()
	dns, _ = s.coffre.PoserCategorie("", "DNS")
	hebergement, err := s.coffre.PoserCategorie("", "Hébergement")
	if err != nil {
		t.Fatal(err)
	}
	ids = map[string]string{}
	for _, e := range []EntreeCoffre{
		{Nom: "CLOUDFLARE_TOKEN", Type: "cloudflare", Categorie: dns.ID}, {Nom: "OVH_KEY", Type: "jeton", Categorie: hebergement.ID},
		{Nom: "STRIPE_KEY", Type: "stripe"}, {Nom: "SSH_WEB", Type: "ssh"}, {Nom: "SSH_LIBRE", Type: "ssh"},
	} {
		e.Famille, e.Portee = "secret", "code"
		x, err := s.coffre.Ajouter(e, temoin)
		if err != nil {
			t.Fatal(err)
		}
		ids[e.Nom] = x.ID
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "srv1", Nom: "web-1", Hote: "203.0.113.5", Cle: ids["SSH_WEB"]}), nil
	}); err != nil {
		t.Fatal(err)
	}
	return ids, dns, hebergement
}

// TestRechercheAPI: GET /api/mentions finds an API by its name, its type and the name of its category, and
// filters by category (an id, or "sans"); the ssh key a server is reached with is not an API.
func TestRechercheAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	appel, reponses := appelCategoriesTest(t, s)
	temoin := "TEMOIN-" + aleatoire(12)
	ids, dns, hebergement := apiRechercheTest(t, s, temoin)
	liste := func(requete string) []string {
		t.Helper()
		code, v := appel("GET", "/api/mentions?"+requete, "")
		if code != 200 {
			t.Fatalf("%s: %d", requete, code)
		}
		return nomsMentions(v)
	}
	for requete, attendu := range map[string][]string{
		"type=api":                       {"api:CLOUDFLARE_TOKEN", "api:OVH_KEY", "api:SSH_LIBRE", "api:STRIPE_KEY"},
		"type=cle":                       {"api:CLOUDFLARE_TOKEN", "api:OVH_KEY", "api:SSH_LIBRE", "api:STRIPE_KEY"}, // the old types, read as api
		"type=fournisseur":               {"api:CLOUDFLARE_TOKEN", "api:OVH_KEY", "api:SSH_LIBRE", "api:STRIPE_KEY"},
		"":                               {"api:CLOUDFLARE_TOKEN", "api:OVH_KEY", "api:SSH_LIBRE", "api:STRIPE_KEY", "serveur:web-1"},
		"q=ovh":                          {"api:OVH_KEY"},                     // by name
		"q=STRIPE":                       {"api:STRIPE_KEY"},                  // by name or type, any case
		"q=dns":                          {"api:CLOUDFLARE_TOKEN"},            // by the name of its category
		"q=H%C3%89BERG":                  {"api:OVH_KEY"},                     // accents and case
		"q=ssh":                          {"api:SSH_LIBRE"},                   // SSH_WEB is the server's
		"q=web":                          {"serveur:web-1"},                   // the server, not its key
		"categorie=" + dns.ID:            {"api:CLOUDFLARE_TOKEN"},            // the filter: API only, the server is left out
		"categorie=sans":                 {"api:SSH_LIBRE", "api:STRIPE_KEY"}, // without category
		"categorie=sans&q=stripe":        {"api:STRIPE_KEY"},                  // the filter and the search together
		"categorie=" + dns.ID + "&q=ovh": {},                                  // in another category
		"categorie=0123456789abcdef":     {},                                  // unknown category
		"categorie=" + hebergement.ID + "&type=serveur":                          {},                                        // a category holds API only
		"type=api&sauf=api:" + ids["OVH_KEY"] + "&sauf=cle:" + ids["STRIPE_KEY"]: {"api:CLOUDFLARE_TOKEN", "api:SSH_LIBRE"}, // chips already chosen, old type too
	} {
		if l := liste(requete); !slices.Equal(l, attendu) {
			t.Errorf("%q: %v, expected %v", requete, l, attendu)
		}
	}
	// what the list says of an API: its type, the id and the name of its category, never a value
	_, v := appel("GET", "/api/mentions?q=cloudflare_token", "")
	b, _ := json.Marshal(v["mentions"])
	if attendu := `[{"categorie":"` + dns.ID + `","categorie_nom":"DNS","detail":"cloudflare","nom":"CLOUDFLARE_TOKEN","ref":"` + ids["CLOUDFLARE_TOKEN"] + `","type":"api"}]`; string(b) != attendu {
		t.Errorf("an API in the list: %s, expected %s", b, attendu)
	}
	if strings.Contains(reponses.String(), temoin) || strings.Contains(reponses.String(), `"valeur"`) {
		t.Fatal("a value is in an answer")
	}
}

// TestMentionAPI: an API chip resolves to its vault entry, as the old "cle" and "fournisseur" chips still do
// (a mission card made before M287 sends them); a server's ssh key and an unknown ref are refused.
func TestMentionAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	ids, _, _ := apiRechercheTest(t, s, "TEMOIN-"+aleatoire(12))
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "cf1", Type: "cloudflare", Nom: "Cloudflare perso", Cle: ids["CLOUDFLARE_TOKEN"]},
			Fournisseur{ID: "cf2", Type: "cloudflare", Nom: "Sans clé", Cle: "0123456789abcdef"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	m, err := s.resoudreMentions([]MentionDemandee{{"api", ids["OVH_KEY"]}, {"cle", ids["OVH_KEY"]}, {"cle", ids["STRIPE_KEY"]},
		{"fournisseur", "cf1"}, {"api", ids["CLOUDFLARE_TOKEN"]}, {"serveur", "srv1"}})
	if err != nil || !slices.Equal(m.Cles, []string{ids["OVH_KEY"], ids["STRIPE_KEY"], ids["CLOUDFLARE_TOKEN"]}) || !slices.Equal(m.Serveurs, []string{"srv1"}) || len(m.Projets) != 0 {
		t.Fatalf("resolved: %+v %v", m, err)
	}
	for _, d := range []MentionDemandee{{"api", ids["SSH_WEB"]}, {"cle", ids["SSH_WEB"]}, {"api", "zzz"}, {"api", ""}, {"api", "OVH_KEY"},
		{"fournisseur", "zz"}, {"fournisseur", ""}, {"fournisseur", "cf2"}, {"api", "cf1"}, {"serveur", ids["OVH_KEY"]}, {"categorie", ids["OVH_KEY"]}} {
		if m, err := s.resoudreMentions([]MentionDemandee{d}); !errors.Is(err, errMentionInconnue) {
			t.Errorf("%+v: %+v %v, expected a refusal", d, m, err)
		}
	}
	// a chip already chosen, whatever its type was called, is not offered again
	for _, sauf := range []string{"api:" + ids["CLOUDFLARE_TOKEN"], "cle:" + ids["CLOUDFLARE_TOKEN"], "fournisseur:cf1"} {
		l, _ := s.chercherMentions("cloudflare", "api", "", sauf)
		if len(l) != 0 {
			t.Errorf("sauf=%s: %+v", sauf, l)
		}
	}
	if l, _ := s.chercherMentions("cloudflare", "api", ""); len(l) != 1 || l[0].Type != "api" || l[0].Ref != ids["CLOUDFLARE_TOKEN"] {
		t.Errorf("list: %+v", l)
	}
}

// fichiersTest: every file under dossier but those named sauf, by its path.
func fichiersTest(t *testing.T, dossier string, sauf ...string) map[string][]byte {
	t.Helper()
	l := map[string][]byte{}
	if err := filepath.WalkDir(dossier, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || slices.Contains(sauf, d.Name()) {
			return err
		}
		l[p], err = os.ReadFile(p)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return l
}

// TestMigrationFournisseurs: at start-up, once, the token of each provider of fournisseurs.json goes to a
// category "Fournisseurs"; without a provider nothing is made. The vault stays closed, the value still opens
// with the same password, no other file changes (what projects and missions refer to), nothing is done twice.
func TestMigrationFournisseurs(t *testing.T) {
	// without a provider: no category, and an empty vault still writes no file
	s := serveurCoffreTest(t)
	if len(s.coffre.Categories()) != 0 || !s.coffre.f.FournisseursMigres {
		t.Fatalf("no provider: %+v, done %v", s.coffre.Categories(), s.coffre.f.FournisseursMigres)
	}
	if _, err := os.Stat(s.coffre.chemin); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("vault file of an empty vault: %v", err)
	}

	// a vault from before M287: two provider tokens (one already sorted by the owner), another API, no mark
	motDePasseCoffreTest(t, s)
	temoin := "TEMOIN-" + aleatoire(12)
	perso, _ := s.coffre.PoserCategorie("", "Perso")
	ids := map[string]string{}
	for _, e := range []EntreeCoffre{{Nom: "CF_TOKEN", Type: "cloudflare", Portee: "infra"}, {Nom: "CF_PERSO", Type: "cloudflare", Portee: "infra", Categorie: perso.ID},
		{Nom: "STRIPE_KEY", Type: "stripe", Portee: "code"}} {
		e.Famille = "secret"
		x, err := s.coffre.Ajouter(e, temoin+"-"+e.Nom)
		if err != nil {
			t.Fatal(err)
		}
		ids[e.Nom] = x.ID
	}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "cf1", Type: "cloudflare", Nom: "Cloudflare", Cle: ids["CF_TOKEN"]}, Fournisseur{ID: "cf2", Type: "cloudflare", Nom: "Cloudflare perso", Cle: ids["CF_PERSO"]},
			Fournisseur{ID: "cf3", Type: "cloudflare", Nom: "Clé retirée", Cle: "0123456789abcdef"}), nil
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(fichierFournisseursChemin()) })
	if err := enregistrerMission(Mission{ID: "migr01", Phrase: "x", Etat: "proposee", Politique: "prod", Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{},
		Carte: json.RawMessage(`{"titre":"x","projet":{"nom":"boutique"},"acces":[{"entree":"CF_TOKEN","pour":"boutique","id":"` + ids["CF_TOKEN"] + `"}]}`), Cree: "2026-11-16T10:00:00.000Z"}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.Synchroniser(s.coffre.Liste(), []string{"boutique"}); err != nil {
		t.Fatal(err)
	}
	s.coffre.mu.Lock()
	s.coffre.f.FournisseursMigres = false
	err := s.coffre.enregistrer()
	s.coffre.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	nomCoffre := filepath.Base(s.coffre.chemin)
	avant := fichiersTest(t, dossierAthanor(), nomCoffre)
	if len(avant) < 2 {
		t.Fatalf("files before: %d", len(avant))
	}

	redemarrer := func() {
		t.Helper()
		s.chargerCoffre() // as a start: read from the disk, the infra key closed
		if s.coffreErr != nil {
			t.Fatal(s.coffreErr)
		}
		s.migrerFournisseurs()
	}
	verifier := func(quand string, categories int) {
		t.Helper()
		cats := s.coffre.Categories()
		i := slices.IndexFunc(cats, func(c CategorieCoffre) bool { return c.Nom == categorieFournisseurs })
		if len(cats) != categories || i < 0 {
			t.Fatalf("%s, categories: %+v", quand, cats)
		}
		for _, e := range s.coffre.Liste() {
			attendue := map[string]string{ids["CF_TOKEN"]: cats[i].ID, ids["CF_PERSO"]: perso.ID, ids["STRIPE_KEY"]: ""}
			if c, ok := attendue[e.ID]; !ok || e.Categorie != c {
				t.Fatalf("%s, %s: category %q (known %v)", quand, e.Nom, e.Categorie, ok)
			}
		}
		if len(s.coffre.Liste()) != 3 {
			t.Fatalf("%s: %d entries", quand, len(s.coffre.Liste()))
		}
	}
	redemarrer()
	if _, err := s.coffre.Valeur(ids["CF_TOKEN"]); !errors.Is(err, errCoffreVerrouille) {
		t.Fatalf("the migration opened the infra key: %v", err)
	}
	verifier("first start", 2)
	redemarrer() // a second start: the mark is on the disk, nothing more
	verifier("second start", 2)

	// the same password opens the same values; the old provider chip and the provider of a deployment still resolve
	if err := s.coffre.Deverrouiller("mdp-test"); err != nil {
		t.Fatal(err)
	}
	for nom, id := range ids {
		if v, err := s.coffre.Valeur(id); err != nil || v != temoin+"-"+nom {
			t.Fatalf("value of %s after the migration: %v", nom, err)
		}
	}
	if m, err := s.resoudreMentions([]MentionDemandee{{"fournisseur", "cf1"}}); err != nil || !slices.Equal(m.Cles, []string{ids["CF_TOKEN"]}) {
		t.Fatalf("provider chip: %+v %v", m, err)
	}
	l, _ := lireFournisseurs()
	if f, ok := fournisseurDeploiement(Environnement{APIs: []string{ids["CF_TOKEN"]}}, l); !ok || f.ID != "cf1" {
		t.Fatalf("provider of a deployment: %+v %v", f, ok)
	}
	// no other file moved (providers, mission, accesses), and the witness is in clear in none, the vault included
	apres := fichiersTest(t, dossierAthanor(), nomCoffre)
	for p, b := range avant {
		if !bytes.Equal(apres[p], b) {
			t.Errorf("%s changed", p)
		}
	}
	if len(apres) != len(avant) {
		t.Errorf("%d files, %d before", len(apres), len(avant))
	}
	for p, b := range fichiersTest(t, dossierAthanor()) {
		if bytes.Contains(b, []byte(temoin)) {
			t.Errorf("the witness value is in %s", p)
		}
	}

	// the owner removes the category: it never comes back, his API stay
	i := slices.IndexFunc(s.coffre.Categories(), func(c CategorieCoffre) bool { return c.Nom == categorieFournisseurs })
	if err := s.coffre.SupprimerCategorie(s.coffre.Categories()[i].ID); err != nil {
		t.Fatal(err)
	}
	redemarrer()
	if cats := s.coffre.Categories(); len(cats) != 1 || cats[0].ID != perso.ID || len(s.coffre.Liste()) != 3 {
		t.Fatalf("after the owner's removal: %+v, %d entries", cats, len(s.coffre.Liste()))
	}
}
