package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// TestSansSessionLangue: a page asked without a session says it in Athanor's language first, then in English (N9).
func TestSansSessionLangue(t *testing.T) {
	s := &Serveur{config: Config{Langue: "fr"}, cleAcces: "x"}
	w := httptest.NewRecorder()
	if s.pageAcces(w, httptest.NewRequest("GET", "/", nil)) {
		t.Fatal("opened without a session")
	}
	fr, en := strings.Index(w.Body.String(), "Ouvrez Athanor"), strings.Index(w.Body.String(), "Open Athanor")
	if w.Code != 403 || fr < 0 || en < fr {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
