package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOutilsInutilises(t *testing.T) {
	ancien := tailleOutilInutilise
	tailleOutilInutilise = 10
	defer func() { tailleOutilInutilise = ancien }()
	kit := t.TempDir()
	tools := filepath.Join(kit, "tools")
	for _, d := range []string{"gradle/caches", "minecraft/versions", "windows/jdk17/bin", "windows/ffmpeg", "bin", "pgdata", "petit"} {
		_ = os.MkdirAll(filepath.Join(tools, filepath.FromSlash(d)), 0o755)
	}
	for _, f := range []string{"gradle/caches/a.jar", "minecraft/versions/1.21.jar", "windows/jdk17/bin/java", "windows/ffmpeg/ffmpeg", "bin/rtk", "pgdata/base"} {
		_ = os.WriteFile(filepath.Join(tools, filepath.FromSlash(f)), []byte("0123456789abcdef"), 0o644)
	}
	_ = os.WriteFile(filepath.Join(tools, "petit", "x"), []byte("1"), 0o644) // under the size: never offered
	projet := filepath.Join(kit, "projet")
	_ = os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755)
	_ = os.WriteFile(filepath.Join(projet, ".athanor", "check.sh"), []byte("#!/bin/bash\n\"$TOOLS/windows/ffmpeg/ffmpeg\" -version\n"), 0o755)

	// first sight: every folder starts its 30 days now, nothing is offered
	if l := outilsInutilises(tools, nil); len(l) != 0 {
		t.Fatalf("offered at first sight: %v", l)
	}
	// 40 days later (the notes moved back), a cycle used Minecraft through a script and Gradle through gradlew
	u := lireUsageOutils(tools)
	for n := range u {
		u[n] = map[string]int64{"": time.Now().Add(-40 * 24 * time.Hour).Unix()}
	}
	ecrireUsageOutils(tools, u)
	_ = os.WriteFile(filepath.Join(projet, "smoke.sh"), []byte("java -jar \"$TOOLS/minecraft/versions/1.21.jar\"\n"), 0o755)
	noterUsageOutils(tools, projet, []commandeCycle{{cmd: "bash smoke.sh > out.log"}, {cmd: "./gradlew build"}})
	l := outilsInutilises(tools, []Projet{{Dossier: projet}})
	noms := map[string]bool{}
	for _, o := range l {
		noms[o.Nom] = true
	}
	// jdk17 only: Minecraft and Gradle were just used, ffmpeg is in a listed project's check, bin and pgdata are protected
	if len(l) != 1 || !noms["windows/jdk17"] || l[0].Taille != 16 {
		t.Fatalf("offered: %+v", l)
	}
}
