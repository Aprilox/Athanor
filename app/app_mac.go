package main

// Athanor.app (app_mac_darwin.go): what the Dock icon and its menus need from the application, and their
// texts. Kept apart to be built and tested on every system.

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// actionsAppMac: what Athanor.app needs from the application.
type actionsAppMac struct {
	lien       func() string   // a single-use address of the interface, for the window (acces.go)
	navigateur func()          // the interface in a browser's window, when the Mac's own one cannot open
	pilotes    func() []string // the projects whose pilot runs (the menus, the question when quitting)
	quitter    func(arreter bool)
	langue     func() string
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

// texteAppMac: the menus of Athanor.app (Dock, menu bar, the icon near the clock) and the question asked
// when quitting with pilots running.
type texteAppMac struct {
	ouvrir, quitter, aucun, enCours, titreQuitter string
	texteGarder, garder, arreterQuitter           string // quitting from the window: the menu bar keeps Athanor
	texteSansFenetre, quitterSansArret            string // quitting from the menu bar
	annuler, edition, defaire, refaire            string
	couper, copier, coller, toutSelectionner      string
	fenetre, fermerFenetre, reduire               string
}

func textesAppMac(langue string) texteAppMac {
	switch langue {
	case "fr":
		return texteAppMac{"Ouvrir Athanor", "Quitter Athanor", "Aucun pilote en cours", "Pilotes en cours : %s", "Des pilotes tournent : %s",
			"Ils continuent avec Athanor dans la barre des menus (son icône près de l'horloge), d'où vous le rouvrez. Ou arrêtez-les maintenant (le cycle en cours est coupé).",
			"Garder dans la barre des menus", "Les arrêter et quitter",
			"Ils peuvent continuer sans Athanor : rouvrez Athanor pour les retrouver. Ou arrêtez-les maintenant (le cycle en cours est coupé).", "Quitter, ils continuent",
			"Annuler", "Édition", "Annuler la frappe", "Rétablir", "Couper", "Copier", "Coller", "Tout sélectionner", "Fenêtre", "Fermer la fenêtre", "Placer dans le Dock"}
	case "es":
		return texteAppMac{"Abrir Athanor", "Salir de Athanor", "Ningún piloto en marcha", "Pilotos en marcha: %s", "Hay pilotos trabajando: %s",
			"Siguen con Athanor en la barra de menús (su icono junto al reloj), desde donde lo vuelve a abrir. O deténgalos ahora (el ciclo en curso se corta).",
			"Mantener en la barra de menús", "Detenerlos y salir",
			"Pueden seguir sin Athanor: vuelva a abrir Athanor para verlos. O deténgalos ahora (el ciclo en curso se corta).", "Salir, siguen trabajando",
			"Cancelar", "Edición", "Deshacer", "Rehacer", "Cortar", "Copiar", "Pegar", "Seleccionar todo", "Ventana", "Cerrar ventana", "Minimizar"}
	case "de":
		return texteAppMac{"Athanor öffnen", "Athanor beenden", "Kein Pilot läuft", "Laufende Piloten: %s", "Piloten laufen: %s",
			"Sie arbeiten weiter, mit Athanor in der Menüleiste (sein Symbol bei der Uhr), von wo Sie es wieder öffnen. Oder stoppen Sie sie jetzt (der laufende Zyklus wird abgebrochen).",
			"In der Menüleiste behalten", "Stoppen und beenden",
			"Sie können ohne Athanor weiterarbeiten: Öffnen Sie Athanor wieder, um sie zu sehen. Oder stoppen Sie sie jetzt (der laufende Zyklus wird abgebrochen).", "Beenden, sie arbeiten weiter",
			"Abbrechen", "Bearbeiten", "Widerrufen", "Wiederholen", "Ausschneiden", "Kopieren", "Einsetzen", "Alles auswählen", "Fenster", "Fenster schließen", "Im Dock ablegen"}
	case "it":
		return texteAppMac{"Apri Athanor", "Esci da Athanor", "Nessun pilota in corso", "Piloti in corso: %s", "Ci sono piloti al lavoro: %s",
			"Continuano con Athanor nella barra dei menu (la sua icona vicino all'orologio), da cui lo riapri. Oppure fermali ora (il ciclo in corso viene interrotto).",
			"Tieni nella barra dei menu", "Fermali ed esci",
			"Possono continuare senza Athanor: riapri Athanor per ritrovarli. Oppure fermali ora (il ciclo in corso viene interrotto).", "Esci, continuano",
			"Annulla", "Modifica", "Annulla", "Ripeti", "Taglia", "Copia", "Incolla", "Seleziona tutto", "Finestra", "Chiudi finestra", "Contrai"}
	case "pt":
		return texteAppMac{"Abrir o Athanor", "Sair do Athanor", "Nenhum piloto em curso", "Pilotos em curso: %s", "Há pilotos a trabalhar: %s",
			"Continuam com o Athanor na barra de menus (o seu ícone junto ao relógio), de onde o volta a abrir. Ou pare-os agora (o ciclo em curso é interrompido).",
			"Manter na barra de menus", "Pará-los e sair",
			"Podem continuar sem o Athanor: volte a abrir o Athanor para os ver. Ou pare-os agora (o ciclo em curso é interrompido).", "Sair, continuam",
			"Cancelar", "Edição", "Desfazer", "Refazer", "Cortar", "Copiar", "Colar", "Selecionar tudo", "Janela", "Fechar janela", "Minimizar"}
	}
	return texteAppMac{"Open Athanor", "Quit Athanor", "No pilot running", "Pilots running: %s", "Pilots are running: %s",
		"They keep working with Athanor in the menu bar (its icon near the clock), where you open it again. Or stop them now (the current cycle is cut off).",
		"Keep in the menu bar", "Stop them and quit",
		"They can go on without Athanor: open Athanor again to find them. Or stop them now (the current cycle is cut off).", "Quit, they keep working",
		"Cancel", "Edit", "Undo", "Redo", "Cut", "Copy", "Paste", "Select All", "Window", "Close Window", "Minimize"}
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
