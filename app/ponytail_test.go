package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every sub-agent gets Ponytail: its code rules, or its lean work for those that plan, explore or judge.
func TestPonytailAgents(t *testing.T) {
	t.Setenv("PONYTAIL", "")
	js := `{"developer":{"description":"d","prompt":"Build <it>."},"critic":{"description":"c","prompt":"Judge."},"scout":{"description":"s","prompt":"Look."},"architect":{"description":"a","prompt":"Plan."},"lecteur":{"description":"l","prompt":"Read.","disallowedTools":["Write","Edit","NotebookEdit"]},"modeleur":{"description":"m","prompt":"Model.","model":"opus"}}`
	var v map[string]map[string]any
	if err := json.Unmarshal([]byte(avecPonytailAgents(js)), &v); err != nil {
		t.Fatal(err)
	}
	// every sub-agent gets Ponytail: the code rules, or its lean work for those that plan, explore or judge
	for nom, code := range map[string]bool{"developer": true, "modeleur": true, "critic": false, "scout": false, "architect": false, "lecteur": false} {
		p, _ := v[nom]["prompt"].(string)
		if strings.Contains(p, "## Ponytail: the smallest complete change") != code || strings.Contains(p, "## Ponytail: lean work") == code {
			t.Errorf("%s: %q", nom, p)
		}
	}
	if !strings.HasPrefix(v["developer"]["prompt"].(string), "Build <it>.") || v["modeleur"]["model"] != "opus" {
		t.Fatalf("the rest of a definition kept: %v", v)
	}
	t.Setenv("PONYTAIL", "0")
	if avecPonytailAgents(js) != js {
		t.Fatal("PONYTAIL=0: the team unchanged")
	}
	// what Claude reads: the scope guard first, the rules word for word
	for _, s := range []string{"never **how much**", "Never cut:", "The best code is the code never written.", versionPonytail} {
		if !strings.Contains(consignesPonytail, s) {
			t.Errorf("consignesPonytail without %q", s)
		}
	}
	for _, s := range []string{"The scope is the request's", "Never cut:", "Answer short", versionPonytail} {
		if !strings.Contains(consignesPonytailLecture, s) {
			t.Errorf("consignesPonytailLecture without %q", s)
		}
	}
}

// The Overview's savings: the cache at list price, RTK's measures, with and without side by side.
func TestCalculerGains(t *testing.T) {
	var stats []LigneStat
	for i := 0; i < 3; i++ {
		stats = append(stats, LigneStat{Cycle: "x", Etat: "ok", Commit: "a", CoutUSD: 2, SortieTokens: 1000})
		stats = append(stats, LigneStat{Cycle: "y", Etat: "ok", Commit: "b", CoutUSD: 1, SortieTokens: 500, Options: "ponytail,rtk", RTKEntree: 1000, RTKEconomise: 700})
	}
	stats = append(stats, LigneStat{Cycle: "1", Etat: "ok", Commit: "c", CoutUSD: 50, Options: "cadrage"}, LigneStat{Cycle: "4·M2", Etat: "ok", Commit: "d", CoutUSD: 50, Options: "ponytail"},
		LigneStat{Cycle: "5", Etat: "erreur", CoutUSD: 50, Options: "ponytail"})
	stats[0].EntreeTokens, stats[0].CacheEcrit, stats[0].CacheLu = 100, 100, 800
	g := calculerGains(stats, true)
	// paid 100 + 125 + 80 = 305 instead of 1000
	if g.CachePct != 70 || g.RTKEntree != 3000 || g.RTKEconomise != 2100 || g.RTKPct != 70 || !g.Ponytail || g.VersionPonytail != versionPonytail {
		t.Fatalf("%+v", g)
	}
	if len(g.Comparaisons) != 1 {
		t.Fatalf("only Ponytail has 3 cycles on each side: %+v", g.Comparaisons)
	}
	if c := g.Comparaisons[0]; c.Option != "ponytail" || c.AvecN != 3 || c.SansN != 3 || c.AvecCout != 1 || c.SansCout != 2 || c.Ecart != -50 || c.AvecSortie != 500 {
		t.Fatalf("%+v", c)
	}
	if g := calculerGains(stats[:4], false); len(g.Comparaisons) != 0 || g.Ponytail {
		t.Fatalf("2 cycles a side: no comparison: %+v", g)
	}
}

// What RTK spared, cycle by cycle, from its own totals.
func TestMesureRTK(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("RTK is a Linux and macOS program")
	}
	d := t.TempDir()
	totaux := filepath.Join(d, "totaux")
	faux := filepath.Join(d, "rtk")
	ecrireEssai(t, faux, "#!/bin/sh\n[ \"$1 $2 $3\" = \"gain --format json\" ] || exit 1\ncase \"$RTK_DB_PATH\" in */.athanor/rtk/history.db) ;; *) exit 1 ;; esac\nprintf '{\"summary\":{\"total_input\":%s,\"total_saved\":%s}}' $(cat '"+totaux+"')\n")
	_ = os.Chmod(faux, 0o755)
	p := &pilote{projet: d, prive: t.TempDir(), rtkBin: faux}
	ecrireEssai(t, totaux, "1000 600")
	if e, s := p.mesureRTK(); e != 1000 || s != 600 {
		t.Fatalf("first: %d %d", e, s)
	}
	ecrireEssai(t, totaux, "1500 900")
	if e, s := p.mesureRTK(); e != 500 || s != 300 {
		t.Fatalf("second: %d %d", e, s)
	}
	ecrireEssai(t, totaux, "200 100") // reset: counted from zero
	if e, s := p.mesureRTK(); e != 200 || s != 100 {
		t.Fatalf("after a reset: %d %d", e, s)
	}
	p.rtkBin = ""
	if e, s := p.mesureRTK(); e != 0 || s != 0 {
		t.Fatal("without RTK: nothing")
	}
}

// Setup & health: Ponytail on, off, and switched on again by its repair.
func TestSantePonytail(t *testing.T) {
	kit := t.TempDir()
	var b bytes.Buffer
	ancien := sortieSante
	sortieSante = &b
	defer func() { sortieSante = ancien }()
	if santePonytail(kit) != 0 || !strings.Contains(b.String(), "built in and on") {
		t.Fatalf("no .env: on: %s", b.String())
	}
	ecrireEssai(t, filepath.Join(kit, ".env"), "RTK=1\nPONYTAIL=0\n")
	if santePonytail(kit) != 1 || !strings.Contains(b.String(), "OFF") {
		t.Fatalf("off: %s", b.String())
	}
	if reparerSante("ponytail", kit) != 0 || !ponytailDansEnv(filepath.Join(kit, ".env")) {
		t.Fatalf("repair: %s", b.String())
	}
	if e, _ := os.ReadFile(filepath.Join(kit, ".env")); !strings.Contains(string(e), "RTK=1") {
		t.Fatalf("the other settings kept: %s", e)
	}
	if verifSante(runtime.GOOS, "ponytail") == nil {
		t.Fatal("no ponytail check on this system")
	}
}
