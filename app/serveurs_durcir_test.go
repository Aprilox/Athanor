package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestPlanDurcissement (M138): only the steps not in place, in their order; the firewall step is destructive
// (closes every other port) and comes after ufw's installation when ufw is missing; tout_refuser takes no port.
func TestPlanDurcissement(t *testing.T) {
	s := &Serveur{}
	etat := map[string]bool{"utilisateur": true, "cles": false, "root": true, "maj": false, "heure": true, "pare_feu": false, "docker": true, "ufw": false, "caddy": false}
	l, err := s.planDurcissement(ServeurInventaire{ID: "srv"}, etat)
	if err != nil {
		t.Fatal(err)
	}
	cles := []string{}
	for _, e := range l {
		cles = append(cles, e.Durcir+":"+e.Outil+":"+e.Classe)
	}
	if got := strings.Join(cles, " "); got != "cles:fichier_ecrire:destructrice maj:ssh_commande:changement ufw:ssh_commande:changement pare_feu:pare_feu:destructrice caddy:ssh_commande:changement" {
		t.Errorf("plan: %s", got)
	}
	if l[3].SansSauvegarde == "" || string(l[3].Arguments) != `{"action":"tout_refuser"}` || classeHaute(l) != "destructrice" {
		t.Errorf("firewall step: %+v", l[3])
	}
	tout := map[string]bool{}
	for _, k := range append(etapesDurcir, "ufw") {
		tout[k] = true
	}
	if l, _ := s.planDurcissement(ServeurInventaire{}, tout); len(l) != 0 {
		t.Errorf("everything in place, plan %+v", l)
	}
	if _, err := specPareFeu(ServeurInventaire{Port: 22}, argsOutilServeur{Action: "tout_refuser", Port: 8080}); err == nil {
		t.Error("tout_refuser with a port accepted")
	}
	if c, _ := classerOutil("pare_feu", argsOutilServeur{Action: "tout_refuser"}); c != ClasseDestructrice {
		t.Errorf("tout_refuser class %v", c)
	}
}

