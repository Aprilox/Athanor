package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestServeursAnnulationConteneur (M93): against the test container's sshd, a firewall rule kept by the owner
// stays; a firewall rule and an sshd file left unconfirmed are put back by the server itself, sshd reloaded.
func TestServeursAnnulationConteneur(t *testing.T) {
	s, appelAPI, id, _, appel := serveurConteneurTest(t)
	ctx := context.Background()
	avant := delaiAnnulation
	delaiAnnulation = 10 * time.Second
	t.Cleanup(func() { delaiAnnulation = avant })
	margeAvant := margeVerifRetour
	margeVerifRetour = 2 * time.Second
	t.Cleanup(func() { margeVerifRetour = margeAvant })
	const fichier = "/etc/ssh/athanor-m93.conf"
	appel("ssh_commande", map[string]any{"commande": "printf 'v1\\n' | sudo -n tee " + fichier + " >/dev/null"})
	t.Cleanup(func() {
		appel("ssh_commande", map[string]any{"commande": "sudo -n ufw --force delete allow 8282/tcp; sudo -n rm -f " + fichier + "*; sudo -n rm -rf /var/backups/athanor"})
	})

	// kept from the server page (Keep this change)
	r := appel("pare_feu", map[string]any{"action": "allow", "port": 8282, "proto": "tcp"})
	if r.Annulation == "" || !strings.Contains(r.Sortie, "A new connection to the server works") {
		t.Fatalf("allow without its rollback: %+v", r)
	}
	if l := annulationsDe(id); len(l) != 1 || l[0].ID != r.Annulation || l[0].Quoi != "pare_feu allow 8282 tcp" {
		t.Errorf("pending changes: %+v", l)
	}
	if code, v := appelAPI("/api/serveurs/"+id, map[string]any{"garder": r.Annulation}); code != 200 {
		t.Fatalf("keep: %d %v", code, v)
	}
	if _, err := s.confirmerChangement(ctx, "test", id, r.Annulation); err == nil || !strings.Contains(err.Error(), "already undone") {
		t.Errorf("kept twice: %v", err)
	}

	// left unconfirmed: a firewall rule and an sshd file
	if r := appel("pare_feu", map[string]any{"action": "allow", "port": 8181, "proto": "tcp"}); r.Annulation == "" {
		t.Fatalf("allow 8181: %+v", r)
	}
	r = appel("fichier_ecrire", map[string]any{"chemin": fichier, "contenu": "v2\n", "sudo": true})
	if r.Annulation == "" {
		t.Fatalf("sshd file written without its rollback: %+v", r)
	}
	if len(annulationsDe(id)) != 2 {
		t.Errorf("pending changes: %+v", annulationsDe(id))
	}
	etat := ""
	for fin := time.Now().Add(30 * time.Second); time.Now().Before(fin); time.Sleep(time.Second) {
		etat = appel("ssh_commande", map[string]any{"commande": "sudo -n ufw show added; sudo -n cat " + fichier + " /var/backups/athanor/annul-" + r.Annulation + ".log"}).Sortie
		if !strings.Contains(etat, "8181") && strings.Contains(etat, "rolled back") {
			break
		}
	}
	if strings.Contains(etat, "8181") || !strings.Contains(etat, "ufw allow 8282/tcp") || !strings.Contains(etat, "v1\n") || !strings.Contains(etat, "sshd reloaded") {
		t.Errorf("after the delay: %q", etat)
	}
	if r := appel("service", map[string]any{"action": "status", "nom": "ssh"}); !strings.Contains(r.Sortie, "sshd is running") {
		t.Errorf("sshd after its reload: %+v", r)
	}
	time.Sleep(500 * time.Millisecond) // Athanor's own timer, a little after the server's
	if l := annulationsDe(id); len(l) != 0 {
		t.Errorf("still pending: %+v", l)
	}
	journal := attendreAudit(t, "non confirmé : remis par le serveur (vérifié)", 2)
	for _, m := range []string{"retour arrière programmé", "gardé"} {
		if !strings.Contains(journal, m) {
			t.Errorf("audit log without %q", m)
		}
	}

	// C48: a broken sshd_config is refused by sshd -t and put back at once
	const lire = "sudo -n cat /etc/ssh/sshd_config"
	orig := appel("ssh_commande", map[string]any{"commande": lire}).Sortie
	if !strings.Contains(orig, "Include") {
		t.Fatalf("sshd_config: %q", orig)
	}
	r = appel("fichier_ecrire", map[string]any{"chemin": "/etc/ssh/sshd_config", "contenu": "Port pas-un-port\n" + orig, "sudo": true})
	if r.Code == 0 || !strings.Contains(r.Sortie, "sshd -t refused the new configuration") || !strings.Contains(r.Sortie, "state before was put back") {
		t.Errorf("broken sshd_config: %+v", r)
	}
	if got := appel("ssh_commande", map[string]any{"commande": lire}).Sortie; strings.Contains(got, "pas-un-port") {
		t.Errorf("broken sshd_config left in place: %q", got)
	}

	// C48: a valid config that locks Athanor out is reloaded at once, the new connection fails, the server puts it back
	r = appel("fichier_ecrire", map[string]any{"chemin": "/etc/ssh/sshd_config", "contenu": "DenyUsers *\n" + orig, "sudo": true})
	if r.Annulation == "" || !strings.Contains(r.Sortie, "sshd -t ok, sshd reloaded") || !strings.Contains(r.Sortie, "could not open a new connection") {
		t.Errorf("locking sshd_config: %+v", r)
	}
	var got string
	var err error
	for fin := time.Now().Add(40 * time.Second); time.Now().Before(fin); time.Sleep(2 * time.Second) {
		var x ResultatOutil
		if x, err = s.appelerOutilServeur(ctx, "test", id, "ssh_commande", []byte(`{"commande":"`+lire+`"}`), Sauvegarde{}); err == nil {
			if got = x.Sortie; !strings.Contains(got, "DenyUsers") {
				break
			}
		}
	}
	if err != nil || strings.Contains(got, "DenyUsers") || !strings.Contains(got, "Include") {
		t.Errorf("after the delay, connection %v, sshd_config %q", err, got)
	}
	attendreAudit(t, "non confirmé : remis par le serveur (vérifié)", 3)

	// C49: a rollback whose ufw reload fails is not announced as done: the audit log says so and the bell rings
	appel("ssh_commande", map[string]any{"commande": `sudo -n mv /usr/sbin/ufw /usr/sbin/ufw.vrai && printf '#!/bin/sh
case "$1" in status) echo "Status: active";; reload) echo "faux ufw: reload refused"; exit 1;; *) exec /usr/sbin/ufw.vrai "$@";; esac
' | sudo -n tee /usr/sbin/ufw >/dev/null && sudo -n chmod 755 /usr/sbin/ufw`})
	t.Cleanup(func() {
		appel("ssh_commande", map[string]any{"commande": "sudo -n mv -f /usr/sbin/ufw.vrai /usr/sbin/ufw"})
	})
	r = appel("pare_feu", map[string]any{"action": "allow", "port": 8484, "proto": "tcp"})
	if r.Annulation == "" {
		t.Fatalf("allow 8484: %+v", r)
	}
	journal = attendreAudit(t, "non confirmé : retour arrière non vérifié : the rollback failed: faux ufw: reload refused", 1)
	if !strings.Contains(journal, "ROLLBACK FAILED") {
		t.Errorf("audit log without the server's log: %s", journal)
	}
	var alerte *ElementHub
	for _, e := range (moduleServeurs{}).Elements(s) {
		if e.ID == id+":retour:"+r.Annulation {
			alerte = &e
		}
	}
	if alerte == nil || alerte.Attente == nil || len(alerte.Attente.Args) != 4 || alerte.Attente.Args[0] != "pare_feu allow 8484 tcp" ||
		!strings.Contains(alerte.Attente.Args[3].(string), "annul-"+r.Annulation+".log") {
		t.Errorf("the bell for the failed rollback: %+v", alerte)
	}
}

