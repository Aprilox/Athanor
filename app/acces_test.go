package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccesFenetresSeulement(t *testing.T) {
	s := nouveauServeur(Config{Kit: t.TempDir()})
	s.csrf, s.cleAcces = "jeton", "cle"
	mux := http.NewServeMux()
	s.routes(mux)
	appel := func(cookie string) int {
		r := httptest.NewRequest("GET", "/api/etat", nil)
		r.Header.Set("X-Athanor", "jeton")
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: cookieAcces, Value: cookie})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w.Code
	}
	if c := appel(""); c != http.StatusForbidden {
		t.Fatalf("API without the window cookie: %d", c)
	}
	if c := appel("autre"); c != http.StatusForbidden {
		t.Fatalf("API with a wrong cookie: %d", c)
	}
	if c := appel("cle"); c != http.StatusOK {
		t.Fatalf("API from a window: %d", c)
	}
	page := func(u string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		if s.pageAcces(w, httptest.NewRequest("GET", u, nil)) {
			w.WriteHeader(299) // the page would be served
		}
		return w
	}
	if w := page("/"); w.Code != http.StatusForbidden {
		t.Fatalf("page without cookie: %d", w.Code)
	}
	lien := s.lienOuverture("http://127.0.0.1:1/")
	u := lien[strings.Index(lien, "/?"):]
	if w := page(u); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Set-Cookie"), cookieAcces+"=cle") || !strings.Contains(w.Header().Get("Set-Cookie"), "HttpOnly") {
		t.Fatalf("single-use address: %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	if w := page(u); w.Code != http.StatusForbidden {
		t.Fatalf("address used twice: %d", w.Code)
	}
}
