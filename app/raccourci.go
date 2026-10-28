package main

// A shortcut to the installed Athanor (the copy kept in Athanor's folder, embarque.go), wherever the user
// wants it: the downloaded file installs Athanor and can be thrown away; the shortcut opens the installed
// one, which updates itself. Windows: an Athanor.lnk (the Desktop by default); Linux: an athanor.desktop
// (the desktop by default, and always the applications menu); macOS: an Athanor.app that opens the
// installed Athanor (Applications by default).
//
//   athanor raccourci [FOLDER]     the shortcut made in FOLDER (default: the system's usual place)
//   GET/POST /api/raccourci        the same for the window: the default folder, the shortcuts made

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func init() {
	enregistrerCommande("raccourci", "raccourci [FOLDER]: a shortcut to the installed Athanor in FOLDER (default: the Desktop, the Mac's Applications)", lancerRaccourci)
}

// VueRaccourci: what the window shows of the shortcuts.
type VueRaccourci struct {
	Defaut     string         `json:"defaut"`               // the folder proposed
	Raccourcis []string       `json:"raccourcis,omitempty"` // the ones made, still there
	Lancement  *InfoLancement `json:"lancement,omitempty"`  // what the downloaded file did, when it opened this window
	Systeme    string         `json:"systeme"`
}

// cibleRaccourci: what a shortcut opens: the executable of Athanor's folder (never the downloaded file).
func cibleRaccourci(kit string) (string, error) {
	exe := exeDuKit(kit)
	if !fichierRegulierPilote(exe) {
		if e, err := os.Executable(); err == nil {
			exe = e // a copy of the source code run as it is
		} else {
			return "", errors.New("Athanor's executable was not found")
		}
	}
	return exe, nil
}

// creerRaccourci: the shortcut in that folder; the files made (a Linux shortcut is also put in the menu).
func creerRaccourci(kit, dossier string) ([]string, error) {
	dossier = strings.TrimSpace(dossier)
	if dossier == "" {
		dossier = dossierRaccourciDefaut()
	}
	if !filepath.IsAbs(dossier) {
		return nil, errors.New("Give the shortcut's folder as a full path.")
	}
	if st, err := os.Stat(dossier); err != nil || !st.IsDir() {
		return nil, errors.New("This folder does not exist.")
	}
	exe, err := cibleRaccourci(kit)
	if err != nil {
		return nil, err
	}
	return creerRaccourciSysteme(dossier, exe, kit)
}

func lancerRaccourci(args []string) int {
	kit := kitDuProgramme()
	l, err := creerRaccourci(kit, strings.Join(args, " "))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return 1
	}
	for _, f := range l {
		fmt.Println("Shortcut: " + f)
	}
	return 0
}

// lancementActuel: what the downloaded file did when it opened this application (read once, then out of the
// environment: the pilots and commands started from here do not carry it).
var lancementActuel = func() *InfoLancement {
	i := infoLancement()
	for _, k := range []string{"ATHANOR_LANCEMENT", "ATHANOR_LANCEMENT_FICHIER", "ATHANOR_LANCEMENT_ANCIEN", "ATHANOR_LANCEMENT_MAJ", "ATHANOR_LANCEMENT_REPARES"} {
		_ = os.Unsetenv(k)
	}
	return i
}()

