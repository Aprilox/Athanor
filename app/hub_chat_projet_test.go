package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// bancCarteProjet (M295): a home conversation whose assistant proposed `projet` cards, with an existing project
// "site", two servers (vps, base) and two API keys in the vault.
type bancCarteProjet struct {
	s        *Serveur
	site     Projet
	vps, api string
	racine   string
	pilotes  []string
}

func nouveauBancCarteProjet(t *testing.T, cartes ...string) *bancCarteProjet {
	t.Helper()
	s := serveurCoffreTest(t)
	_ = os.Remove(s.acces.chemin) // a file left next to the test binary by an interrupted run
	s.chargerAcces()
	t.Cleanup(func() { _ = os.Remove(s.acces.chemin) })
	for _, f := range []string{dossierModule("servers"), fichierReglagesProjets()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	b := &bancCarteProjet{s: s, racine: t.TempDir(), vps: aleatoire(8)}
	b.site = fauxProjet(t, b.racine, "site", 3, 1)
	s.mu.Lock()
	s.config.Projets = []Projet{b.site}
	err := ecrireConfig(s.config)
	s.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: b.vps, Nom: "vps", Hote: "203.0.113.7", Port: 22, Cree: time.Now()},
			ServeurInventaire{ID: aleatoire(8), Nom: "base", Hote: "203.0.113.8", Port: 22, Cree: time.Now()}), nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, nom := range []string{"Clé Stripe", "Clé Mailjet"} {
		e, err := s.coffre.Ajouter(EntreeCoffre{Nom: nom, Famille: "secret", Type: "autre"}, "valeur-"+nom)
		if err != nil {
			t.Fatal(err)
		}
		if nom == "Clé Stripe" {
			b.api = e.ID
		}
	}
	ancien := demarrerPiloteProjet
	demarrerPiloteProjet = func(_ *Serveur, x Projet) (string, error) {
		b.pilotes = append(b.pilotes, x.Nom)
		return "Pilot started.", nil
	}
	t.Cleanup(func() { demarrerPiloteProjet = ancien })
	k := s.kitCourant()
	h := projetAccueil(k)
	_ = os.MkdirAll(h.Dossier, 0o700)
	conv := conversationChat{Messages: []MessageChat{{Role: "vous", Texte: "Un site sur @vps"}}}
	for _, j := range cartes {
		conv.Messages = append(conv.Messages, MessageChat{Role: "claude", Texte: "Voici la carte.\n<action>" + j + "</action>"})
	}
	ecrireConversation(k, h, conv)
	return b
}

// carte: the card of message i, as the page receives it now.
func (b *bancCarteProjet) carte(i int) CarteActionChat {
	k := b.s.kitCourant()
	conv := lireConversation(k, projetAccueil(k))
	a, _ := lireActionChat(conv.Messages[i].Texte)
	return b.s.carteActionChat(a)
}

// lancer: Launch on the card of message i, as it is shown (its fingerprint) unless plus says otherwise.
func (b *bancCarteProjet) lancer(t *testing.T, i int, appareil string, plus map[string]any) (int, string) {
	t.Helper()
	c := b.carte(i)
	corps := map[string]any{"index": i, "decision": "lancer", "action": c.ActionChat, "empreinte": c.Empreinte}
	for k, v := range plus {
		corps[k] = v
	}
	j, _ := json.Marshal(corps)
	r := httptest.NewRequest("POST", "/api/hub/chat/action", strings.NewReader(string(j)))
	if appareil != "" {
		r = r.WithContext(context.WithValue(r.Context(), cleContexte(nomCleAppareil), appareil))
	}
	w := httptest.NewRecorder()
	b.s.apiHubChatAction(w, r)
	return w.Code, w.Body.String()
}

func (b *bancCarteProjet) dossier(nom string) string {
	return filepath.ToSlash(filepath.Join(b.racine, nom))
}

