package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// tunnelHTTPS: a local address carried by Athanor's SSH connection to the server's Caddy on 443 (the test
// container publishes 22 and 80 only); the verifier reaches it through resolutionsTest.
func tunnelHTTPS(t *testing.T, s *Serveur, id string) string {
	t.Helper()
	l, _ := lireServeurs()
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		t.Fatal("server not in the inventory")
	}
	c, err := s.connecterServeur(context.Background(), l[i])
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close(); _ = c.Close() })
	go func() {
		for {
			a, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer a.Close()
				b, err := c.Dial("tcp", "127.0.0.1:443")
				if err != nil {
					return
				}
				defer b.Close()
				go func() { _, _ = io.Copy(b, a) }()
				_, _ = io.Copy(a, b)
			}()
		}
	}()
	return ln.Addr().String()
}

// TestMissionP5 (M176, P5): the whole mission of VISION §1 on the test SSH container. The sentence → the card by a
// fake orchestrator (the real /api/mcp route) → Lancer → a fake Dev pilot (played here) delivers M1 (site v1) then
// M2 (site v2) with commits → a fake chef ops: the dev task's plan (DNS on a fake Cloudflare, origin certificate,
// the static deployment that writes Caddy's site with auth_proxy), the prod task's plan (DNS, certificate) accepted by
// the owner → M2 relayed to dev, checked (401 without the password, 200 with it) → Athanor restarted in the middle
// (a new Serveur on the same config and files, the vault locked) → the prod accord given by a paired phone, unlocked
// → prod deployed by the mission's relay and checked → the mission ends → Revenir on dev serves v1 again. The journal
// reads from end to end (every key translated, no event twice), nothing launched twice, no vault value anywhere.
func TestMissionP5(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if os.Getenv("ATHANOR_SSH_TEST") == "" || web == "" {
		t.Skip("ATHANOR_SSH_TEST/ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	debut := time.Now()
	// dossierAthanor() fixed before the inventory is written: pairing and the project's creation write the config
	cfg := cheminConfig()
	t.Cleanup(func() { _ = os.Remove(cfg) })
	if err := ecrireConfig(Config{Langue: "fr"}); err != nil {
		t.Fatal(err)
	}
	s, _, id, _, _ := serveurConteneurTest(t)
	cur := s                                                                              // the Athanor running now: a new one after the restart
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) { // the owner's one box: dev and prod
		for i := range l {
			if l[i].ID == id {
				l[i].Roles = []string{"dev", "prod"}
			}
		}
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}
	const projet, hoteDev, hoteProd, mdp = "boulangerie-martin", "dev.martin.fr", "martin.fr", "mdp-test"
	ancienPas := pasSuiviMission
	pasSuiviMission = time.Hour // one reading at each start; the test reads the sources itself (observer)
	t.Cleanup(func() { pasSuiviMission = ancienPas })
	for _, f := range []string{dossierMissions(), fichierJalonsFaits()} {
		_ = os.RemoveAll(f)
		t.Cleanup(func() { _ = os.RemoveAll(f) })
	}
	oublierRelais(projet)
	t.Cleanup(func() { oublierRelais(projet) })
	ssh := func(cmd string) string { // never fatal: also run by the cleanup, on the Athanor running then
		b, _ := json.Marshal(map[string]any{"commande": cmd})
		r, _ := cur.appelerOutilServeur(context.Background(), "test", id, "ssh_commande", b, Sauvegarde{})
		return r.Sortie
	}
	nettoyer := "sudo -n rm -rf /srv/athanor/" + projet + "-dev /srv/athanor/" + projet + "-prod /etc/caddy/sites/" + hoteDev + ".caddy /etc/caddy/sites/" + hoteProd + ".caddy " +
		dossierTLS + "/" + hoteDev + ".* " + dossierTLS + "/" + hoteProd + ".*; sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null 2>&1; true"
	ssh(nettoyer)
	t.Cleanup(func() { ssh(nettoyer) })

	// the window and the tunnel; the phone paired first (pairing rewrites the config), as in TestMissionP7
	var mux *http.ServeMux
	var h http.Handler
	interfaceDe := func(x *Serveur) {
		x.csrf, x.cleAcces, x.session = "jeton", "cle", "session"
		x.config.Langue = "fr"
		mux = http.NewServeMux()
		x.routes(mux)
		x.mux = mux
		x.config.Distance.Actif = true
		x.dist().tunnel = etatTunnel{etat: "pret", adresse: "https://abc-def.trycloudflare.com"}
		h = x.gestionDistance(mux)
	}
	interfaceDe(s)
	tel := appairerTest(t, s, h)
	telAPI := []string{"X-Athanor", "jeton"}

	// the vault's Cloudflare token and the provider holding the zone martin.fr (one fake for DNS, SSL and the origin CA)
	jetonCF := "cf-jeton-TEMOIN-p5-" + aleatoire(8)
	cf, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare", Portee: "infra"}, jetonCF)
	if err != nil {
		t.Fatal(err)
	}
	droits := []string{"dns:lire", "dns:écrire", "ssl", "certificats"}
	if err := modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		return append(l, Fournisseur{ID: "f1", Type: "cloudflare", Nom: "CF", Cle: cf.ID, Etat: "ok", Compte: "acc1", Droits: droits,
			Zones: []ZoneFournisseur{{ID: "z1", Nom: "martin.fr", Droits: droits}}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	_, relever := fauxCloudflareComplet(t, jetonCF)
	// a project beside which the new one is made (never in the user's home)
	base := t.TempDir()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "ancien-site", Dossier: filepath.Join(base, "ancien-site")})

	// HTTPS from this PC: the tunnel to the container's 443, the origin certificates trusted once issued
	tunnel := tunnelHTTPS(t, s, id)
	racines := x509.NewCertPool()
	ancienneRacine := racinesVerif
	racinesVerif = racines
	t.Cleanup(func() { racinesVerif = ancienneRacine })
	resoudreTest(t, hoteDev+":443", tunnel)
	resoudreTest(t, hoteProd+":443", tunnel)
	var muRacines sync.Mutex
	faireConfiance := func(env string) { // the certificate certificat_origine kept in the vault (its key never read here)
		e := cur.acces.Environnements(projet)[env]
		v, err := cur.coffre.Valeur(e.Certificat)
		if err != nil {
			t.Errorf("the %s certificate: %v", env, err)
			return
		}
		muRacines.Lock()
		defer muRacines.Unlock()
		for reste := []byte(v); ; {
			b, r := pem.Decode(reste)
			if b == nil {
				break
			}
			reste = r
			if b.Type == "CERTIFICATE" {
				if c, err := x509.ParseCertificate(b.Bytes); err == nil {
					racines.AddCert(c)
				}
			}
		}
	}
	page := func(hote, motDePasse string) (int, string) { // what a visitor gets, by the tunnel, the certificate checked
		muRacines.Lock()
		tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: racines.Clone(), ServerName: hote},
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", tunnel)
			}}
		muRacines.Unlock()
		defer tr.CloseIdleConnections()
		req, _ := http.NewRequest("GET", "https://"+hote+"/", nil)
		if motDePasse != "" {
			req.SetBasicAuth(utilisateurProxy, motDePasse)
		}
		rep, err := (&http.Client{Transport: tr, Timeout: 20 * time.Second}).Do(req)
		if err != nil {
			return 0, err.Error()
		}
		defer rep.Body.Close()
		b, _ := io.ReadAll(rep.Body)
		return rep.StatusCode, string(b)
	}
	motDePasseProxy := func() string { // read from the vault, never printed
		for _, e := range cur.coffre.Liste() {
			if e.Source == "proxy:"+projet+"/dev" {
				v, _ := cur.coffre.Valeur(e.ID)
				return v
			}
		}
		return ""
	}
	devSert := func(quoi, version string) {
		t.Helper()
		if code, _ := page(hoteDev, ""); code != http.StatusUnauthorized {
			t.Errorf("%s: dev without the password: %d", quoi, code)
		}
		mp := motDePasseProxy()
		if code, corps := page(hoteDev, mp); mp == "" || code != http.StatusOK || !strings.Contains(corps, "site-exemple "+version) {
			t.Errorf("%s: dev with the password: %d %.120q", quoi, code, corps)
		}
	}

	var mu sync.Mutex
	var reponses, vu strings.Builder // every API answer; every MCP answer an agent received
	var consignes []string
	appel := func(methode, chemin, corps string, code int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		r.AddCookie(&http.Cookie{Name: "athanor_session", Value: cur.session})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		mu.Lock()
		reponses.WriteString(w.Body.String())
		mu.Unlock()
		if w.Code != code {
			t.Fatalf("%s %s: %d %s, expected %d", methode, chemin, w.Code, w.Body.String(), code)
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}
	outil := func(jeton, nom, args string) string { // an agent's MCP call, through the real route
		rep := appelMCP(t, mux, "Bearer "+jeton, appelOutil(nom, args))
		texte := ""
		if rep.Error != nil {
			texte = rep.Error.Message
		} else if len(rep.Result.Content) > 0 {
			texte = rep.Result.Content[0].Text
		}
		mu.Lock()
		vu.WriteString(texte)
		mu.Unlock()
		if rep.Error != nil || rep.Result.IsError {
			t.Errorf("%s %s: %s", nom, args, texte)
		}
		return texte
	}
	session := func(dossier, sid, resultat string) error {
		j, _ := json.Marshal(map[string]any{"type": "result", "is_error": false, "result": resultat})
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(`{"type":"system","subtype":"init","session_id":"`+sid+`"}`+"\n"+string(j)+"\n"), 0o600)
	}
	lireDossier := func(dossier, f string) string {
		b, _ := os.ReadFile(filepath.Join(dossier, f))
		mu.Lock()
		if f == "consigne.txt" {
			consignes = append(consignes, string(b))
		}
		mu.Unlock()
		return strings.TrimSpace(string(b))
	}
	git := func(dossier string, args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dossier, "-c", "user.name=Pilote", "-c", "user.email=pilote@exemple.test", "-c", "core.autocrlf=false"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}

	// the fakes: the orchestrator reads the hub's state then proposes Martin's card; the pilot's start is counted
	ancienOrch, ancienPilote, ancienOps := sessionOrchestrateur, demarrerPiloteProjet, sessionTacheServeurs
	t.Cleanup(func() {
		sessionOrchestrateur, demarrerPiloteProjet, sessionTacheServeurs = ancienOrch, ancienPilote, ancienOps
	})
	sessionOrchestrateur = func(_ *Serveur, _ context.Context, dossier string) error {
		lireDossier(dossier, "consigne.txt")
		jeton := lireDossier(dossier, "hub-jeton")
		if etat := outil(jeton, "modules_etat", "{}"); !strings.Contains(etat, `"Conteneur"`) || !strings.Contains(etat, `"Jeton Cloudflare"`) {
			t.Errorf("modules_etat: %.300s", etat)
		}
		outil(jeton, "proposer_mission", `{"carte":{"titre":"Site vitrine de la boulangerie Martin","projet":{"nom":"`+projet+`","nouveau":true},`+
			`"dev":{"brief":"Un site vitrine : accueil, produits, horaires, plan d'accès, contact"},`+
			`"envs":[{"nom":"dev","url":"https://`+hoteDev+`","protection":"auth_proxy","serveurs":["`+id+`"]},`+
			`{"nom":"prod","url":"https://`+hoteProd+`","protection":"aucune","serveurs":["`+id+`"]}],`+
			`"serveurs":{"brief":"DNS chez Cloudflare, certificats d'origine, Caddy, mot de passe devant la dev"},`+
			`"acces":[{"entree":"Jeton Cloudflare","pour":"`+id+`"}],"jalons_estimes":2,"approbations":[]}}`)
		return session(dossier, "sess-orch", "ok")
	}
	var pilotes []string
	demarrerPiloteProjet = func(_ *Serveur, p Projet) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		pilotes = append(pilotes, p.Nom)
		return "Pilot started", nil
	}

	// the fake chef ops, routed by its task's environment (said by its first consigne only, then by its folder)
	envTache, sessions := map[string]string{}, map[string]int{}
	etapesTache := map[string][][2]string{}
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		consigne, jeton := lireDossier(dossier, "consigne.txt"), lireDossier(dossier, "hub-jeton")
		switch {
		case strings.Contains(consigne, "security critic"):
			return session(dossier, "sess-critique", "VERDICT: ok")
		case strings.Contains(consigne, "external verifier"):
			return session(dossier, "sess-verif", "VERIFICATION: ok")
		}
		tid := filepath.Base(dossier)
		mu.Lock()
		switch {
		case strings.Contains(consigne, "environment dev: https://"+hoteDev):
			envTache[tid] = "dev"
		case strings.Contains(consigne, "environment prod: https://"+hoteProd):
			envTache[tid] = "prod"
		}
		env, n := envTache[tid], sessions[tid]
		sessions[tid]++
		etapes := etapesTache[tid]
		mu.Unlock()
		if n > 0 { // the plan accepted: each step once, in order; the origin certificate trusted by this PC once issued
			for _, e := range etapes {
				if r := outil(jeton, e[0], e[1]); e[0] == "deployer" && !strings.Contains(r, `"etat": "ok"`) {
					t.Errorf("deployer: %.400s", r)
				}
				if e[0] == "certificat_origine" {
					faireConfiance(env)
				}
			}
			return session(dossier, "sess-ops-"+tid, "Fait.")
		}
		hote := map[string]string{"dev": hoteDev, "prod": hoteProd}[env]
		etapes = [][2]string{{"dns_ecrire", `{"nom":"` + hote + `","type":"A","contenu":"203.0.113.20","proxied":true}`}, {"certificat_origine", `{"nom":"` + hote + `"}`}}
		if env == "dev" { // the first deployment, which chooses dev's method: once the pilot committed M1
			p, _ := cur.projetNomme(projet)
			var head string
			for range 600 {
				if l, _ := gitSortie(p.Dossier, "log", "--format=%s"); strings.Contains(l, "M1:") {
					head, _ = gitSortie(p.Dossier, "rev-parse", "HEAD")
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			etapes = append(etapes, [2]string{"deployer", `{"env":"dev","methode":"statique","source":"artefact","dossier":"site","commit":"` + head + `"}`})
		}
		mu.Lock()
		etapesTache[tid] = etapes
		mu.Unlock()
		var l []string
		for _, e := range etapes {
			l = append(l, `{"outil":"`+e[0]+`","arguments":`+e[1]+`,"pourquoi":"V§1"}`)
		}
		outil(jeton, "proposer_plan", `{"serveur":"`+id+`","resume":"Mettre `+hote+` en ligne","etapes":[`+strings.Join(l, ",")+`]}`)
		return session(dossier, "sess-ops-"+tid, "Plan proposé.")
	}

	// 1. the sentence of V§1, the container mentioned, a new project: the card
	phrase := "Développe un site vitrine pour la boulangerie Martin. Mets une version de dev protégée par mot de passe sur dev.martin.fr " +
		"et la prod sur martin.fr, sur mon @vps, de la façon la plus propre et la plus sûre possible."
	corps, _ := json.Marshal(map[string]any{"phrase": phrase, "mentions": []map[string]string{{"type": "serveur", "ref": id}, {"type": "nouveau"}}})
	v := appel("POST", "/api/missions", string(corps), http.StatusOK)
	mid, _ := v["mission"].(map[string]any)["id"].(string)
	for i := 0; i < 400 && orchestrateurTravaille(mid); i++ {
		time.Sleep(25 * time.Millisecond)
	}
	var lu struct{ Mission Mission }
	_ = json.Unmarshal([]byte(enJSON(appel("GET", "/api/missions/"+mid, "", http.StatusOK))), &lu)
	m := lu.Mission
	var c CarteMission
	_ = json.Unmarshal(m.Carte, &c)
	if m.Etat != "proposee" || c.Projet != (ProjetCarte{Nom: projet, Nouveau: true}) || len(c.Envs) != 2 || c.Envs[0].Protection != "auth_proxy" || c.Envs[1].URL != "https://"+hoteProd ||
		len(c.Acces) != 1 || c.Acces[0].ID != cf.ID || c.Acces[0].Decoche || !slices.Equal(c.Touche.Zones, []string{"martin.fr"}) ||
		!slices.Contains(c.Approbations, ApprobationPrevue{"mise_en_prod", "https://" + hoteProd}) {
		t.Fatalf("card: %s %+v", m.Etat, c)
	}

	// 2. Lancer (proof of presence): the project, its environments (with the zone's provider), the pilot, both Servers tasks
	stocke, _ := lireMission(mid) // the print the window received (the map above reorders the card)
	appel("POST", "/api/missions/"+mid+"/lancer", `{"mot_de_passe":"`+mdp+`","empreinte":"`+empreinteCarte(stocke)+`"}`, http.StatusOK)
	m, _ = lireMission(mid)
	p, ok := cur.projetNomme(projet)
	if !ok || m.Etat != "lancee" || len(m.Voies) != 3 || m.Voies[1].Module != "servers" || m.Voies[2].Module != "servers" {
		t.Fatalf("launched: %+v", m)
	}
	envs := cur.acces.Environnements(projet)
	if d, pr := envs["dev"], envs["prod"]; d.Protection != "auth_proxy" || !d.DeployerAuJalon || d.Fournisseur != "f1" || pr.Politique != "accord" || pr.Fournisseur != "f1" {
		t.Fatalf("environments: %+v %+v", d, pr)
	}
	// M273: the Studio project asks for no server's access: its request says the mission deploys it, a task asking for one is refused
	if dem, _ := os.ReadFile(p.Demande()); !strings.Contains(string(dem), "no request for a server's access") {
		t.Errorf("the project's request has no deployment note: %q", dem)
	}
	t.Setenv("PROJECT", p.Dossier)
	if _, se, code := lancerAvecEntree(t, []byte("utilisateur@hôte SSH et port"), func() int { return lancerTodo([]string{"add", "Accès au serveur"}) }); code == 0 || !strings.Contains(se, "Servers lane") {
		t.Errorf("a server's access asked by the Studio project: %d %q", code, se)
	}
	// M249: the new project's framing writes its plan; the follower's next reading starts both Servers preparations
	ecrireFichier(t, p.Plan(), "# Plan\n\n- [ ] M1 : Le site vitrine\n", 0o644)
	cur.observerMission(mid)
	m, _ = lireMission(mid)
	taches := map[string]string{}
	for _, voie := range m.Voies[1:] {
		x, _ := tacheMission(voie.Ref)
		taches[strings.TrimPrefix(x.Env, projet+"/")] = voie.Ref
	}
	if taches["dev"] == "" || taches["prod"] == "" {
		t.Fatalf("tasks: %v", taches)
	}

	// 3. the fake pilot: the plan, then M1 (site v1) committed and ticked
	plan := func(m1, m2 bool) {
		x := map[bool]string{true: "x", false: " "}
		ecrireFichier(t, p.Plan(), "# Plan\n\n- ["+x[m1]+"] M1 : Accueil et produits\n- ["+x[m2]+"] M2 : Horaires, plan d'accès et contact\n", 0o644)
	}
	livrer := func(version, message string) string {
		site := filepath.Join(p.Dossier, "site")
		_ = os.RemoveAll(site)
		if err := os.CopyFS(site, os.DirFS(filepath.Join("testdata", "site-exemple", version))); err != nil {
			t.Fatal(err)
		}
		git(p.Dossier, "add", "site", ".athanor/PLAN.md")
		git(p.Dossier, "commit", "-q", "-m", message)
		return git(p.Dossier, "rev-parse", "HEAD")
	}
	if _, err := os.Stat(filepath.Join(p.Dossier, ".git")); err != nil {
		git(p.Dossier, "init", "-q")
	}
	faits := map[string]map[string]bool{}
	plan(false, false)
	observerPlan(p, faits, fichierJalonsFaits(), time.Now()) // the hub's watcher learns the plan
	plan(true, false)
	c1 := livrer("v1", "M1: accueil et produits")
	// M1 is in the journal; its dev deployment is the chef ops's (the first one chooses the method): the relay starts at M2
	if n := observerPlan(p, faits, fichierJalonsFaits(), time.Now()); len(n) != 1 || !strings.HasPrefix(n[0].Titre, "M1") {
		t.Fatalf("M1 not seen: %+v", n)
	}

	// 4. the chef ops's plans: dev's waits as the card planned (plan_serveurs dev); prod's always waits for the owner
	attendre := func(tid string, delai time.Duration, etats ...string) TacheServeurs {
		t.Helper()
		for fin := time.Now().Add(delai); time.Now().Before(fin); time.Sleep(100 * time.Millisecond) {
			if x, ok := tacheMission(tid); ok && slices.Contains(etats, x.Etat) {
				return x
			}
		}
		x, _ := tacheMission(tid)
		t.Fatalf("task %s never reached %v: %+v", tid, etats, x)
		return x
	}
	accepter := func(env string) bool {
		t.Helper()
		x := attendre(taches[env], time.Minute, "attente_accord", "finie", "echec", "arretee")
		if x.Etat != "attente_accord" {
			return false
		}
		if _, _, err := cur.deciderApprobation("fenetre", x.Plans[len(x.Plans)-1], "accepter", "", "", ""); err != nil {
			t.Fatal(err)
		}
		return true
	}
	if attendu := slices.Contains(c.Approbations, ApprobationPrevue{"plan_serveurs", "dev"}); accepter("dev") != attendu {
		t.Errorf("dev plan waited for the owner: %v, the card said %v", !attendu, attendu)
	}
	if x := attendre(taches["prod"], time.Minute, "attente_accord", "finie", "echec", "arretee"); x.Etat != "attente_accord" {
		t.Fatalf("prod plan not waiting for the owner: %+v", x)
	}
	if fin := attendre(taches["dev"], 3*time.Minute, "finie", "echec", "arretee"); fin.Etat != "finie" {
		t.Fatalf("dev task: %s %q", fin.Etat, fin.Resultat)
	}
	appelsDev := strings.Join(relever(), "\n")
	if strings.Contains(appelsDev, `"name":"`+hoteProd+`"`) || !strings.Contains(appelsDev, `"name":"`+hoteDev+`"`) || !strings.Contains(appelsDev, "POST /certificates") {
		t.Errorf("Cloudflare calls before the prod plan's accord:\n%s", appelsDev)
	}
	if !accepter("prod") {
		t.Fatal("prod plan")
	}
	if fin := attendre(taches["prod"], time.Minute, "finie", "echec", "arretee"); fin.Etat != "finie" {
		t.Fatalf("prod task: %s %q", fin.Etat, fin.Resultat)
	}
	if a := strings.Join(relever(), "\n"); !strings.Contains(a, `"name":"`+hoteProd+`"`) || !strings.Contains(a, "POST /certificates") {
		t.Errorf("Cloudflare calls of the prod plan:\n%s", a)
	}
	hd := historiqueDeploiements(projet, "dev")
	if len(hd) != 1 || hd[0].Commit != c1 || hd[0].Auteur != "agent" || hd[0].Etat != "ok" || hd[0].Verification == nil || !hd[0].Verification.OK {
		t.Fatalf("dev v1 by the chef ops: %s", enJSON(hd))
	}
	if b := ssh("sudo -n cat /etc/caddy/sites/" + hoteDev + ".caddy"); !strings.Contains(b, "basicauth") || !strings.Contains(b, "tls "+dossierTLS+"/"+hoteDev+".crt") {
		t.Errorf("dev's Caddy site: %q", b)
	}
	devSert("v1", "v1")
	if len(historiqueDeploiements(projet, "prod")) != 0 || ssh("test -e /srv/athanor/"+projet+"-prod && echo present; true") != "" {
		t.Fatal("prod deployed by the chef ops")
	}
	observer := func() Mission {
		t.Helper()
		for range 2 {
			cur.observerMission(mid)
		}
		m, _ := lireMission(mid)
		return m
	}
	if m = observer(); m.Relais == nil || m.Relais.Etape != "dev_verifie" || m.Relais.Accord != "" {
		t.Fatalf("after M1 (the pilot still works): %+v\nlanes: %s\nhistory: %s\nevents: %s", m.Relais, enJSON(cur.voiesEnDirect(m)), enJSON(historiqueDeploiements(projet, "dev")), enJSON(m.Historique))
	}

	// 5. M2 (site v2) committed and ticked, the request done → the milestone's relay deploys v2 to dev, checked
	plan(true, true)
	c2 := livrer("v2", "M2: horaires, plan d'accès et contact")
	ecrireFichier(t, filepath.Join(p.Atelier(), "FINAL_REPORT.md"), "# Rapport final\n\nSTATUS: DONE\n", 0o644)
	ecrireFichier(t, p.EtatJSON(), `{"phase":"termine"}`, 0o644)
	n := observerPlan(p, faits, fichierJalonsFaits(), time.Now())
	if len(n) != 1 || !strings.HasPrefix(n[0].Titre, "M2") {
		t.Fatalf("M2 not seen: %+v", n)
	}
	cur.noterJalonRelais(p, n[0].Titre, time.Now())
	ch := cur.avancerRelais(p, time.Now())
	if ch == nil {
		t.Fatalf("relay not started: %+v", lireRelais()[projet])
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Minute):
		t.Fatal("the relay's deployment did not end")
	}
	hd = historiqueDeploiements(projet, "dev")
	if d := hd[len(hd)-1]; len(hd) != 2 || d.Commit != c2 || d.Auteur != "relais" || d.Etat != "ok" || d.Verification == nil || !d.Verification.OK {
		t.Fatalf("dev v2 by the relay: %s", enJSON(hd))
	}
	devSert("v2", "v2")
	// dev checked on HEAD, the Studio lane done, the prod task done: the prod accord is asked, nothing deployed
	m = observer()
	accord := ""
	if m.Relais != nil {
		accord = m.Relais.Accord
	}
	if x, _ := cur.approbations.Lire(accord); m.Relais.Etape != "accord_prod" || x.Etat != "attente" || x.Details["sha"] != c2 || x.Env != "prod" ||
		len(historiqueDeploiements(projet, "prod")) != 0 {
		t.Fatalf("prod accord: %+v %+v", m.Relais, x)
	}

	// 6. Athanor stops before the accord is given: its follower dies with it, the vault closes; a new Serveur starts on
	// the same config and files (as main: nouveauServeur, then reprendreMissions after the vault and the tokens)
	suivisMission.Delete(mid) // the old process's follower (asleep for an hour) is gone with it
	s.coffre.Verrouiller()
	s2 := nouveauServeur(lireConfig())
	modeNatif.Store(false) // nouveauServeur sets it again on Windows (SOLUTIONS)
	if s2.coffre == nil || s2.coffreErr != nil || s2.approbations == nil {
		t.Fatalf("restart: vault %v", s2.coffreErr)
	}
	s2.coffre.Verrouiller() // the infra key waits for the password, as after a real start
	cur = s2
	interfaceDe(s2)
	s2.reprendreMissions()
	m, _ = lireMission(mid)
	if ids, _ := journalCles(m); !slices.ContainsFunc(ids, func(i string) bool { return strings.HasPrefix(i, "redemarrage:") }) || m.Etat != "lancee" {
		t.Fatalf("after the restart: %s %v", m.Etat, ids)
	}
	if x, _ := s2.approbations.Lire(accord); x.Etat != "attente" {
		t.Fatalf("the pending accord after the restart: %+v", x)
	}

	// 7. the paired phone: locked, it decides nothing; unlocked (the vault opens, the mission resumes), it accepts
	decider := func(decision string) int {
		return appelDistance(h, "POST", "/api/hub/approbations", tel, `{"id":"`+accord+`","decision":"`+decision+`"}`, telAPI...).Code
	}
	if code := decider("accepter"); code == http.StatusOK {
		t.Fatal("a locked phone decides")
	}
	if w := appelDistance(h, "POST", "/api/deverrouiller", tel, `{"mot_de_passe":"`+mdp+`"}`, telAPI...); w.Code != http.StatusOK || !s2.etatCoffre().Infra {
		t.Fatalf("phone unlocks: %d %s", w.Code, w.Body)
	}
	if code := decider("accepter"); code != http.StatusOK {
		t.Fatalf("the phone accepts: %d", code)
	}
	for fin := time.Now().Add(3 * time.Minute); time.Now().Before(fin); time.Sleep(time.Second) {
		if m = observer(); m.Etat != "lancee" {
			break
		}
	}
	hp := historiqueDeploiements(projet, "prod")
	if m.Etat != "terminee" || m.Relais.Etape != "termine" || len(hp) != 1 || hp[0].Commit != c2 || hp[0].Auteur != "relais" || !hp[0].Accorde ||
		hp[0].Verification == nil || !hp[0].Verification.OK {
		t.Fatalf("prod: %s %+v %s", m.Etat, m.Relais, enJSON(hp))
	}
	if x, _ := s2.approbations.Lire(accord); x.Etat != "acceptee" || !strings.HasPrefix(x.Par, "appareil:") || x.Utilisee == "" {
		t.Errorf("the accord: %+v", x)
	}
	if code, corps := page(hoteProd, ""); code != http.StatusOK || !strings.Contains(corps, "site-exemple v2") {
		t.Errorf("prod serves: %d %.120q", code, corps)
	}

	// 8. Revenir on dev (the owner's route): v1 served again, checked
	r := appel("POST", "/api/projets/"+projet+"/revenir", `{"env":"dev"}`, http.StatusOK)
	var d Deploiement
	_ = json.Unmarshal([]byte(enJSON(r["deploiement"])), &d)
	if d.Etat != "ok" || !d.Retour || d.Verification == nil || !d.Verification.OK {
		t.Errorf("Revenir: %s", enJSON(r["deploiement"]))
	}
	devSert("after Revenir", "v1")

	// 9. the journal, from end to end: every event once, every key translated; nothing launched twice
	ids, cles := journalCles(m)
	attendus := []string{"jalon:M1", "jalon:M2", "dev:termine", "deploi:" + hd[0].ID + ":ok", "verif:" + hd[0].ID + ":ok", "deploi:" + hd[1].ID + ":ok",
		"verif:" + hd[1].ID + ":ok", "tache:" + taches["dev"] + ":finie", "tache:" + taches["prod"] + ":finie", "accord:" + accord, "accord:" + accord + ":acceptee",
		"deploi:" + hp[0].ID + ":ok", "verif:" + hp[0].ID + ":ok", "redemarrage:", "deverrouille:", "mission:terminee"}
	for _, a := range attendus {
		if !slices.ContainsFunc(ids, func(i string) bool { return strings.HasPrefix(i, a) }) {
			t.Errorf("journal without %s: %v", a, ids)
		}
	}
	vus := map[string]bool{}
	fr := dicoTUI("fr")
	for i, x := range ids {
		if vus[x] {
			t.Errorf("journal entry twice: %s", x)
		}
		vus[x] = true
		if fr[cles[i]] == "" {
			t.Errorf("journal key not translated: %q", cles[i])
		}
	}
	// launched once: one "environment set" per environment of the card, one pilot start, two Servers tasks
	if n := len(slices.DeleteFunc(slices.Clone(m.Historique), func(e EvenementMission) bool { return !strings.HasPrefix(e.Detail, "environment set: ") })); n != 2 ||
		!slices.Equal(pilotes, []string{projet}) {
		t.Errorf("launched: %d environments set, pilots %v", n, pilotes)
	}
	if l := tachesServeur(id); len(l) != 2 {
		t.Errorf("%d Servers tasks started", len(l))
	}

	// 10. no vault value anywhere: answers, consignes, the mission and its journal, Athanor's files, the project
	temoins := append(valeursCoffreTemoins(t, cur), jetonCF, temoinOutils)
	if len(temoins) < 6 { // token, server key, proxy password, the witness entry, two certificate keys
		t.Fatalf("witnesses missing: %d", len(temoins))
	}
	bm, _ := json.Marshal(m)
	mu.Lock()
	textes := map[string]string{"an API answer": reponses.String(), "an MCP answer": vu.String(), "a consigne": strings.Join(consignes, "\n"), "the mission": string(bm)}
	mu.Unlock()
	for ou, texte := range textes {
		for _, v := range temoins {
			if strings.Contains(texte, v) {
				t.Errorf("a vault value in %s: %.12q…", ou, v)
			}
		}
	}
	for _, dossier := range []string{dossierAthanor(), p.Dossier} {
		if f := fichiersAvec(t, dossier, temoins, filepath.Base(os.Args[0])); len(f) > 0 {
			t.Errorf("a vault value in files of %s: %v", dossier, f)
		}
	}
	t.Logf("P5 in %s", time.Since(debut).Round(time.Second))
}
