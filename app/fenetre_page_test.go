package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestFenetrePage: a detached terminal or SFTP opens in a window of its own with a fresh opening link; any other page,
// or no link to make, opens nothing.
func TestFenetrePage(t *testing.T) {
	s := &Serveur{}
	ouvertes := make(chan string, 4)
	avantLien, avantOuvrir := lienApp, ouvrirFenetrePage
	t.Cleanup(func() { lienApp, ouvrirFenetrePage = avantLien, avantOuvrir })
	ouvrirFenetrePage = func(u string) { ouvertes <- u }
	appel := func(corps string) int {
		w := httptest.NewRecorder()
		s.apiFenetrePage(w, httptest.NewRequest("POST", "/api/fenetre/page", strings.NewReader(corps)))
		return w.Code
	}
	lienApp = func() string { return "" }
	if code := appel(`{"page":"terminal/c0ffee01"}`); code != http.StatusConflict {
		t.Errorf("no opening link: %d", code)
	}
	lienApp = func() string { return "http://127.0.0.1:9/?ouvrir=j1" }
	for _, p := range []string{"", "projets", "terminal/../reglages", "terminal/c0ffee01/sftp/x", "https://exemple.org", "terminal/c0ffee01#x"} {
		if code := appel(`{"page":"` + p + `"}`); code != http.StatusBadRequest {
			t.Errorf("%q: %d", p, code)
		}
	}
	for _, p := range []string{"terminal/c0ffee01", "terminal/c0ffee01/sftp"} {
		if code := appel(`{"page":"` + p + `"}`); code != http.StatusOK {
			t.Fatalf("%q: %d", p, code)
		}
		select {
		case u := <-ouvertes:
			if u != "http://127.0.0.1:9/?ouvrir=j1#"+p {
				t.Errorf("opened %q", u)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q: no window opened", p)
		}
	}
	if len(ouvertes) != 0 {
		t.Error("a refused page opened a window")
	}
}