// TestHubChatActionsProjet (M295): the `projet` card of the home, on this PC only, with its deployment, or for an
// existing project (its deployment only); Ignore permissions as everywhere; an unknown mention launches nothing.
func TestHubChatActionsProjet(t *testing.T) {
	b := nouveauBancCarteProjet(t)
	b = nouveauBancCarteProjet(t,
		`{"type":"projet","projet":"studio","texte":"Un jeu.","dossier":"`+b.dossier("studio")+`"}`,
		`{"type":"projet","projet":"boutique","texte":"Une boutique.","dossier":"`+b.dossier("boutique")+`","deploiement":{"serveurs":["@VPS"],"apis":["clé stripe"],"url":"https://boutique.test"},"permission":"auto"}`,
		`{"type":"projet","projet":"SITE","texte":"à ne pas écrire","deploiement":{"serveurs":["vps"],"url":"https://site.test","moment":"jalon"},"permission":"auto"}`,
		`{"type":"projet","projet":"risque","texte":"Un essai.","dossier":"`+b.dossier("risque")+`","permission":"ignorer"}`,
		`{"type":"projet","projet":"perdu","texte":"x","dossier":"`+b.dossier("perdu")+`","deploiement":{"serveurs":["fantome"]}}`,
		`{"type":"projet","projet":"perdu2","texte":"x","dossier":"`+b.dossier("perdu2")+`","deploiement":{"serveurs":["vps"],"apis":["nulle-part"]}}`,
		`{"type":"demande","projet":"site","texte":"x","deploiement":{"serveurs":["vps"]},"permission":"ignorer"}`)
	s := b.s
	env := func(projet string) (Environnement, int) {
		envs := s.acces.Environnements(projet)
		return envs[s.deploiementProjet(projet)], len(envs)
	}

	// on this PC only: the card shows nothing of a deployment, and Launch writes none
	if c := b.carte(1); c.Deploiement != nil || c.Noms != nil || len(c.Acces) != 2 || c.Erreur != "" || c.Empreinte == "" {
		t.Fatalf("Studio only: %+v", c)
	}
	if code, txt := b.lancer(t, 1, "", nil); code != 200 {
		t.Fatalf("Studio only: %d %s", code, txt)
	}
	if _, n := env("studio"); n != 0 || !slices.Equal(b.pilotes, []string{"studio"}) || len(lireReglagesProjets()["studio"]) != 0 {
		t.Fatalf("Studio only wrote a deployment or a setting: %d envs, pilots %v", n, b.pilotes)
	}

	// with its deployment: the names the owner mentioned become ids, the card names them
	c := b.carte(2)
	if d := c.Deploiement; c.Erreur != "" || d == nil || !slices.Equal(d.Serveurs, []string{b.vps}) || !slices.Equal(d.APIs, []string{b.api}) ||
		d.Moment != "livraison" || c.Noms[b.vps] != "vps" || c.Noms[b.api] != "Clé Stripe" || len(c.Noms) != 2 {
		t.Fatalf("card with a deployment: %+v %+v", c, c.Deploiement)
	}
	if code, txt := b.lancer(t, 2, "tel", nil); code != 200 { // a mode that still asks: from a device too
		t.Fatalf("with a deployment: %d %s", code, txt)
	}
	if e, n := env("boutique"); n != 1 || !slices.Equal(e.Serveurs, []string{b.vps}) || !slices.Equal(e.APIs, []string{b.api}) || e.URL != "https://boutique.test" ||
		lireReglagesProjets()["boutique"]["permission"] != "auto" || !slices.Equal(b.pilotes, []string{"studio", "boutique"}) {
		t.Fatalf("deployment of the new project: %+v (%d), settings %v, pilots %v", e, n, lireReglagesProjets(), b.pilotes)
	}

	// an existing project: its deployment only (no project, no request, no setting, no pilot)
	c = b.carte(3)
	if c.Quoi != "Set the project's deployment" || c.Projet != "site" || c.Texte != "" || c.Dossier != "" || c.Permission != "" || c.Erreur != "" {
		t.Fatalf("card of an existing project: %+v", c)
	}
	avant, _ := os.ReadFile(b.site.Demande())
	file := len(b.site.LireFileAttente())
	if code, txt := b.lancer(t, 3, "", nil); code != 200 {
		t.Fatalf("existing project: %d %s", code, txt)
	}
	apres, _ := os.ReadFile(b.site.Demande())
	s.mu.Lock()
	nb := len(s.config.Projets)
	s.mu.Unlock()
	if e, n := env("site"); n != 1 || !slices.Equal(e.Serveurs, []string{b.vps}) || e.Moment != "jalon" || nb != 3 || len(b.pilotes) != 2 ||
		string(avant) != string(apres) || len(b.site.LireFileAttente()) != file || len(lireReglagesProjets()["site"]) != 0 {
		t.Fatalf("existing project: env %+v (%d), %d projects, pilots %v, request changed %v, queue %d → %d, settings %v", e, n, nb, b.pilotes, string(avant) != string(apres), file, len(b.site.LireFileAttente()), lireReglagesProjets())
	}

	// Ignore permissions: no password for Athanor, not confirmed, wrong password, from a device: nothing created
	confirme := map[string]any{"confirmer": true, "mot_de_passe": mdpPreuveTest}
	if code, _ := b.lancer(t, 4, "", confirme); code != http.StatusForbidden {
		t.Errorf("ignorer without a password for Athanor: %d, want 403", code)
	}
	avecMdpPreuve(s)
	for _, x := range []struct {
		quoi, appareil string
		plus           map[string]any
		code           int
	}{
		{"not confirmed", "", map[string]any{"mot_de_passe": mdpPreuveTest}, http.StatusConflict},
		{"without the proof", "", map[string]any{"confirmer": true}, http.StatusUnauthorized},
		{"wrong password", "", map[string]any{"confirmer": true, "mot_de_passe": "faux"}, http.StatusUnauthorized},
		{"from a device", "tel", confirme, http.StatusForbidden},
	} {
		if code, _ := b.lancer(t, 4, x.appareil, x.plus); code != x.code {
			t.Errorf("ignorer %s: %d, want %d", x.quoi, code, x.code)
		}
	}
	if _, ok := s.projetLocalChat("risque"); ok || len(b.pilotes) != 2 {
		t.Fatalf("a refused Ignore permissions created the project or started its pilot: %v", b.pilotes)
	}
	if code, txt := b.lancer(t, 4, "", confirme); code != 200 || lireReglagesProjets()["risque"]["permission"] != "ignorer" || len(b.pilotes) != 3 {
		t.Fatalf("ignorer with the proof: %d %s, settings %v", code, txt, lireReglagesProjets())
	}
	if j := journalAuditTexte(t); !strings.Contains(j, "defaut → ignorer") || !strings.Contains(j, "déploiement modifié") || strings.Contains(j, mdpPreuveTest) {
		t.Error("audit: the permission or the deployment is missing, or the password is in the log")
	}

	// an unknown server or API: the card says so and launches nothing
	if c := b.carte(5); c.Erreur == "" {
		t.Error("an unknown server gives a card that can be launched")
	}
	if c := b.carte(6); c.Erreur != errEnvAPI.Error() {
		t.Errorf("unknown API: %q", c.Erreur)
	}
	for _, i := range []int{5, 6} {
		if code, _ := b.lancer(t, i, "", nil); code != http.StatusBadRequest {
			t.Errorf("unknown mention launched: %d", code)
		}
	}
	if _, ok := s.projetLocalChat("perdu"); ok {
		t.Error("the project of an unknown mention was created")
	}
	// another card never carries a deployment nor a permission
	if c := b.carte(7); c.Deploiement != nil || c.Permission != "" || c.Empreinte != "" {
		t.Errorf("a request card with a deployment: %+v", c)
	}
	// every word of these cards is translated
	cles := []string{"Set the project's deployment", "Deployment of “{0}” saved.", "This card changed since it was shown: read it again, then launch it.",
		"Project “{0}” added, but its settings were not all saved and its pilot was not started: {1}",
		"Unknown permission mode (manuel, auto, modifications or ignorer)", errEnvAPI.Error()}
	for _, i := range []int{2, 3, 4} {
		cles = append(cles, b.carte(i).Acces...)
	}
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range cles {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}

// TestCarteProjetMentions (M295, in place of R3 and R4 of the missions): Launch writes the servers and the APIs
// the card shows and nothing else, and a card that is no longer the one the owner read launches nothing.
func TestCarteProjetMentions(t *testing.T) {
	b := nouveauBancCarteProjet(t)
	b = nouveauBancCarteProjet(t,
		`{"type":"projet","projet":"vitrine","texte":"Une vitrine.","dossier":"`+b.dossier("vitrine")+`","deploiement":{"serveurs":["vps","base"],"apis":["Clé Stripe","Clé Mailjet"],"url":"https://vitrine.test"}}`,
		`{"type":"projet","projet":"blog","texte":"Un blog.","dossier":"`+b.dossier("blog")+`","deploiement":{"serveurs":["vps"]}}`)
	s := b.s
	cree := func(nom string) bool {
		_, ok := s.projetLocalChat(nom)
		return ok
	}
	// the owner removed a server and an API on the card: only what it still shows is written
	vue := b.carte(1)
	a := vue.ActionChat
	a.Deploiement = &DeploiementDemande{Serveurs: []string{b.vps}, APIs: []string{b.api}, URL: "https://vitrine.test", Moment: "livraison"}
	if code, txt := b.lancer(t, 1, "", map[string]any{"action": a}); code != 200 {
		t.Fatalf("launch as edited: %d %s", code, txt)
	}
	envs := s.acces.Environnements("vitrine")
	if e := envs[s.deploiementProjet("vitrine")]; len(envs) != 1 || !slices.Equal(e.Serveurs, []string{b.vps}) || !slices.Equal(e.APIs, []string{b.api}) {
		t.Fatalf("written beyond the card: %+v", envs)
	}
	if len(s.acces.Environnements("site")) != 0 {
		t.Fatal("the card of a project wrote the deployment of another")
	}

	// R3: without the card that was read, or with the one of an older view, nothing is launched
	vue = b.carte(2)
	if code, _ := b.lancer(t, 2, "", map[string]any{"empreinte": ""}); code != http.StatusConflict || cree("blog") {
		t.Fatalf("launched without the card read: %d", code)
	}
	a = vue.ActionChat
	a.Projet, a.Dossier, a.Texte = "site", "", "" // a new project turned into the deployment of an existing one
	if code, _ := b.lancer(t, 2, "", map[string]any{"action": a}); code != http.StatusConflict || len(s.acces.Environnements("site")) != 0 {
		t.Fatalf("a card turned into another action was launched: %d", code)
	}
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { // the server it named is gone since
		return slices.DeleteFunc(l, func(x ServeurInventaire) bool { return x.ID == b.vps }), nil
	}); err != nil {
		t.Fatal(err)
	}
	if code, _ := b.lancer(t, 2, "", map[string]any{"action": vue.ActionChat, "empreinte": vue.Empreinte}); code != http.StatusConflict || cree("blog") || len(b.pilotes) != 1 {
		t.Fatalf("the card of an older view was launched: %d, pilots %v", code, b.pilotes)
	}
}

