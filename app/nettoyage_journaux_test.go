package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNettoyerJournaux(t *testing.T) {
	kit := t.TempDir()
	projet := filepath.Join(kit, "mon projet")
	vieux := time.Now().Add(-ageJournauxOublies - time.Hour)
	faire := func(rel string, quand time.Time) string {
		d := filepath.Join(kit, "logs", rel)
		_ = os.MkdirAll(d, 0o755)
		_ = os.WriteFile(filepath.Join(d, "pilot.log"), []byte("x"), 0o644)
		_ = os.Chtimes(filepath.Join(d, "pilot.log"), quand, quand)
		_ = os.Chtimes(d, quand, quand)
		return d
	}
	liste := faire(filepath.Join("pilotes", filepath.Base(priveDePilote(kit, projet, estWindows))), vieux)
	oublie := faire(filepath.Join("pilotes", "0123456789abcdef"), vieux)
	actif := faire(filepath.Join("pilotes", "fedcba9876543210"), time.Now())
	tunnelVieux := faire(filepath.Join("equipe-directe", "tunnel-1"), vieux)
	tunnelActif := faire(filepath.Join("equipe-directe", "tunnel-2"), time.Now())
	equipe := filepath.Join(kit, "logs", "equipe-directe", "0011.json")
	_ = os.WriteFile(equipe, []byte("{}"), 0o644)
	_ = os.Chtimes(equipe, vieux, vieux)

	nettoyerJournaux(kit, []Projet{{Dossier: projet}})

	for d, reste := range map[string]bool{liste: true, oublie: false, actif: true, tunnelVieux: false, tunnelActif: true, equipe: true} {
		if _, err := os.Stat(d); (err == nil) != reste {
			t.Errorf("%s: kept %v, want %v", d, err == nil, reste)
		}
	}
}