// attendreAudit: the audit log once it holds texte n times (the rollbacks are checked in the background).
func attendreAudit(t *testing.T, texte string, n int) string {
	t.Helper()
	j := ""
	for fin := time.Now().Add(30 * time.Second); time.Now().Before(fin); time.Sleep(500 * time.Millisecond) {
		if j = journalAuditTexte(t); strings.Count(j, texte) >= n {
			return j
		}
	}
	t.Errorf("audit log without %d × %q: %s", n, texte, couper(j, 3000))
	return j
}

// TestGenreAnnulationRecharge (C48): a reload or restart of sshd, by a command or the service tool, gets the SSH rollback.
func TestGenreAnnulationRecharge(t *testing.T) {
	ctx := context.Background()
	for cmd, want := range map[string]bool{
		"sudo systemctl reload ssh": true, "systemctl restart sshd.service": true, "sudo -n systemctl --no-block restart ssh && echo ok": true,
		"service ssh restart": true, "sudo service sshd reload": true, "systemctl restart nginx": false, "systemctl status ssh": false,
		"systemctl reload ssh-agent": false, "echo systemctl": false,
	} {
		if g, p := genreAnnulation(ctx, nil, ServeurInventaire{}, "ssh_commande", argsOutilServeur{Commande: cmd}); (g == "ssh") != want || want && p != "/etc/ssh/sshd_config" {
			t.Errorf("%q: %q %q, want ssh %v", cmd, g, p, want)
		}
	}
	if g, _ := genreAnnulation(ctx, nil, ServeurInventaire{}, "service", argsOutilServeur{Action: "restart", Nom: "ssh"}); g != "ssh" {
		t.Errorf("service restart ssh: %q", g)
	}
	if g, _ := genreAnnulation(ctx, nil, ServeurInventaire{}, "service", argsOutilServeur{Action: "status", Nom: "ssh"}); g != "" {
		t.Errorf("service status ssh: %q", g)
	}
}
