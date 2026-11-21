package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestColleReconnaissance: the type, family, scope and hint proposed for each kind of pasted text. The
// fake tokens are built at run time from a prefix and random characters: no fake secret in the repository.
func TestColleReconnaissance(t *testing.T) {
	alea := func(n int) string { return aleatoire(n)[:n] }
	maj := func(n int) string { return strings.ToUpper(alea(n)) }
	gh, ghPat, gho := "ghp_"+alea(36), "github_pat_"+alea(40), "gho_"+alea(36)
	ant, oai := "sk-ant-"+alea(40), "sk-"+alea(40)
	stripe, stripePk, aws := "sk_"+"live_"+alea(24), "pk_"+"test_"+alea(24), "AKIA"+maj(16)
	pem := "-----BEGIN OPENSSH " + "PRIVATE KEY-----\n" + alea(64) + "\n-----END OPENSSH PRIVATE KEY-----"
	cert := "-----BEGIN " + "CERTIFICATE-----\n" + alea(64) + "\n-----END CERTIFICATE-----"
	type att struct{ nom, famille, typ, portee, indice string }
	cas := []struct {
		nom, texte string
		veut       []att
	}{
		{"GitHub ghp_", gh, []att{{"GITHUB_TOKEN", "secret", "github", "infra", "ghp_…" + gh[len(gh)-2:]}}},
		{"GitHub fine-grained", ghPat, []att{{"GITHUB_TOKEN", "secret", "github", "infra", "github_pat_…" + ghPat[len(ghPat)-2:]}}},
		{"GitHub OAuth", "  " + gho + "\n", []att{{"GITHUB_TOKEN", "secret", "github", "infra", "gho_…" + gho[len(gho)-2:]}}},
		{"Anthropic", ant, []att{{"ANTHROPIC_API_KEY", "secret", "anthropic", "code", "sk-ant-…" + ant[len(ant)-2:]}}},
		{"OpenAI", oai, []att{{"OPENAI_API_KEY", "secret", "openai", "code", "sk-…" + oai[len(oai)-2:]}}},
		{"Stripe secret", stripe, []att{{"STRIPE_SECRET_KEY", "secret", "stripe", "code", "sk_live_…" + stripe[len(stripe)-2:]}}},
		{"Stripe publishable", stripePk, []att{{"STRIPE_PUBLISHABLE_KEY", "secret", "stripe", "code", "pk_test_…" + stripePk[len(stripePk)-2:]}}},
		{"AWS", aws, []att{{"AWS_ACCESS_KEY_ID", "secret", "aws", "infra", "AKIA…" + aws[len(aws)-2:]}}},
		{"SSH key", pem, []att{{"SSH_PRIVATE_KEY", "secret", "ssh", "infra", "OPENSSH PRIVATE KEY"}}},
		{"certificate", cert, []att{{"TLS_CERTIFICATE", "secret", "certificat", "infra", "CERTIFICATE"}}},
		{"unknown token", "q" + alea(30), []att{{"TOKEN", "secret", "jeton", "code", ""}}},
		{"short password", "abc" + alea(4), []att{{"TOKEN", "secret", "jeton", "code", "…"}}},
		{"placeholder", "YOUR_API_KEY", nil},
		{"placeholder 2", "<your-token>", nil},
		{".env block", "# comment\nexport CLOUDFLARE_API_TOKEN=\"" + alea(40) + "\"\nNODE_ENV=production\nAPI_KEY=your_key_here\nDB_PASSWORD='" + alea(12) + "'\nGH=" + gh + "\nDATABASE_URL=postgres://u:" + alea(8) + "@db.test/x\nPORT=8080 # web\n",
			[]att{{"CLOUDFLARE_API_TOKEN", "secret", "cloudflare", "infra", ""}, {"NODE_ENV", "variable", "autre", "code", "production"},
				{"DB_PASSWORD", "secret", "mot_de_passe", "code", ""}, {"GH", "secret", "github", "infra", ""},
				{"DATABASE_URL", "secret", "mot_de_passe", "code", ""}, {"PORT", "variable", "autre", "code", "8080"}}},
		{"MCP config", `{"mcpServers":{"x":{"command":"npx","args":["-y","pkg"],"env":{"OPENAI_API_KEY":"` + oai + `","LOG_LEVEL":"debug","TOKEN":"<token>"},"headers":{"Authorization":"Bearer ` + gh + `"}}}}`,
			[]att{{"AUTHORIZATION", "secret", "github", "infra", "ghp_…" + gh[len(gh)-2:]}, {"OPENAI_API_KEY", "secret", "openai", "code", ""}}},
		{"base64 token is not NAME=value", "dGVzdA" + alea(10) + "==", []att{{"TOKEN", "secret", "jeton", "code", ""}}},
		{"credentials in a URL", "REDIS_URL=redis://:" + alea(8) + "@h.test:6379\nAPP_DB=postgres://h.test/db?user=u&password=" + alea(8) + "\nHOMEPAGE=https://site.test:8443/a?b=c\n",
			[]att{{"REDIS_URL", "secret", "mot_de_passe", "code", ""}, {"APP_DB", "secret", "mot_de_passe", "code", ""}, {"HOMEPAGE", "variable", "autre", "code", ""}}},
		{"empty", "  \n ", nil},
	}
	for _, c := range cas {
		props := analyserColle(c.texte)
		if len(props) != len(c.veut) {
			t.Errorf("%s: %d proposals %+v, want %d", c.nom, len(props), props, len(c.veut))
			continue
		}
		for i, v := range c.veut {
			p := props[i]
			if p.Nom != v.nom || p.Famille != v.famille || p.Type != v.typ || p.Portee != v.portee || (v.indice != "" && p.Indice != v.indice) {
				t.Errorf("%s #%d: %s/%s/%s/%s %q, want %+v", c.nom, i, p.Nom, p.Famille, p.Type, p.Portee, p.Indice, v)
			}
			if p.valeur == "" {
				t.Errorf("%s #%d: no value kept", c.nom, i)
			}
			if p.Famille == "secret" && len(p.valeur) > 6 && strings.Contains(p.Indice, p.valeur[len(p.valeur)-6:]) {
				t.Errorf("%s #%d: the hint shows the value: %q", c.nom, i, p.Indice)
			}
		}
	}
}