// TestCarteProjetReglages (M295b): the settings chosen under Edit (working modes, GitHub, the project's own
// settings) are applied by Launch before the pilot starts; the assistant cannot set them, a setting cannot carry
// Ignore permissions, and a device does not choose the folder.
func TestCarteProjetReglages(t *testing.T) {
	b := nouveauBancCarteProjet(t)
	b = nouveauBancCarteProjet(t,
		`{"type":"projet","projet":"atelier","texte":"Un site.","dossier":"`+b.dossier("atelier")+`","modes":{"eco":true},"github":"creer","depot":"x","reglages":{"quota_five_hour":"80:pause"}}`,
		`{"type":"projet","projet":"panne","texte":"Un site.","dossier":"`+b.dossier("panne")+`"}`,
		`{"type":"projet","projet":"loin","texte":"Un site.","dossier":"`+b.dossier("loin")+`"}`)
	s := b.s
	var lies []ProjetCarte
	ancien := githubProjet
	githubProjet = func(_ *Serveur, p Projet, pc ProjetCarte) error {
		if p.Nom == "panne" {
			return errors.New("témoin-github-en-panne")
		}
		lies = append(lies, pc)
		return nil
	}
	t.Cleanup(func() { githubProjet = ancien })

	vue := b.carte(1)
	if vue.Modes != nil || vue.GitHub != "" || vue.Depot != "" || vue.Reglages != nil || vue.Erreur != "" {
		t.Fatalf("settings written by the assistant are on the card: %+v", vue)
	}
	a := vue.ActionChat
	// refused as their own screens refuse them, and nothing is created
	for nom, change := range map[string]func(*ActionChat){
		"repository to type": func(x *ActionChat) { x.GitHub = "lier" },
		"unknown setting":    func(x *ActionChat) { x.Reglages = map[string]string{"inconnu": "1"} },
		"invalid value":      func(x *ActionChat) { x.Reglages = map[string]string{"quota_five_hour": "80:exploser"} },
		"too many workers":   func(x *ActionChat) { x.Modes = &ModesCarte{Parallele: true, ParalleleMax: 50} },
	} {
		x := a
		change(&x)
		if code, _ := b.lancer(t, 1, "", map[string]any{"action": x}); code != http.StatusBadRequest {
			t.Errorf("%s: %d", nom, code)
		}
	}
	if _, ok := s.projetLocalChat("atelier"); ok || len(b.pilotes) != 0 {
		t.Fatal("a refused card created its project")
	}
	a.Modes = &ModesCarte{Parallele: true, ParalleleMax: 4, Eco: true}
	a.GitHub, a.Depot, a.SansPousse = "lier", "https://github.com/octo/atelier.git", true
	a.Reglages = map[string]string{"quota_five_hour": "80:pause", "modele": "", "permission": "ignorer"}
	if code, txt := b.lancer(t, 1, "", map[string]any{"action": a}); code != 200 {
		t.Fatalf("launch with settings: %d %s", code, txt)
	}
	p, ok := s.projetLocalChat("atelier")
	if !ok || !slices.Equal(b.pilotes, []string{"atelier"}) {
		t.Fatalf("project or pilot missing: %v", b.pilotes)
	}
	if m := p.LireMode(); !m.Parallele || m.ParalleleMax != 4 || !m.Eco || m.Rapide {
		t.Errorf("working modes: %+v", m)
	}
	if r := lireReglagesProjets()["atelier"]; len(r) != 1 || r["quota_five_hour"] != "80:pause" {
		t.Errorf("own settings (never Ignore permissions through them): %v", r)
	}
	if len(lies) != 1 || lies[0].GitHub != "lier" || lies[0].Depot != "octo/atelier" || !lies[0].SansPousse {
		t.Errorf("GitHub: %+v", lies)
	}

	// a setting that fails: the project stays, its pilot is not started, the card says why
	a = b.carte(2).ActionChat
	a.GitHub = "creer"
	if code, txt := b.lancer(t, 2, "", map[string]any{"action": a}); code != 200 || !strings.Contains(txt, "its settings were not all saved") || !strings.Contains(txt, "témoin-github-en-panne") {
		t.Fatalf("failed setting: %d %s", code, txt)
	}
	if _, ok := s.projetLocalChat("panne"); !ok || len(b.pilotes) != 1 {
		t.Fatalf("after a failed setting: pilots %v", b.pilotes)
	}

	// from a device: the folder proposed, never another one
	a = b.carte(3).ActionChat
	a.Dossier = b.dossier("ailleurs")
	if code, _ := b.lancer(t, 3, "telephone", map[string]any{"action": a}); code != http.StatusForbidden {
		t.Errorf("folder changed from a device: %d", code)
	}
	if code, txt := b.lancer(t, 3, "telephone", nil); code != 200 {
		t.Errorf("card launched from a device as proposed: %d %s", code, txt)
	}
	for _, l := range []string{"fr", "es", "de", "it", "pt"} {
		dico := dicoTUI(l)
		for _, c := range []string{"Creates or links the GitHub repository chosen on this card, with this computer's GitHub connection.",
			"Project “{0}” added, but its settings were not all saved and its pilot was not started: {1}", "The folder is chosen on the PC.",
			"Write the repository as OWNER/NAME (e.g. octocat/my-site).", "Unknown project setting", "This value is not allowed for this project setting"} {
			if _, ok := dico[c]; !ok {
				t.Errorf("%s: no translation of %q", l, c)
			}
		}
	}
}
