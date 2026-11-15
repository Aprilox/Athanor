package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// TestSceneVerrouSecurite (M237): the opening scene is client-side only. Locked, every data route is
// refused whether the scene is on or off; /api/etat says only "locked"; the scene's files are static and
// hold no secret; the password never reaches the URL, the audit log nor the server log.
func TestSceneVerrouSecurite(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	s.routesInterface(mux)
	var journal bytes.Buffer
	ancienne := log.Writer()
	log.SetOutput(&journal)
	t.Cleanup(func() { log.SetOutput(ancienne) })

	temoinProjet := "projet-temoin-" + aleatoire(4)
	mdp := "mdp-scene-" + aleatoire(6) // built at run time: no fake secret in the repository
	s.config.MotDePasseHash, s.config.ProjetActif, s.session = hacherMotDePasse(mdp), temoinProjet, ""
	appel := func(methode, chemin, corps string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	for _, animation := range []bool{true, false} { // scene on (data-scene) or plain lock: the same refusals
		s.config.PasAnimation = !animation
		for _, route := range []string{"/api/projets", "/api/hub", "/api/coffre", "/api/journal", "/api/acces"} {
			if code, corps := appel("GET", route, ""); code != http.StatusUnauthorized || strings.Contains(corps, temoinProjet) {
				t.Errorf("locked, animation %v: %s answered %d %s", animation, route, code, corps)
			}
		}
		code, corps := appel("GET", "/api/etat", "")
		var v map[string]any
		_ = json.Unmarshal([]byte(corps), &v)
		if code != 200 || v["verrouille"] != true || v["projets"] != nil || strings.Contains(corps, temoinProjet) || strings.Contains(corps, s.config.Kit) {
			t.Errorf("locked /api/etat (animation %v) says more than locked: %d %s", animation, code, corps)
		}
		code, page := appel("GET", "/", "")
		lancement := regexp.MustCompile(`<meta name="lancement" content="([^"]*)">`).FindStringSubmatch(page)
		if code != 200 || lancement == nil || (lancement[1] != "") == !animation || lancement[1] != "" && lancement[1] != idLancement {
			t.Errorf("page (animation %v): %d launch %v", animation, code, lancement)
		}
		if strings.Contains(page, temoinProjet) {
			t.Error("the page names a project before the password")
		}
	}

	// the scene's files: served as embedded, the same for everyone (no token, path nor project put in them)
	sous, _ := fs.Sub(statique, "static")
	for _, f := range []string{"splash.js", "app.css", "logo.svg"} {
		b, err := fs.ReadFile(sous, f)
		if err != nil {
			t.Fatal(err)
		}
		if code, corps := appel("GET", "/"+f, ""); code != 200 || corps != string(b) {
			t.Errorf("%s not served as the static file: %d", f, code)
		}
	}
	page, _ := fs.ReadFile(sous, "index.html")
	form := regexp.MustCompile(`<form id="verrou-form"[^>]*>`).FindString(string(page))
	if form == "" || strings.Contains(form, "action") || strings.Contains(form, "method") || !strings.Contains(string(page), `<input type="password" id="verrou-mdp"`) {
		t.Errorf("lock form: %q (a GET form would put the password in the URL)", form)
	}

	// a wrong then the right password: never in the URL, the answers, the audit log nor the server log
	if code, corps := appel("POST", "/api/deverrouiller", `{"mot_de_passe":"`+mdp+`-faux"}`); code != http.StatusUnauthorized || strings.Contains(corps, mdp) {
		t.Errorf("wrong password: %d %s", code, corps)
	}
	if code, _ := appel("GET", "/api/projets", ""); code != http.StatusUnauthorized {
		t.Error("a wrong password opened a session")
	}
	if code, corps := appel("POST", "/api/deverrouiller", `{"mot_de_passe":"`+mdp+`"}`); code != 200 || strings.Contains(corps, mdp) || s.session == "" {
		t.Errorf("right password: %d %s", code, corps)
	}
	if code, _ := appel("GET", "/api/deverrouiller?mot_de_passe="+mdp, ""); code == 200 {
		t.Error("the password is accepted in the URL")
	}
	if strings.Contains(journal.String(), mdp) || strings.Contains(journalAuditTexte(t), mdp) {
		t.Error("the password is in a log")
	}
}