func (s *Serveur) apiRaccourci(w http.ResponseWriter, r *http.Request) {
	kit := s.kitCourant().Racine
	if r.Method == http.MethodPost {
		var c struct {
			Dossier string `json:"dossier"`
		}
		if err := lireCorps(r, &c); err != nil {
			s.erreur(w, http.StatusBadRequest, "Invalid request")
			return
		}
		l, err := creerRaccourci(kit, c.Dossier)
		if err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		s.mu.Lock()
		for _, f := range l {
			deja := false
			for _, x := range s.config.Raccourcis {
				deja = deja || x == f
			}
			if !deja {
				s.config.Raccourcis = append(s.config.Raccourcis, f)
			}
		}
		err = ecrireConfig(s.config)
		s.mu.Unlock()
		if err != nil {
			s.erreur(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.ecrireJSON(w, map[string]any{"crees": l, "raccourci": s.vueRaccourci()})
		return
	}
	s.ecrireJSON(w, s.vueRaccourci())
}

func (s *Serveur) vueRaccourci() VueRaccourci {
	v := VueRaccourci{Defaut: dossierRaccourciDefaut(), Lancement: lancementActuel, Systeme: runtime.GOOS}
	s.mu.Lock()
	l := append([]string{}, s.config.Raccourcis...)
	s.mu.Unlock()
	for _, f := range l {
		if _, err := os.Lstat(f); err == nil {
			v.Raccourcis = append(v.Raccourcis, f)
		}
	}
	return v
}

// echapperExecDesktop: a path in a .desktop file's Exec= (the freedesktop rules: quoted, \ " ` $ escaped).
func echapperExecDesktop(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`)
	return `"` + r.Replace(p) + `"`
}

// entreeDesktop: Athanor's .desktop entry.
func entreeDesktop(exe, kit, icone string) string {
	e := "[Desktop Entry]\nType=Application\nName=Athanor\nGenericName=Claude Code, project after project\nComment=Claude Code building whole projects\n" +
		"Exec=" + echapperExecDesktop(exe) + "\nPath=" + kit + "\nTerminal=false\nCategories=Development;\nStartupNotify=true\n"
	if icone != "" {
		e += "Icon=" + icone + "\n"
	}
	return e
}

const marqueRaccourciMac = "athanor-raccourci"

// creerAppRaccourciMac: dossier/Athanor.app, a small application that opens the installed Athanor (with its
// Dock icon: the bundle is named to it). An Athanor.app that is not one of these (the downloaded one) is left
// alone: it opens the installed Athanor as well.
func creerAppRaccourciMac(dossier, exe string, icone []byte) (string, error) {
	app := filepath.Join(dossier, "Athanor.app")
	if st, err := os.Stat(app); err == nil && st.IsDir() {
		if _, err := os.Stat(filepath.Join(app, "Contents", "Resources", marqueRaccourciMac)); err != nil {
			return "", errors.New("An Athanor.app is already in this folder (the downloaded one?): it opens the installed Athanor too. Choose another folder, or keep it.")
		}
	}
	tmp := app + ".nouveau"
	_ = os.RemoveAll(tmp)
	c := filepath.Join(tmp, "Contents")
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(c, d), 0o755); err != nil {
			return "", err
		}
	}
	citer := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	script := "#!/bin/sh\n# Athanor: opens the installed Athanor (" + marqueRaccourciMac + ")\n" +
		"ATHANOR_APP_MAC=\"$(cd \"$(dirname \"$0\")/../..\" && pwd -P)\"\nexport ATHANOR_APP_MAC\nexec " + citer(exe) + " \"$@\"\n"
	plist := strings.Replace(infoPlistMac(versionAthanor), "<string>"+idBundleMac+"</string>", "<string>"+idBundleMac+".raccourci</string>", 1)
	for f, b := range map[string][]byte{"MacOS/athanor": []byte(script), "Info.plist": []byte(plist), "PkgInfo": []byte("APPL????"),
		"Resources/" + marqueRaccourciMac: []byte(exe + "\n")} {
		mode := os.FileMode(0o644)
		if f == "MacOS/athanor" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(c, filepath.FromSlash(f)), b, mode); err != nil {
			_ = os.RemoveAll(tmp)
			return "", err
		}
	}
	if len(icone) > 0 {
		_ = os.WriteFile(filepath.Join(c, "Resources", "athanor.icns"), icone, 0o644)
	}
	_ = os.RemoveAll(app)
	if err := os.Rename(tmp, app); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return app, nil
}
