package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The master's role handed over: a live node gets the team, copies its workshop, opens its own listener and
// tells the old master, which becomes its node; the other node is sent to the new master at its next beat;
// the old store is frozen meanwhile; a handover nobody finishes is cancelled, the store open again.
func TestPassationDirecte(t *testing.T) {
	sansTunnelDirect = true
	ancienRe, ancienH, ancienA, ancienD := reAdresseDirecte, atelierHorsGitFn, attenteRelais, dureePassation
	reAdresseDirecte = regexp.MustCompile(`^http://127\.0\.0\.1:[0-9]+$`)
	atelierHorsGitFn = func(*Serveur, Projet, bool) error { return nil }
	attenteRelais = 100 * time.Millisecond
	defer func() {
		sansTunnelDirect = false
		reAdresseDirecte, atelierHorsGitFn, attenteRelais, dureePassation = ancienRe, ancienH, ancienA, ancienD
	}()
	type pc struct {
		s     *Serveur
		p     Projet
		appel func(methode, corps string) (int, map[string]any)
	}
	nouveau := func(nom string) pc {
		d := t.TempDir()
		p := Projet{Nom: nom, Dossier: filepath.Join(d, nom)}
		ecrire(t, p.Plan(), planTest)
		s := nouveauServeur(Config{Kit: filepath.Join(d, "kit"), Projets: []Projet{p}, ProjetActif: nom})
		s.csrf = "jeton"
		mux := http.NewServeMux()
		s.routes(mux)
		pcsMu.Lock()
		pcsCache[nom] = &cachePCs{vue: VuePCs{Actif: true, Moi: &PresencePC{ID: "id" + nom, Nom: "PC " + nom}}, quand: time.Now()}
		pcsMu.Unlock()
		return pc{s, p, func(methode, corps string) (int, map[string]any) {
			r := httptest.NewRequest(methode, "/api/pcs-direct?p="+nom, strings.NewReader(corps))
			r.Header.Set("X-Athanor", "jeton")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			var v map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &v)
			return w.Code, v
		}}
	}
	vue := func(v map[string]any) map[string]any { d, _ := v["direct"].(map[string]any); return d }
	a, b, c := nouveau("passa"), nouveau("passb"), nouveau("passc")
	defer a.s.arreterMaitre(a.p.Nom)
	defer b.s.arreterMaitre(b.p.Nom)
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	cle := base64.StdEncoding.EncodeToString(k)
	jb, jc := aleatoire(32), aleatoire(32)
	if err := a.s.ecrireDirect(a.p, equipeDirecte{Role: "maitre", Equipe: "eq", Cle: cle, AtelierLien: true, Noeuds: map[string]noeudConnu{
		"idpassb": {Nom: "PC passb", Jeton: hacherJeton(jb)}, "idpassc": {Nom: "PC passc", Jeton: hacherJeton(jc)}}}); err != nil {
		t.Fatal(err)
	}
	ecrire(t, filepath.Join(a.p.Atelier(), "REQUEST.md"), "# Request\n")
	a.s.semerAtelier(a.p)
	if err := a.s.demarrerMaitre(a.p); err != nil {
		t.Fatal(err)
	}
	adresse := func(x pc) string {
		for i := 0; i < 100; i++ {
			if v := x.s.vueDirecte(x.p); v != nil && v.Adresse != "" && v.Role == "maitre" {
				return v.Adresse
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("no address")
		return ""
	}
	adrA := adresse(a)
	for _, x := range []struct {
		pc
		j string
	}{{b, jb}, {c, jc}} {
		if err := x.s.ecrireDirect(x.p, equipeDirecte{Role: "noeud", Equipe: "eq", Cle: cle, Adresse: adrA, Jeton: x.j, Maitre: "PC passa", AtelierLien: true, AccepteOrdres: true, Depuis: time.Now().Unix()}); err != nil {
			t.Fatal(err)
		}
	}
	// only a live node takes the team over
	if code, v := a.appel("POST", `{"action":"transmettre","id":"idpassb"}`); code != 400 || !strings.Contains(v["erreur"].(string), "not live") {
		t.Fatalf("a node not seen yet: %d %v", code, v)
	}
	if code, _ := a.appel("POST", `{"action":"transmettre","id":"idinconnu"}`); code != 400 {
		t.Fatal("an unknown computer")
	}
	if code, _ := b.appel("POST", `{"action":"transmettre","id":"idpassc"}`); code != 400 {
		t.Fatal("a node handing the team over")
	}
	b.s.battementNoeud(b.p)
	c.s.battementNoeud(c.p)
	if code, v := a.appel("POST", `{"action":"transmettre","id":"idpassb"}`); code != 200 || vue(v)["passation"].(map[string]any)["etat"] != "en_cours" {
		t.Fatalf("hand over: %d %v", code, v)
	}
	// the old store is frozen: a node's write is refused (it keeps its change)
	mC := magasinPilote(c.s.priveProjet(c.p), c.s.lireDirect(c.p))
	ecrire(t, filepath.Join(c.p.Atelier(), "LOG.md"), "- by C, during the handover\n")
	if _, err := synchroniserAtelier(c.p.Atelier(), filepath.Join(c.s.priveProjet(c.p), "atelier-base", "equipe"), mC, fusionAtelierGit, "PC passc", ""); !errors.Is(err, errGeleAtelier) {
		t.Fatalf("a write to the frozen store: %v", err)
	}
	// B takes over at its next beat
	b.s.battementNoeud(b.p)
	for i := 0; i < 200 && (b.s.lireDirect(b.p).Role != "maitre" || a.s.lireDirect(a.p).Role != "noeud"); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	eA, eB := a.s.lireDirect(a.p), b.s.lireDirect(b.p)
	adrB := adresse(b)
	if eB.Role != "maitre" || eB.Cle != cle || eB.Equipe != "eq" || eA.Role != "noeud" || eA.Adresse != adrB || eA.Maitre != "PC passb" {
		t.Fatalf("after the handover: A %+v, B %+v", eA, eB)
	}
	if _, ok := eB.Noeuds["idpassc"]; !ok || len(eB.Noeuds) != 2 || eB.Noeuds["idpassa"].Jeton != hacherJeton(eA.Jeton) {
		t.Fatalf("the new master's nodes: %+v", eB.Noeuds)
	}
	vA, _ := a.s.magasinProjet(a.p).dernier()
	vB, _ := b.s.magasinProjet(b.p).dernier()
	if vB.Version != vA.Version || vB.Fichiers["REQUEST.md"] == "" || vB.Fichiers["REQUEST.md"] != vA.Fichiers["REQUEST.md"] {
		t.Fatalf("the workshop did not move: %+v %+v", vA, vB)
	}
	if a.s.lienMaitre(a.p) != "" || b.s.lienMaitre(b.p) == "" {
		t.Fatal("the address on GitHub: only the new master's")
	}
	// C, at its next beat, is sent to B; A beats B as its node; B sees both
	c.s.battementNoeud(c.p)
	if e := c.s.lireDirect(c.p); e.Adresse != adrB || e.Maitre != "PC passb" {
		t.Fatalf("C not moved to the new master: %+v", e)
	}
	a.s.battementNoeud(a.p)
	if _, v := a.appel("GET", ""); vue(v)["etat"] != "connecte" || vue(v)["maitre"] != "PC passb" {
		t.Fatalf("A as a node: %v", v)
	}
	if _, v := b.appel("GET", ""); len(vue(v)["noeuds"].([]any)) != 3 {
		t.Fatalf("B's table: %v", v)
	}
	// C's change made during the handover reaches the new master
	mC = magasinPilote(c.s.priveProjet(c.p), c.s.lireDirect(c.p))
	if r, err := synchroniserAtelier(c.p.Atelier(), filepath.Join(c.s.priveProjet(c.p), "atelier-base", "equipe"), mC, fusionAtelierGit, "PC passc", ""); err != nil || r.Version != vB.Version+1 {
		t.Fatalf("C's change to the new master: %+v %v", r, err)
	}
	// a handover nobody finishes: cancelled, the store open again
	b.s.battementNoeud(b.p) // (a master: nothing)
	if code, v := b.appel("POST", `{"action":"transmettre","id":"idpassc"}`); code != 200 {
		t.Fatalf("second handover: %d %v", code, v)
	}
	dureePassation = 0
	b.s.verifierPassation(b.p)
	if _, v := b.appel("GET", ""); vue(v)["passation"].(map[string]any)["etat"] != "erreur" || vue(v)["role"] != "maitre" {
		t.Fatalf("cancelled: %v", v)
	}
	if b.s.magasinProjet(b.p).gele() {
		t.Fatal("the store stayed frozen")
	}
}
