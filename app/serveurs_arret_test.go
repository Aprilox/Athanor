package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestServeursArretOperations (C33): an operation in progress stops as soon as its session token is revoked
// (by its hash: the end of a task, the owner) or at the emergency stop, without waiting for its delay.
func TestServeursArretOperations(t *testing.T) {
	ancien := modeNatif.Load()
	modeNatif.Store(false)
	t.Cleanup(func() { modeNatif.Store(ancien) })
	signer, prive := nouvelleCleSSH(t)
	mem := lancerSSHMemoire(t, signer.PublicKey(), "", func(cmd string, _ []byte) (string, int) {
		time.Sleep(20 * time.Second) // a long command: only the cancellation ends the call early
		return "fini", 0
	})
	s, srv := outilsServeursTest(t, mem.hote, mem.port, mem.empreinte, prive)
	session := func(projet string) SessionHub { // one project each: a new token of the same pair revokes the old
		t.Helper()
		j, err := s.jetons.CreerSession(SessionHub{Projet: projet, Agent: agentServeurs, Outils: outilsServeursMCP, Serveurs: []string{srv.ID}})
		if err != nil {
			t.Fatal(err)
		}
		x, ok := s.jetons.Verifier(j)
		if !ok {
			t.Fatal("token not valid")
		}
		return x
	}
	args, _ := json.Marshal(map[string]any{"serveur": srv.ID, "commande": "uname -a", "delai_s": 60})
	coupe := func(nom string, x SessionHub, arreter func()) {
		t.Helper()
		fini := make(chan error, 1)
		debut := time.Now()
		go func() { _, err := faireOutilServeur("ssh_commande")(s, x, args); fini <- err }()
		select {
		case <-mem.commandes: // the command runs on the server
		case err := <-fini:
			t.Fatalf("%s: ended before running: %v", nom, err)
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the command never reached the server", nom)
		}
		arreter()
		select {
		case err := <-fini:
			if err == nil || !strings.Contains(err.Error(), "cancelled") {
				t.Errorf("%s: %v", nom, err)
			}
			if d := time.Since(debut); d > 5*time.Second {
				t.Errorf("%s: stopped after %s", nom, d)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the operation kept running", nom)
		}
	}

	// the token revoked by its hash (what a task's end and the owner do); another session keeps running
	x, autre := session("demo"), session("autre")
	coupe("revocation", x, func() { s.revoquerJetons("fin de tâche", func(y SessionHub) bool { return y.Hache == x.Hache }) })
	if s.contexteSession(autre.Hache).Err() != nil {
		t.Error("another session was cancelled too")
	}
	// a token already revoked gets a cancelled context: nothing starts
	if s.contexteSession(x.Hache).Err() == nil {
		t.Error("a revoked session got a live context")
	}
	// the emergency stop cuts every session's operation (the vault closes last: nothing runs after it)
	coupe("emergency stop", autre, func() { s.arretUrgence("test") })
	s.opsMu.Lock()
	defer s.opsMu.Unlock()
	if len(s.ops) != 0 {
		t.Errorf("%d cancelled sessions kept", len(s.ops))
	}
}

// commandeBornee wraps the agent's command whole, never interpreted on the way (C34).
func TestCommandeBornee(t *testing.T) {
	c := commandeBornee("sleep 1; echo 'a b'", 2500*time.Millisecond)
	if !strings.HasSuffix(c, " "+citerShell("sleep 1; echo 'a b'")+" '3'") || !strings.Contains(c, `exec timeout -k 5 "$2"`) {
		t.Errorf("wrapped command: %s", c)
	}
}