// TestDurcirConteneur (M138): a new server reached as root gets the plan of the missing steps; once the owner
// accepts it, Athanor connects as the new user with sudo, root and passwords are refused, ufw denies all but
// SSH, 80 and 443; asked again, everything is already in place. C73: while only Athanor's key lets root in, the
// plan says so and is not accepted without the ticked box; the owner's key in root's file gets in as athanor
// afterwards. B78: two clicks at once make one plan.
func TestDurcirConteneur(t *testing.T) {
	s, appelAPI, id, _, appel := serveurConteneurTest(t)
	ctx := context.Background()
	srv, _ := serveurProd(id)
	// a "new" server: only Athanor's key lets root in (other tests leave keys at athanor's), Athanor connects as root
	pub, err := s.clePubliqueServeur(srv)
	if err != nil {
		t.Fatal(err)
	}
	appel("ssh_commande", map[string]any{"commande": "sudo -n install -d -m 700 /root/.ssh && echo '" + pub + "' | sudo -n tee /root/.ssh/authorized_keys >/dev/null && sudo -n rm -f /etc/apt/apt.conf.d/20auto-upgrades"})
	if err := s.changerUtilisateurServeur(id, "root", nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // the container as the next tests expect it: passwords, root's key gone, ufw off
		_ = s.changerUtilisateurServeur(id, "athanor", nil)
		appel("ssh_commande", map[string]any{"commande": "sudo -n rm -f " + fichierClesDurci + " " + fichierRootDurci + " /etc/sudoers.d/90-athanor-athanor /root/.ssh/authorized_keys; sudo -n sed -i '/temoin-proprio/d' ~athanor/.ssh/authorized_keys; sudo -n sed -i '/^# BEGIN ATHANOR DOCKER-USER/,/^# END ATHANOR DOCKER-USER/d' /etc/ufw/after.rules; " +
			"sudo -n ufw --force disable >/dev/null; for p in 22 80 443; do sudo -n ufw --force delete allow $p/tcp >/dev/null; done; sudo -n kill -HUP \"$(cat /run/sshd.pid)\""})
	})
	if r := appel("ssh_commande", map[string]any{"commande": "id -un"}); strings.TrimSpace(r.Sortie) != "root" {
		t.Fatalf("not root: %+v", r)
	}

	plans := make([]string, 2)
	var wg sync.WaitGroup
	for i := range plans {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			s.apiServeurDurcir(w, httptest.NewRequest("POST", "/api/serveurs/"+id, nil), id)
			var v struct{ Plan string }
			_ = json.Unmarshal(w.Body.Bytes(), &v)
			plans[i] = v.Plan
		}()
	}
	wg.Wait()
	if plans[0] == "" || plans[0] != plans[1] {
		t.Fatalf("two clicks, two plans: %v", plans)
	}
	if ap, _ := s.approbations.Lire(plans[0]); ap.Details["seule_cle"] != "oui" {
		t.Errorf("only Athanor's key, not said: %+v", ap.Details)
	}
	if p, _ := s.lirePlan(plans[0]); p.SeuleCle == "" {
		t.Error("Athanor's public key not on the plan")
	}
	if code, v := appelAPI("/api/hub/approbations", map[string]any{"id": plans[0], "decision": "accepter"}); code != 409 || !strings.Contains(fmt.Sprint(v), "tick the box") {
		t.Errorf("accepted without the box: %d %v", code, v)
	}
	if _, _, err := s.deciderApprobation("test", plans[0], "refuser", "", "", ""); err != nil {
		t.Fatal(err)
	}
	// the owner's key (a witness) lets root in: copied to athanor, nothing to tick
	temoin, temoinPriv := nouvelleCleSSH(t)
	pubTemoin := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(temoin.PublicKey())))
	if r := appel("ssh_commande", map[string]any{"commande": "printf '%s temoin-proprio\n' '" + pubTemoin + "' >> /root/.ssh/authorized_keys"}); r.Code != 0 {
		t.Fatalf("witness key: %+v", r)
	}

	code, v := appelAPI("/api/serveurs/"+id, map[string]any{"durcir": true})
	if code != 200 || v["plan"] == nil {
		t.Fatalf("durcir: %d %v", code, v)
	}
	plan := v["plan"].(string)
	b, _ := json.Marshal(v["etapes"])
	if string(b) != `["utilisateur","cles","root","maj","heure","pare_feu","docker"]` { // ufw and Caddy are already in the image
		t.Errorf("steps: %s", b)
	}
	if ap, ok := s.approbations.Lire(plan); !ok || ap.Etat != "attente" || ap.Agent != agentDurcir || ap.Details["classe"] != "destructrice" || ap.Details["seule_cle"] != "" {
		t.Errorf("approval: %+v", ap)
	}
	if _, _, err := s.deciderApprobation("test", plan, "accepter", "", "", ""); err != nil {
		t.Fatal(err)
	}
	journal := ""
	for fin := time.Now().Add(8 * time.Minute); time.Now().Before(fin); time.Sleep(2 * time.Second) {
		if journal = journalAuditTexte(t); strings.Contains(journal, "plan "+plan+" appliqué") || strings.Contains(journal, "en échec, arrêt") {
			break
		}
	}
	if !strings.Contains(journal, "plan "+plan+" appliqué : tout est en place") {
		t.Fatalf("plan not applied: %s", couper(journal, 4000))
	}
	p, _ := s.lirePlan(plan)
	for _, e := range p.Etapes {
		if e.Etat != "ok" || e.Executee == "" {
			t.Errorf("step %s: %+v", e.Durcir, e)
		}
	}
	if len(annulationsDe(id)) != 0 {
		t.Errorf("changes left unconfirmed: %+v", annulationsDe(id))
	}

	// Athanor's connection holds: the new user, with sudo
	srv2, _ := serveurProd(id)
	if srv2.Utilisateur != "athanor" || !srv2.MotDePasseCoupe {
		t.Errorf("inventory: %+v", srv2)
	}
	if r := appel("ssh_commande", map[string]any{"commande": "id -un; sudo -n true && echo sudo-ok; sudo -n ufw status verbose"}); !strings.Contains(r.Sortie, "athanor\nsudo-ok") ||
		!strings.Contains(r.Sortie, "Status: active") || !strings.Contains(r.Sortie, "deny (incoming)") ||
		!strings.Contains(r.Sortie, "22/tcp") || !strings.Contains(r.Sortie, "80/tcp") || !strings.Contains(r.Sortie, "443/tcp") {
		t.Errorf("after hardening: %q", r.Sortie)
	}
	// C70: ufw fills DOCKER-USER, new connections from outside into Docker's bridges refused
	if r := appel("ssh_commande", map[string]any{"commande": "sudo -n iptables -S DOCKER-USER"}); !strings.Contains(r.Sortie, "-A DOCKER-USER -o docker+ -j DROP") ||
		!strings.Contains(r.Sortie, "-A DOCKER-USER -i docker+ -j RETURN") {
		t.Errorf("DOCKER-USER: %q", r.Sortie)
	}
	if c, err := connecterSSH(ctx, AccesSSH{Hote: srv.Hote, Port: srv.Port, Utilisateur: "athanor", Cle: temoinPriv, Empreinte: srv.EmpreinteHote}); err != nil {
		t.Errorf("the owner's key no longer gets in: %v", err)
	} else {
		c.Close()
	}
	// root and passwords are refused now
	cle, _ := s.coffre.Valeur(srv.Cle)
	if c, err := connecterSSH(ctx, AccesSSH{Hote: srv.Hote, Port: srv.Port, Utilisateur: "root", Cle: []byte(cle), Empreinte: srv.EmpreinteHote}); err == nil {
		c.Close()
		t.Error("root still gets in")
	}
	if c, err := connecterSSH(ctx, AccesSSH{Hote: srv.Hote, Port: srv.Port, Utilisateur: "athanor", MotDePasse: motDePasseConteneurSSH, Empreinte: srv.EmpreinteHote}); err == nil {
		c.Close()
		t.Error("a password still gets in")
	}

	// asked again: already in place, no new plan
	if code, v := appelAPI("/api/serveurs/"+id, map[string]any{"durcir": true}); code != 200 || v["deja"] != true {
		t.Errorf("second time: %d %v", code, v)
	}
}
