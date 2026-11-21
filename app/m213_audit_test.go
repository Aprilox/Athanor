package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAuditFinRetiree (B49): the last lines cut together with the end mark are still reported; a log
// still empty without its mark stays intact.
func TestAuditFinRetiree(t *testing.T) {
	d := t.TempDir()
	a := Audit{Dossier: d}
	if v, err := a.Verifier(); err != nil || !v.OK {
		t.Fatalf("empty log: %+v %v", v, err)
	}
	for i := range 5 {
		if err := a.Ecrire(LigneAudit{Type: "evenement", Action: "test", Resultat: fmt.Sprint("ligne ", i+1)}); err != nil {
			t.Fatal(err)
		}
	}
	nom := filepath.Join(d, time.Now().UTC().Format("2006-01")+".jsonl")
	b, _ := os.ReadFile(nom)
	l := bytes.Split(bytes.TrimRight(b, "\n"), []byte("\n"))
	if err := os.WriteFile(nom, append(bytes.Join(l[:3], []byte("\n")), '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(d, finAudit)); err != nil {
		t.Fatal(err)
	}
	if v, err := a.Verifier(); err != nil || v.OK || v.Ligne != 4 || v.Raison != "retiree" {
		t.Fatalf("got %+v %v, want line 4 retiree", v, err)
	}
}
