package main

// Migration from the previous name of the tool (Claude Autopilote → Athanor).
// Everything here only renames what an older version left behind, never deletes user data,
// and is a no-op on a fresh installation.

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ancienConfig  = "autopilote-config.json"
	ancienAtelier = ".autopilote"
)

// migrerConfig renames autopilote-config.json (next to the exe) to athanor-config.json.
func migrerConfig() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	ancien := filepath.Join(filepath.Dir(exe), ancienConfig)
	if _, err := os.Stat(cheminConfig()); err == nil {
		return
	}
	if _, err := os.Stat(ancien); err == nil {
		_ = os.Rename(ancien, cheminConfig())
	}
}

// MigrerAtelier renames the project's old .autopilote/ workshop to .athanor/, unless a pilot
// of the old version is still running in it (its heartbeat is fresh) or .athanor/ already exists.
// The pilot does the same at startup (and repairs git worktrees, which needs git).
func (p Projet) MigrerAtelier() {
	ancien := filepath.Join(p.Dossier, ancienAtelier)
	st, err := os.Lstat(ancien)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return
	}
	if fi, err := os.Stat(filepath.Join(ancien, "pilot.pid")); err == nil && time.Since(fi.ModTime()) < 90*time.Second {
		return // an old pilot is alive: it will be migrated at its next start
	}
	if st, err := os.Lstat(p.Atelier()); err == nil {
		// a near-empty .athanor/ (a STOP request made before the migration) is folded into the old one
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || existe(p.Demande()) || existe(p.Plan()) {
			return
		}
		entrees, _ := os.ReadDir(p.Atelier())
		for _, e := range entrees {
			if e.Type()&os.ModeSymlink == 0 && !existe(filepath.Join(ancien, e.Name())) {
				_ = os.Rename(filepath.Join(p.Atelier(), e.Name()), filepath.Join(ancien, e.Name()))
			}
		}
		if os.RemoveAll(p.Atelier()) != nil {
			return
		}
	}
	if os.Rename(ancien, p.Atelier()) != nil {
		return
	}
	// .git/info/exclude: the workshop stays out of git under its new name
	exclude := filepath.Join(p.Dossier, ".git", "info", "exclude")
	if fi, err := os.Lstat(exclude); err == nil && fi.Mode().IsRegular() {
		if b, err := os.ReadFile(exclude); err == nil {
			lignes := strings.Split(string(b), "\n")
			for i, l := range lignes {
				if strings.TrimSpace(strings.TrimSuffix(l, "\r")) == ancienAtelier+"/" {
					lignes[i] = ".athanor/"
				}
			}
			_ = os.WriteFile(exclude, []byte(strings.Join(lignes, "\n")), fi.Mode().Perm())
		}
	}
}

func existe(chemin string) bool { _, err := os.Lstat(chemin); return err == nil }

// MigrerUnreal: UNREAL_MCP=1 in the .env (versions before 2.13.0) becomes the "unreal" server of the
// verified catalog, switched on for every project, with the address the user had set. The .env keys
// are then turned off and no longer shown on the Settings page.
func (s *Serveur) MigrerUnreal() {
	k := s.kitCourant()
	actif, url := false, ""
	for _, l := range LireEnv(k.Env()) {
		switch l.Cle {
		case "UNREAL_MCP":
			actif = strings.TrimSpace(l.Valeur) == "1"
		case "UNREAL_MCP_URL":
			url = strings.TrimSpace(l.Valeur)
		}
	}
	if !actif {
		return
	}
	extMu.Lock()
	defer extMu.Unlock()
	b, choix, secrets := lireBiblio(k), lireChoix(k), lireSecrets(k)
	if _, ok := b.MCP["unreal"]; !ok {
		for _, m := range lireCatalogue().MCP {
			if m.ID != "unreal" {
				continue
			}
			x := MCPInstalle{Nom: m.Nom, Catalogue: m.ID, Config: m.Config, Reglages: map[string]string{}, Ajoute: maintenantISO()}
			for _, rg := range m.Reglages {
				x.Reglages[rg.Nom] = rg.Defaut
			}
			b.MCP["unreal"] = x
		}
	}
	if url != "" && url != "http://127.0.0.1:8000/mcp" {
		if secrets["unreal"] == nil {
			secrets["unreal"] = map[string]string{}
		}
		secrets["unreal"]["UNREAL_MCP_URL"] = url
	}
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	for _, p := range projets {
		ch := choix[cleProjet(p)]
		if !contient(ch.MCP, "unreal") {
			ch.MCP = append(ch.MCP, "unreal")
		}
		if ch.Skills == nil {
			ch.Skills = []string{}
		}
		choix[cleProjet(p)] = ch
	}
	if ecrireJSONFichier(filepath.Join(dossierExt(k), "bibliotheque.json"), b, 0o644) != nil ||
		ecrireJSONFichier(filepath.Join(dossierExt(k), "secrets.json"), secrets, 0o600) != nil ||
		ecrireJSONFichier(filepath.Join(dossierExt(k), "projets.json"), choix, 0o644) != nil {
		return
	}
	_ = EcrireEnv(k.Env(), map[string]string{"UNREAL_MCP": "0"})
}
