package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTachesPurgeEtCache: a server keeps its newest finished tasks (a running one is never removed), a task file is
// decoded again only once it changed, and a task older than the 50 shown is still found by its id.
func TestTachesPurgeEtCache(t *testing.T) {
	_ = serveurCoffreTest(t)
	const id = "zz99"
	d := dossierTaches(id)
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(d)) })
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	debut := time.Now().Add(-time.Hour)
	ecrire := func(n int, texte string) string {
		tid := fmt.Sprintf("t%011d", n)
		b, _ := json.Marshal(TacheServeurs{ID: tid, Texte: texte, Serveurs: []string{id}, Etat: "finie", Cree: debut.Add(time.Duration(n) * time.Second).UTC().Format(formatCree), Fin: "x"})
		f := filepath.Join(d, tid+".json")
		if err := os.WriteFile(f, b, 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chtimes(f, debut.Add(time.Duration(n)*time.Second), debut.Add(time.Duration(n)*time.Second))
		return tid
	}
	for n := range maxTachesGardees + 5 {
		ecrire(n, "tâche")
	}
	enCours := fmt.Sprintf("t%011d", 1) // among the oldest, but running
	tachesEnCours.Lock()
	tachesEnCours.m[enCours] = &tacheEnCours{t: TacheServeurs{ID: enCours, Etat: "application"}}
	tachesEnCours.Unlock()
	t.Cleanup(func() { tachesEnCours.Lock(); delete(tachesEnCours.m, enCours); tachesEnCours.Unlock() })
	if x := tacheParID(id, fmt.Sprintf("t%011d", 3)); x.ID == "" {
		t.Error("a task older than the 50 shown is not found by its id")
	}
	purgerTaches(id)
	l, _ := filepath.Glob(filepath.Join(d, "*.json"))
	if len(l) != maxTachesGardees+1 { // the 100 newest, and the running one
		t.Errorf("%d task files kept", len(l))
	}
	if _, err := os.Stat(filepath.Join(d, enCours+".json")); err != nil {
		t.Error("a running task was removed")
	}
	if _, err := os.Stat(filepath.Join(d, fmt.Sprintf("t%011d", 0)+".json")); err == nil {
		t.Error("the oldest finished task was kept")
	}
	// the cache: a changed file is read again
	tid := ecrire(500, "avant")
	if x := tacheParID(id, tid); x.Texte != "avant" {
		t.Fatalf("read: %+v", x)
	}
	ecrire(500, "après, plus long")
	if x := tacheParID(id, tid); x.Texte != "après, plus long" {
		t.Errorf("a changed file kept from the cache: %+v", x)
	}
}
