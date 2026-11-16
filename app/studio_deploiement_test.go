package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStudioSansDeploiement (M273): the Studio only develops. The kit's instructions plan no deployment milestone
// and ask for no server's access; a task for the user or a key asking for one is refused; the request a mission
// writes says its Servers lane deploys; the orchestrator never puts a deployment in the Dev brief.
func TestStudioSansDeploiement(t *testing.T) {
	lire := func(f string) string {
		b, err := os.ReadFile(filepath.Join("..", "template", f))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	kit, cadrage, pourVous := lire("CLAUDE.md"), lire(filepath.Join("guides", "FRAMING.md")), lire(filepath.Join("guides", "FOR_YOU.md"))
	if strings.Contains(kit, "deploy as the request and the plan say") {
		t.Error("the kit still tells the pilot to deploy")
	}
	for _, x := range []struct{ texte, attendu string }{
		{kit, "**No deployment here.**"}, {kit, "never a server's access"}, {kit, "the mission's Servers lane deploys it"},
		{cadrage, "**No deployment milestone**"}, {cadrage, "ready to deploy"}, {pourVous, "Never a server's access"},
	} {
		if !strings.Contains(x.texte, x.attendu) {
			t.Errorf("missing %q", x.attendu)
		}
	}

	// the tasks for the user: a server's access refused (title, details, --need or --secret), the others kept
	p := t.TempDir()
	if err := os.MkdirAll(filepath.Join(p, ".athanor"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROJECT", p)
	refus := []struct {
		args   []string
		entree string
	}{
		{[]string{"add", "Déployer le site sur le serveur de production"}, "Par deploy/deploy.sh : donnez utilisateur@hôte SSH et le port."},
		{[]string{"add", "Donner l'accès au serveur", "--secret", "SSH_PRIVATE_KEY"}, "Collez le contenu de la clé privée SSH."},
		{[]string{"add", "Accès au VPS", "--need", "user@host and the port"}, ""},
		{[]string{"add", "Serveur", "--secret", "DEPLOY_HOST"}, ""},
		{[]string{"add", "Ouvrir le serveur"}, "deploy@203.0.113.7, port 2222"},
	}
	for _, r := range refus {
		_, se, code := lancerAvecEntree(t, []byte(r.entree), func() int { return lancerTodo(r.args) })
		if code == 0 || !strings.Contains(se, "Servers lane") {
			t.Errorf("todo %q not refused: %d %q", r.args, code, se)
		}
	}
	so, se, code := lancerAvecEntree(t, []byte("1. Créez un compte de test.\n"), func() int {
		return lancerTodo([]string{"add", "Créer un compte Stripe de test", "--secret", "STRIPE_KEY"})
	})
	if code != 0 || !strings.Contains(so, "T1 created") {
		t.Fatalf("an ordinary task refused: %d %q %q", code, so, se)
	}
	if _, se, code := lancerAvecEntree(t, []byte("Et la clé privée du serveur."), func() int { return lancerTodo([]string{"edit", "T1"}) }); code == 0 || !strings.Contains(se, "Servers lane") {
		t.Errorf("todo edit not refused: %d %q", code, se)
	}
	for _, n := range []string{"SHOPIFY_TOKEN", "API_KEY", "SMTP_PASSWORD", "DATABASE_URL"} {
		if demandeAccesServeur([]string{n}, "Envoyer les courriels du formulaire de contact") {
			t.Errorf("%s taken for a server's access", n)
		}
	}

	// demander_acces: a server's key refused before anything is recorded (s is not reached)
	for _, a := range []string{`{"nom":"SSH_KEY","raison":"deploy the site"}`, `{"nom":"SERVER_PASSWORD","raison":"x"}`, `{"nom":"TOKEN","raison":"to log in over SSH"}`} {
		if _, err := outilDemanderAcces(nil, SessionHub{}, json.RawMessage(a)); err == nil || !strings.Contains(err.Error(), "Servers lane") {
			t.Errorf("demander_acces %s: %v", a, err)
		}
	}

	// the mission's request: the deployment note only when the card has an environment
	c := CarteMission{Dev: BriefCarte{Brief: "Un site vitrine pour la boulangerie, déployé sur dev.martin.fr."}}
	if demandeStudio(c) != c.Dev.Brief {
		t.Error("a note without an environment")
	}
	c.Envs = []EnvCarte{{Nom: "site", URL: "https://dev.martin.fr", Serveurs: []string{"s1"}}}
	if d := demandeStudio(c); !strings.HasPrefix(d, c.Dev.Brief) || !strings.Contains(d, "no deployment milestone, no request for a server's access") {
		t.Errorf("request: %q", d)
	}
	if o := consigneOrchestrateur("un site", nil); !strings.Contains(o, "the Studio only develops: never ask it to deploy") {
		t.Error("the orchestrator may put a deployment in the Dev brief")
	}
}
