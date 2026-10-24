package main

// Athanor.app (app_mac_darwin.go): what the Dock icon and its menus need from the application, and their
// texts. Kept apart to be built and tested on every system.

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// actionsAppMac: what the Dock icon does.
type actionsAppMac struct {
	ouvrir  func()          // the Dock icon clicked, "Open Athanor": a window of the interface
	pilotes func() []string // the projects whose pilot runs (the Dock menu, the question when quitting)
	quitter func(arreter bool)
	langue  func() string
}

// bundleMac: the Athanor.app an executable belongs to ("" when it is not inside one). A Mac path: always
// with "/" (path, not filepath, so that it reads the same on every system).
func bundleMac(exe string) string {
	d := path.Dir(filepath.ToSlash(exe)) // …/Athanor.app/Contents/MacOS
	if path.Base(d) != "MacOS" || path.Base(path.Dir(d)) != "Contents" {
		return ""
	}
	b := path.Dir(path.Dir(d))
	if !strings.HasSuffix(strings.ToLower(b), ".app") {
		return ""
	}
	return b
}

// bundleAthanorMac: the Athanor.app this application was opened from ("" otherwise). The bundle's executable
// becomes the copy kept in Athanor's folder (embarque.go) and leaves its path in ATHANOR_APP_MAC.
func bundleAthanorMac() string {
	b := os.Getenv("ATHANOR_APP_MAC")
	if b == "" || !strings.HasSuffix(strings.ToLower(b), ".app") {
		return ""
	}
	if st, err := os.Stat(b); err != nil || !st.IsDir() {
		return ""
	}
	return b
}

// bundleDurableMac: the same, when it stays where it is: a downloaded Athanor.app opened where it was
// downloaded runs from a temporary copy of macOS ("App Translocation", gone once it is quit), useless to
// reopen it later (after an update, at login).
func bundleDurableMac() string {
	b := bundleAthanorMac()
	if strings.Contains(filepath.ToSlash(b), "/AppTranslocation/") {
		return ""
	}
	return b
}

// textesAppMac: the Dock menu and the question asked when quitting with pilots running.
type texteAppMac struct {
	ouvrir, quitter, aucun, enCours, titreQuitter, texteQuitter, continuer, arreter, annuler string
}

func textesAppMac(langue string) texteAppMac {
	switch langue {
	case "fr":
		return texteAppMac{"Ouvrir Athanor", "Quitter Athanor", "Aucun pilote en cours", "Pilotes en cours : %s",
			"Des pilotes tournent : %s", "Ils peuvent continuer sans Athanor ouvert : rouvrez Athanor pour les retrouver. Ou arrêtez-les maintenant (le cycle en cours est coupé).",
			"Les laisser tourner", "Les arrêter", "Annuler"}
	case "es":
		return texteAppMac{"Abrir Athanor", "Salir de Athanor", "Ningún piloto en marcha", "Pilotos en marcha: %s",
			"Hay pilotos trabajando: %s", "Pueden seguir sin Athanor abierto: vuelva a abrir Athanor para verlos. O deténgalos ahora (el ciclo en curso se corta).",
			"Dejarlos trabajar", "Detenerlos", "Cancelar"}
	case "de":
		return texteAppMac{"Athanor öffnen", "Athanor beenden", "Kein Pilot läuft", "Laufende Piloten: %s",
			"Piloten laufen: %s", "Sie können ohne geöffnetes Athanor weiterarbeiten: Öffnen Sie Athanor wieder, um sie zu sehen. Oder stoppen Sie sie jetzt (der laufende Zyklus wird abgebrochen).",
			"Weiterlaufen lassen", "Stoppen", "Abbrechen"}
	case "it":
		return texteAppMac{"Apri Athanor", "Esci da Athanor", "Nessun pilota in corso", "Piloti in corso: %s",
			"Ci sono piloti al lavoro: %s", "Possono continuare senza Athanor aperto: riapri Athanor per ritrovarli. Oppure fermali ora (il ciclo in corso viene interrotto).",
			"Lasciali lavorare", "Fermali", "Annulla"}
	case "pt":
		return texteAppMac{"Abrir o Athanor", "Sair do Athanor", "Nenhum piloto em curso", "Pilotos em curso: %s",
			"Há pilotos a trabalhar: %s", "Podem continuar sem o Athanor aberto: volte a abrir o Athanor para os ver. Ou pare-os agora (o ciclo em curso é interrompido).",
			"Deixá-los trabalhar", "Pará-los", "Cancelar"}
	}
	return texteAppMac{"Open Athanor", "Quit Athanor", "No pilot running", "Pilots running: %s",
		"Pilots are running: %s", "They can go on without Athanor open: open Athanor again to find them. Or stop them now (the current cycle is cut off).",
		"Keep them running", "Stop them", "Cancel"}
}

// nomsPilotesActifs: the projects whose pilot runs.
func (s *Serveur) nomsPilotesActifs() []string {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	var r []string
	for _, p := range projets {
		if p.PiloteActif() {
			r = append(r, p.Nom)
		}
	}
	return r
}

// arreterTousPilotes: every running pilot stops now (quitting Athanor.app with "Stop them").
func (s *Serveur) arreterTousPilotes() {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	for _, p := range projets {
		if p.PiloteActif() {
			marqueAuto(p, false)
			_, _ = s.arreterPiloteProjet(p)
		}
	}
}
