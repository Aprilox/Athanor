package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// TestMdpCourt2x: a 2.x password (4 characters were enough, stored in the old sha256 format) still unlocks
// 3.0.0, which then asks for a new one of 12 characters at once, after a reload too, until it is changed.
func TestMdpCourt2x(t *testing.T) {
	for _, court := range []string{"12" + "34", "1234" + "5678"} { // 2.x's shortest, and 8 digits
		t.Run(strconv.Itoa(len(court)), func(t *testing.T) { mdpCourt2x(t, court) })
	}
}

func mdpCourt2x(t *testing.T, court string) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	s.config.Sel, s.session = "sel-"+aleatoire(4), ""
	s.config.MotDePasseHash = ancienHashMdp(s.config.Sel, court)
	session := ""
	appel := func(methode, chemin, corps string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(methode, chemin, strings.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		if session != "" {
			r.AddCookie(&http.Cookie{Name: "athanor_session", Value: session})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		for _, c := range w.Result().Cookies() {
			if c.Name == "athanor_session" {
				session = c.Value
			}
		}
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	if _, v := appel("GET", "/api/etat", ""); v["verrouille"] != true || v["changer_mdp"] != nil {
		t.Fatalf("locked: %v", v)
	}
	if c, v := appel("POST", "/api/deverrouiller", `{"mot_de_passe":"`+court+`"}`); c != 200 || v["changer_mdp"] != true {
		t.Fatalf("the 2.x password: %d %v, want unlocked and asked to change", c, v)
	}
	if !strings.HasPrefix(s.config.MotDePasseHash, prefixeMdp+"$") || s.config.Sel != "" {
		t.Error("the old hash was not rewritten in the new format")
	}
	if _, v := appel("GET", "/api/etat", ""); v["verrouille"] != false || v["changer_mdp"] != true {
		t.Fatalf("after a reload: %v, want the change asked again", v)
	}
	if c, _ := appel("POST", "/api/mot-de-passe", `{"ancien":"`+court+`","nouveau":"`+court+`"}`); c != http.StatusBadRequest {
		t.Errorf("the same short password again: %d, want 400", c)
	}
	long := "nouveau-" + aleatoire(8)
	if c, v := appel("POST", "/api/mot-de-passe", `{"ancien":"`+court+`","nouveau":"`+long+`"}`); c != 200 {
		t.Fatalf("change: %d %v", c, v)
	}
	if _, v := appel("GET", "/api/etat", ""); v["changer_mdp"] != nil {
		t.Errorf("after the change: %v", v)
	}
	appel("POST", "/api/verrouiller", "")
	if c, v := appel("POST", "/api/deverrouiller", `{"mot_de_passe":"`+long+`"}`); c != 200 || v["changer_mdp"] != false {
		t.Errorf("a 12-character password: %d %v", c, v)
	}
}
