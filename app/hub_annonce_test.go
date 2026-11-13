package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestAnnonceInstances (M185): two test servers announce themselves in one folder and see each other; an
// entry whose pid is dead, whose port is silent or which answers another id is skipped then deleted;
// /api/annonce says the id and the version only; nothing is linked without the other one's code.
func TestAnnonceInstances(t *testing.T) {
	s := serveurCoffreTest(t)
	ancien := pauseRefusClient
	pauseRefusClient = 0
	t.Cleanup(func() { pauseRefusClient = ancien })
	_ = os.Remove(cheminClients())
	t.Cleanup(func() { _ = os.Remove(cheminClients()) })

	dossier := t.TempDir()
	instance := func(id string) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/annonce", repondreAnnonce(func() string { return id }))
		mux.HandleFunc("POST /api/mcp/appairer", s.apiAppairerClient)
		h := httptest.NewServer(mux)
		t.Cleanup(h.Close)
		port, _ := strconv.Atoi(h.URL[strings.LastIndex(h.URL, ":")+1:])
		b, _ := json.Marshal(Annonce{ID: id, Nom: "pc", Version: versionAthanor, Port: port, PID: os.Getpid(), Modules: []string{"hub", "dev"}})
		if err := os.WriteFile(filepath.Join(dossier, id+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		return h
	}
	a := instance("aaaaaaaaaaaa")
	instance("bbbbbbbbbbbb")

	// the public route: the id and the version, nothing else
	r, err := http.Get(a.URL + "/api/annonce")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	_ = json.NewDecoder(r.Body).Decode(&v)
	r.Body.Close()
	if len(v) != 2 || v["id"] != "aaaaaaaaaaaa" || v["version"] != versionAthanor {
		t.Errorf("/api/annonce = %v, want {id, version} only", v)
	}

	// dead entries: a pid gone, a silent port, another id at that port
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	muet := l.Addr().(*net.TCPAddr).Port
	l.Close()
	portA := a.Listener.Addr().(*net.TCPAddr).Port
	morts := map[string]Annonce{
		"cccccccccccc": {ID: "cccccccccccc", Port: portA, PID: 0},
		"dddddddddddd": {ID: "dddddddddddd", Port: muet, PID: os.Getpid()},
		"eeeeeeeeeeee": {ID: "eeeeeeeeeeee", Port: portA, PID: os.Getpid()}, // A answers its own id
	}
	for id, m := range morts {
		b, _ := json.Marshal(m)
		_ = os.WriteFile(filepath.Join(dossier, id+".json"), b, 0o600)
	}
	liste := lireAnnonces(dossier)
	if len(liste) != 2 || liste[0].ID != "aaaaaaaaaaaa" || liste[1].ID != "bbbbbbbbbbbb" {
		t.Fatalf("live instances = %+v, want A and B", liste)
	}
	for id := range morts {
		if _, err := os.Stat(filepath.Join(dossier, id+".json")); err == nil {
			t.Errorf("dead entry %s not deleted", id)
		}
	}

	// no link without the code: no code, a wrong code, an unknown instance
	for _, c := range []struct{ id, code string }{{"bbbbbbbbbbbb", ""}, {"bbbbbbbbbbbb", "12ab56"}, {"bbbbbbbbbbbb", "000000"}, {"ffffffffffff", "123456"}} {
		if err := s.appairerInstance(dossier, c.id, c.code); err == nil {
			t.Errorf("pairing %s with %q accepted", c.id, c.code)
		}
	}
	nb := func() (int, int, string) {
		v := s.vueClients()
		d, c := v["demandes"].([]demandeClient), v["clients"].([]ClientMCP)
		if len(d) == 1 {
			return len(d), len(c), d[0].Sorte
		}
		return len(d), len(c), ""
	}
	if d, c, _ := nb(); d != 0 || c != 0 {
		t.Fatalf("%d requests, %d clients without the code", d, c)
	}
	// with the code the other one shows: a request waiting for its owner's yes (still no client, no token)
	m := s.cli()
	m.mu.Lock()
	m.code, m.codeFin, m.essais = "482913", time.Now().Add(dureeCodeDirect), 0
	code := m.code
	m.mu.Unlock()
	if err := s.appairerInstance(dossier, "bbbbbbbbbbbb", code); err != nil {
		t.Fatal(err)
	}
	if d, c, sorte := nb(); d != 1 || sorte != "athanor" || c != 0 {
		t.Errorf("after the code: %d requests (%s), %d clients; want one athanor request and no client", d, sorte, c)
	}
	demandesInstances.Lock()
	_, ok := demandesInstances.m["bbbbbbbbbbbb"]
	demandesInstances.Unlock()
	if !ok {
		t.Error("the request is not kept for the remote Athanors (M187)")
	}
}