// TestColleAPI: the proposals never hold a secret's value; saving sends the text back, the server
// analyses it again and stores what was kept (edited name and note, scope downgraded without a password).
func TestColleAPI(t *testing.T) {
	s := serveurCoffreTest(t)
	mux := http.NewServeMux()
	s.routes(mux)
	temoin := "TEMOIN" + aleatoire(16)
	texte := "export CLOUDFLARE_API_TOKEN=" + temoin + "\nANTHROPIC_API_KEY=sk-ant-" + temoin + "\nNODE_ENV=production\n"
	var tout strings.Builder
	appel := func(corps any) (int, map[string]any) {
		b, _ := json.Marshal(corps)
		r := httptest.NewRequest("POST", "/api/coffre/coller", strings.NewReader(string(b)))
		r.Header.Set("X-Athanor", "jeton")
		r.AddCookie(&http.Cookie{Name: cookieAcces, Value: "cle"})
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		tout.WriteString(w.Body.String())
		var v map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	code, v := appel(map[string]any{"texte": texte})
	props, _ := v["propositions"].([]any)
	if code != 200 || len(props) != 3 {
		t.Fatalf("proposals: %d %v", code, v)
	}
	if p := props[0].(map[string]any); p["type"] != "cloudflare" || p["portee"] != "code" { // no password: no infra
		t.Errorf("Cloudflare without a password: %v", p)
	}
	code, v = appel(map[string]any{"texte": texte, "enregistrer": []map[string]any{
		{"i": 1, "nom": "CLE_IA", "note": "pour les tests"}, {"i": 2}}})
	l, _ := v["entrees"].([]any)
	if code != 200 || len(l) != 2 {
		t.Fatalf("save: %d %v", code, v)
	}
	if e := l[0].(map[string]any); e["nom"] != "CLE_IA" || e["type"] != "anthropic" || e["note"] != "pour les tests" || e["valeur"] != "" {
		t.Errorf("saved secret: %v", e)
	}
	ents := s.coffre.Liste()
	if len(ents) != 2 {
		t.Fatalf("%d entries", len(ents))
	}
	if got, _ := s.coffre.Valeur(ents[0].ID); got != "sk-ant-"+temoin {
		t.Error("the saved value is not the pasted one")
	}
	if ents[1].Valeur != "production" || ents[1].Famille != "variable" {
		t.Errorf("variable: %+v", ents[1])
	}
	for _, corps := range []any{map[string]any{"texte": texte, "enregistrer": []map[string]any{{"i": 9}}}, `{"texte":`,
		map[string]any{"texte": texte, "enregistrer": []map[string]any{{"i": 2}, {"i": 1, "type": "inconnu"}}}} {
		if code, _ := appel(corps); code != http.StatusBadRequest {
			t.Errorf("bad request %v: %d", corps, code)
		}
	}
	if n := len(s.coffre.Liste()); n != 2 { // a bad choice is refused before any entry is added
		t.Errorf("%d entries after the refused saves, want 2", n)
	}
	if strings.Contains(tout.String(), temoin) {
		t.Error("a response holds the witness value")
	}
}
