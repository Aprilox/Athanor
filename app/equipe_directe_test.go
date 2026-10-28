package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// The direct link between two computers in one process: a master (its own listener, no tunnel in the
// tests) and a node that pairs with a code, beats, sees the whole team; a wrong or used code refused,
// a removed node told so, a master's new address found in its encrypted presence; the master's listener
// answers nothing of the application.
func TestEquipeDirecte(t *testing.T) {
	sansTunnelDirect = true
	defer func() { sansTunnelDirect = false }()
	serveur := func(nom string) (*Serveur, Projet, func(methode, corps string) (int, map[string]any)) {
		d := t.TempDir()
		p := Projet{Nom: nom, Dossier: filepath.Join(d, nom)}
		ecrire(t, p.Plan(), planTest)
		s := nouveauServeur(Config{Kit: filepath.Join(d, "kit"), Projets: []Projet{p}, ProjetActif: nom})
		s.csrf = "jeton"
		mux := http.NewServeMux()
		s.routes(mux)
		appel := func(methode, corps string) (int, map[string]any) {
			r := httptest.NewRequest(methode, "/api/pcs-direct?p="+nom, strings.NewReader(corps))
			r.Header.Set("X-Athanor", "jeton")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			var v map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &v)
			return w.Code, v
		}
		// each one its own computer (in the tests, the machine is the same for both)
		pcsMu.Lock()
		pcsCache[nom] = &cachePCs{vue: VuePCs{Actif: true, Moi: &PresencePC{ID: "id-" + nom, Nom: "PC " + nom, Pilote: true, Jalon: "M2"}}, quand: time.Now()}
		pcsMu.Unlock()
		return s, p, appel
	}
	sm, pm, appelM := serveur("alpha")
	sn, pn, appelN := serveur("beta")
	defer sm.arreterMaitre(pm.Nom)
	vue := func(v map[string]any) map[string]any { d, _ := v["direct"].(map[string]any); return d }

	if c, v := appelM("POST", `{"action":"inviter"}`); c != 200 || vue(v)["code"] == "" {
		t.Fatalf("invite: %d %v", c, v)
	}
	var adresse, code string
	for i := 0; i < 50; i++ {
		_, v := appelM("GET", "")
		adresse, _ = vue(v)["adresse"].(string)
		code, _ = vue(v)["code"].(string)
		if adresse != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.HasPrefix(adresse, "http://127.0.0.1:") || len(code) != 6 {
		t.Fatalf("master: %q %q", adresse, code)
	}
	// the master's listener: the team's calls only, never the application
	for _, chemin := range []string{"/", "/api/etat", "/api/pcs-direct"} {
		if r, err := http.Get(adresse + chemin); err != nil || r.StatusCode != 404 {
			t.Fatalf("%s through the team's listener: %v %v", chemin, r, err)
		}
	}
	if r, _ := http.Get(adresse + "/equipe/noeuds"); r.StatusCode != 401 {
		t.Fatalf("the team's table without a token: %d", r.StatusCode)
	}
	// a wrong code, then the right one, then the same again
	faux := "000000"
	if code == faux {
		faux = "111111"
	}
	if c, v := appelN("POST", `{"action":"rejoindre","invitation":"`+adresse+`#`+faux+`"}`); c != 400 || !strings.Contains(v["erreur"].(string), "wrong or expired") {
		t.Fatalf("wrong code: %d %v", c, v)
	}
	// the right code: the master still has to say yes on its screen
	if c, v := appelN("POST", `{"action":"rejoindre","invitation":"`+adresse+`#`+code+`"}`); c != 200 || vue(v)["etat"] != "attente" {
		t.Fatalf("join: %d %v", c, v)
	}
	if e := sn.lireDirect(pn); e.Role != "attente" || e.Cle != "" {
		t.Fatalf("waiting: %+v", e)
	}
	if r, _ := http.Post(adresse+"/equipe/appairer/statut", "application/json", strings.NewReader(`{"demande":"x"}`)); r.StatusCode != 404 {
		t.Fatalf("an unknown request: %d", r.StatusCode)
	}
	sn.attenteAccord(pn)
	if e := sn.lireDirect(pn); e.Role != "attente" {
		t.Fatalf("still waiting: %+v", e)
	}
	_, v0 := appelM("GET", "")
	demandes, _ := vue(v0)["demandes"].([]any)
	if len(demandes) != 1 || demandes[0].(map[string]any)["nom"] != "PC beta" {
		t.Fatalf("the master's requests: %v", v0)
	}
	ref, _ := demandes[0].(map[string]any)["ref"].(string)
	if strings.Contains(string(jsonDirect(v0)), sn.lireDirect(pn).Jeton) {
		t.Fatal("the request's secret on the master's page")
	}
	if c, _ := appelM("POST", `{"action":"accepter","id":"nope"}`); c != 400 {
		t.Fatal("accept an unknown request")
	}
	if c, v := appelM("POST", `{"action":"accepter","id":"`+ref+`"}`); c != 200 || vue(v)["demandes"] != nil {
		t.Fatalf("accept: %d %v", c, v)
	}
	sn.attenteAccord(pn)
	if c, v := appelN("GET", ""); c != 200 || vue(v)["etat"] != "connecte" || vue(v)["maitre"] != "PC alpha" {
		t.Fatalf("accepted: %d %v", c, v)
	}
	if c, _ := appelN("POST", `{"action":"rejoindre","invitation":"`+adresse+` `+code+`"}`); c != 400 {
		t.Fatalf("a code used twice: %d", c)
	}
	if e := sn.lireDirect(pn); e.Role != "noeud" || e.Jeton == "" || len(e.Cle) < 40 {
		t.Fatalf("node's file: %+v", e)
	}
	if fi, _ := os.Stat(sn.fichierDirect(pn)); fi.Mode().Perm() != 0o600 && fi.Mode().Perm() != 0o666 {
		t.Errorf("node's file mode: %v", fi.Mode())
	}
	// both see the whole team, with the node's stats
	_, v := appelN("GET", "")
	noeuds, _ := vue(v)["noeuds"].([]any)
	if len(noeuds) != 2 || noeuds[0].(map[string]any)["maitre"] != true || noeuds[1].(map[string]any)["presence"].(map[string]any)["nom"] != "PC beta" {
		t.Fatalf("node's table: %v", v)
	}
	_, v = appelM("GET", "")
	noeuds, _ = vue(v)["noeuds"].([]any)
	if len(noeuds) != 2 || noeuds[1].(map[string]any)["en_direct"] != true || noeuds[1].(map[string]any)["presence"].(map[string]any)["jalon"] != "M2" {
		t.Fatalf("master's table: %v", v)
	}
	if e := sm.lireDirect(pm); len(e.Noeuds) != 1 || strings.Contains(string(jsonDirect(e)), sn.lireDirect(pn).Jeton) {
		t.Fatalf("the master keeps the node, its token hashed: %+v", e)
	}
	// the team's workshop through the link, and the master's orders
	var muEssai sync.Mutex
	var suivis, horsGit []string
	ancienP, ancienH := piloterOrdre, atelierHorsGitFn
	piloterOrdre = func(_ *Serveur, p Projet, a string) error {
		muEssai.Lock()
		suivis = append(suivis, p.Nom+":"+a)
		muEssai.Unlock()
		return nil
	}
	atelierHorsGitFn = func(_ *Serveur, p Projet, h bool) error {
		muEssai.Lock()
		horsGit = append(horsGit, fmt.Sprintf("%s:%v", p.Nom, h))
		muEssai.Unlock()
		return nil
	}
	defer func() { piloterOrdre, atelierHorsGitFn = ancienP, ancienH }()
	vu := func(l *[]string, x string) bool {
		for i := 0; i < 100; i++ {
			muEssai.Lock()
			ok := strings.Contains(strings.Join(*l, ","), x)
			muEssai.Unlock()
			if ok {
				return true
			}
			time.Sleep(20 * time.Millisecond)
		}
		return false
	}
	if r, _ := http.Get(adresse + "/equipe/atelier"); r.StatusCode != 401 {
		t.Fatalf("the workshop without a token: %d", r.StatusCode)
	}
	ecrire(t, filepath.Join(pm.Dossier, ".athanor", "REQUEST.md"), "# Request\n\nBuild it.\n")
	if c, v := appelN("POST", `{"action":"atelier","valeur":true}`); c != 400 {
		t.Fatalf("a node deciding the workshop: %d %v", c, v)
	}
	c, v0 := appelM("POST", `{"action":"atelier","valeur":true}`)
	if at, _ := vue(v0)["atelier"].(map[string]any); c != 200 || vue(v0)["atelier_lien"] != true || at == nil || at["version"] != 1.0 || at["fichiers"] != 2.0 {
		t.Fatalf("the workshop through the link: %d %v", c, v0)
	}
	if !vu(&horsGit, "alpha:true") {
		t.Fatal("the master's .athanor/ not taken out of git")
	}
	sn.battementNoeud(pn)
	if e := sn.lireDirect(pn); !e.AtelierLien || !vu(&horsGit, "beta:true") {
		t.Fatalf("the node did not follow the workshop's move: %+v", e)
	}
	eN, ok := equipeDirectePilote(sn.priveProjet(pn))
	if !ok {
		t.Fatal("the node's pilot does not see the workshop through the link")
	}
	mN, nomN := magasinPilote(sn.priveProjet(pn), eN)
	baseN := filepath.Join(sn.priveProjet(pn), "atelier-base", nomN)
	if r, err := synchroniserAtelier(pn.Atelier(), baseN, mN, fusionAtelierGit, "PC beta", ""); err != nil || !strings.Contains(strings.Join(r.Recus, ","), "REQUEST.md") {
		t.Fatalf("the node's workshop through the link: %+v %v", r, err)
	}
	if b, _ := os.ReadFile(filepath.Join(pn.Atelier(), "REQUEST.md")); !strings.Contains(string(b), "Build it.") {
		t.Fatal("the master's request did not reach the node")
	}
	ecrire(t, filepath.Join(pn.Atelier(), "LOG.md"), "- M1 by beta\n")
	if r, err := synchroniserAtelier(pn.Atelier(), baseN, mN, fusionAtelierGit, "PC beta", ""); err != nil || r.Version != 2 {
		t.Fatalf("the node gives its log: %+v %v", r, err)
	}
	if v, _ := sm.magasinProjet(pm).dernier(); v.Par != "PC beta" || v.Fichiers["LOG.md"] == "" {
		t.Fatalf("the store after the node: %+v", v)
	}
	// the master starts the team: its own pilot, then the node's at its beat (once)
	if c, v := appelM("POST", `{"action":"demarrer-equipe"}`); c != 200 || vue(v)["ordre"] == nil {
		t.Fatalf("start the team: %d %v", c, v)
	}
	if !vu(&suivis, "alpha:demarrer") {
		t.Fatal("the master's own pilot not started")
	}
	sn.battementNoeud(pn)
	sn.battementNoeud(pn)
	if !vu(&suivis, "beta:demarrer") {
		t.Fatal("the node did not follow the start")
	}
	time.Sleep(100 * time.Millisecond)
	muEssai.Lock()
	if n := strings.Count(strings.Join(suivis, ","), "beta:demarrer"); n != 1 {
		t.Fatalf("the node started %d times", n)
	}
	muEssai.Unlock()
	// the node's user says no: the master's stop is not followed there
	if c, v := appelN("POST", `{"action":"ordres","valeur":false}`); c != 200 || vue(v)["accepte_ordres"] == true {
		t.Fatalf("orders refused: %d %v", c, v)
	}
	appelM("POST", `{"action":"arreter-equipe"}`)
	sn.battementNoeud(pn)
	time.Sleep(100 * time.Millisecond)
	if !vu(&suivis, "alpha:arreter") || strings.Contains(strings.Join(suivis, ","), "beta:arreter") {
		t.Fatalf("orders: %v", suivis)
	}
	appelN("POST", `{"action":"ordres","valeur":true}`)

	// the master's address published encrypted; a node that no longer reaches it finds the new one there
	lien := sm.lienMaitre(pm)
	if a, err := dechiffrerDirect(sm.lireDirect(pm).Cle, lien); err != nil || a != adresse || strings.Contains(lien, "127.0.0.1") {
		t.Fatalf("encrypted address: %q %v", a, err)
	}
	if _, err := dechiffrerDirect(base64Cle32(), lien); err == nil {
		t.Fatal("another key read the address")
	}
	sm.arreterMaitre(pm.Nom)
	if err := sm.demarrerMaitre(pm); err != nil {
		t.Fatal(err)
	}
	var nouvelle string
	for i := 0; i < 50 && nouvelle == ""; i++ {
		nouvelle = sm.lienMaitre(pm)
		time.Sleep(20 * time.Millisecond)
	}
	pcsMu.Lock()
	pcsCache[pn.Nom].vue.PCs = []PresencePC{{ID: "id-alpha", Nom: "PC alpha", Lien: nouvelle}}
	pcsMu.Unlock()
	// (in the tests the address is http://127.0.0.1: only a Cloudflare address is taken from GitHub)
	reAncien := reAdresseDirecte
	reAdresseDirecte = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]+$`)
	defer func() { reAdresseDirecte = reAncien }()
	sn.battementNoeud(pn)
	if e := sn.lireDirect(pn); e.Adresse == adresse {
		t.Fatalf("the node kept the old address: %+v", e)
	}
	if _, v := appelN("GET", ""); vue(v)["etat"] != "connecte" {
		t.Fatalf("node after the master moved: %v", v)
	}
	// the master removes the node: told so at its next beat
	if c, _ := appelM("POST", `{"action":"retirer","id":"id-beta"}`); c != 200 {
		t.Fatal("remove")
	}
	sn.battementNoeud(pn)
	if _, v := appelN("GET", ""); vue(v)["etat"] != "injoignable" || !strings.Contains(vue(v)["erreur"].(string), "no longer knows") {
		t.Fatalf("removed node: %v", v)
	}
	// leaving: nothing left
	appelN("POST", `{"action":"quitter"}`)
	if _, err := os.Stat(sn.fichierDirect(pn)); err == nil {
		t.Fatal("the node's file stayed")
	}
	if c, v := appelM("POST", `{"action":"rejoindre","invitation":"`+adresse+`#123456"}`); c != 400 || !strings.Contains(v["erreur"].(string), "master") {
		t.Fatalf("a master joining: %d %v", c, v)
	}
	// refused: the computer is told so, and joins nothing
	_, v = appelM("POST", `{"action":"inviter"}`)
	code, _ = vue(v)["code"].(string)
	adresse, _ = vue(v)["adresse"].(string)
	if c, v := appelN("POST", `{"action":"rejoindre","invitation":"`+adresse+`#`+code+`"}`); c != 200 || vue(v)["etat"] != "attente" {
		t.Fatalf("join again: %d %v", c, v)
	}
	_, v = appelM("GET", "")
	ref, _ = vue(v)["demandes"].([]any)[0].(map[string]any)["ref"].(string)
	if c, _ := appelM("POST", `{"action":"refuser","id":"`+ref+`"}`); c != 200 {
		t.Fatal("refuse")
	}
	sn.attenteAccord(pn)
	if _, v := appelN("GET", ""); vue(v)["etat"] != "fini" || !strings.Contains(vue(v)["erreur"].(string), "refused") {
		t.Fatalf("refused: %v", v)
	}
	if _, err := os.Stat(sn.fichierDirect(pn)); err == nil {
		t.Fatal("a refused computer kept a file")
	}
	if e := sm.lireDirect(pm); len(e.Noeuds) != 0 { // (removed above, refused now)
		t.Fatalf("the refused computer among the master's: %+v", e.Noeuds)
	}
}

func base64Cle32() string { return "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" }

// The encrypted address goes with the presence on GitHub (athanor github pcs --lien).
func TestPresenceLien(t *testing.T) {
	d := t.TempDir()
	if p := presenceLocalePC(t.TempDir(), d); p.Lien != "" {
		t.Fatal("no address yet")
	}
	ecrireEssai(t, filepath.Join(d, "lien-direct"), "QUJDREVGR0hJSktMTU5PUA\n")
	if p := presenceLocalePC(t.TempDir(), d); p.Lien != "QUJDREVGR0hJSktMTU5PUA" {
		t.Fatalf("lien: %q", p.Lien)
	}
	if !reLienDirect.MatchString("QUJDREVGR0hJSktMTU5PUA") || reLienDirect.MatchString("a b") || reLienDirect.MatchString("x;rm") {
		t.Fatal("reLienDirect")
	}
}

func jsonDirect(v any) []byte { b, _ := json.Marshal(v); return b }
