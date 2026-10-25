package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func attendreDomaine(t *testing.T, s *Serveur, ok func(DistanceVue) bool) DistanceVue {
	t.Helper()
	var v DistanceVue
	for i := 0; i < 300; i++ {
		v = s.vueDistance()
		if ok(v) {
			return v
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("domain: etat %q adresse %q domaine %q etape %q message %q", v.Etat, v.Adresse, v.Domaine, v.DomaineEtape, v.DomaineMessage)
	return v
}

func TestDomaineCloudflare(t *testing.T) {
	kit := t.TempDir()
	trace := filepath.Join(t.TempDir(), "trace")
	t.Setenv("ATHANOR_FAUX_TRACE", trace)
	s := nouveauServeur(Config{Kit: kit})
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	s.mux = mux
	s.config.Distance = DistanceConfig{Actif: true, Port: 45558}
	installerFauxCloudflared(t, s)
	t.Cleanup(s.arreterTunnel)
	fenetre := func(corps string) (int, string) {
		r := httptest.NewRequest("POST", "/api/distance/domaine", strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}

	// the quick tunnel first
	go s.lancerTunnel()
	rapide := attendreTunnel(t, s, "")
	if !strings.HasSuffix(rapide.adresse, ".trycloudflare.com") {
		t.Fatalf("quick tunnel: %+v", rapide)
	}
	if c, _ := fenetre(`{"action":"connecter","hote":"pas un nom"}`); c != http.StatusBadRequest {
		t.Fatalf("bad host name: %d", c)
	}

	// the sign-in fails (the browser closed): said, nothing changed
	t.Setenv("ATHANOR_FAUX_LOGIN_ECHEC", "1")
	if c, b := fenetre(`{"action":"connecter","hote":"athanor.example.com"}`); c != http.StatusOK {
		t.Fatalf("connect: %d %s", c, b)
	}
	v := attendreDomaine(t, s, func(v DistanceVue) bool { return v.DomaineEtape == "erreur" })
	if !strings.Contains(v.DomaineMessage, "sign-in") || v.Domaine != "" {
		t.Fatalf("failed sign-in: %+v", v)
	}
	t.Setenv("ATHANOR_FAUX_LOGIN_ECHEC", "")

	// sign-in, tunnel, host name: the remote access moves to the fixed address
	if c, b := fenetre(`{"action":"connecter","hote":"https://Athanor.Example.com/"}`); c != http.StatusOK {
		t.Fatalf("connect: %d %s", c, b)
	}
	v = attendreDomaine(t, s, func(v DistanceVue) bool {
		return v.Domaine != "" && v.Etat == "pret" && v.Adresse == "https://athanor.example.com"
	})
	if v.DomaineEtape != "" {
		t.Fatalf("setup state left: %+v", v)
	}
	id := s.config.Distance.TunnelID
	cred, cfg := s.fichiersTunnelNomme(id)
	conf, _ := os.ReadFile(cfg)
	if !reUUID.MatchString(id) || !fichierExiste(cred) || !fichierExiste(s.certCloudflared()) ||
		!strings.Contains(string(conf), "url: http://127.0.0.1:45558") || !strings.Contains(string(conf), "tunnel: "+id) {
		t.Fatalf("files: id %q cred %v cert %v conf %s", id, fichierExiste(cred), fichierExiste(s.certCloudflared()), conf)
	}
	if !strings.HasPrefix(s.certCloudflared(), kit) {
		t.Fatal("the account's certificate outside Athanor's folder")
	}
	b, _ := os.ReadFile(trace)
	if !strings.Contains(string(b), "route dns "+id+" athanor.example.com") || !strings.Contains(string(b), "--config "+cfg+" run "+id) {
		t.Fatalf("steps: %s", b)
	}
	// only the domain's name is answered now
	h := s.gestionDistance(mux)
	for hote, code := range map[string]int{"athanor.example.com": http.StatusUnauthorized, strings.TrimPrefix(rapide.adresse, "https://"): http.StatusForbidden} {
		r := httptest.NewRequest("GET", "https://"+hote+"/", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != code {
			t.Fatalf("%s: %d, not %d", hote, w.Code, code)
		}
	}

	// Athanor restarts: the same tunnel, kept
	nomme := attendreTunnel(t, s, rapide.adresse)
	s.arreterTunnelSansTuer()
	s2 := nouveauServeur(Config{Kit: kit})
	s2.config.Distance = s.config.Distance
	t.Cleanup(s2.arreterTunnel)
	go s2.lancerTunnel()
	if e := attendreTunnel(t, s2, ""); e.pid != nomme.pid || e.adresse != "https://athanor.example.com" {
		t.Fatalf("kept: %+v, before %+v", e, nomme)
	}
	s2.arreterTunnelSansTuer()

	// a host name already used in the DNS: refused, the new tunnel deleted, the domain in use kept
	_ = os.WriteFile(trace, nil, 0o644)
	if c, _ := fenetre(`{"action":"connecter","hote":"pris.example.com"}`); c != http.StatusOK {
		t.Fatal(c)
	}
	v = attendreDomaine(t, s, func(v DistanceVue) bool { return v.DomaineEtape == "erreur" })
	b, _ = os.ReadFile(trace)
	if !strings.Contains(v.DomaineMessage, "already exists") || v.Domaine != "athanor.example.com" || !strings.Contains(string(b), "delete -f") || strings.Contains(string(b), "tunnel login") {
		t.Fatalf("taken name: %q %q %q\n%s", v.DomaineEtape, v.DomaineMessage, v.Domaine, b)
	}

	// back to the quick tunnel: deleted at Cloudflare, nothing left here
	if c, _ := fenetre(`{"action":"oublier"}`); c != http.StatusOK {
		t.Fatal(c)
	}
	v = attendreDomaine(t, s, func(v DistanceVue) bool {
		return v.Domaine == "" && v.Etat == "pret" && strings.HasSuffix(v.Adresse, ".trycloudflare.com")
	})
	if fichierExiste(cred) || fichierExiste(cfg) || fichierExiste(s.certCloudflared()) || s.config.Distance.TunnelID != "" {
		t.Fatal("files of the domain left")
	}
}
