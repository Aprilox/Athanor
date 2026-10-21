//go:build windows

package main

// Closing the window while native pilots run: the question (go on in the background, or stop) is asked
// in the page's own dialog, and the window waits for the answer. The page may also cancel the closing.
// A page that does not show the question within 3 seconds (not loaded, a script error) lets the window
// close, and Windows' dialog asks instead (fermetureNative).
// Everything here runs on the window's thread: no lock.

import (
	"encoding/json"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

type fermetureFenetre struct {
	vue      webview2.WebView
	hwnd     uintptr
	permise  bool // the answer is in: WM_CLOSE goes through
	affichee bool // the page shows the question
	tour     int  // the closing request under way (a later one replaces it)
}

var fermetureApp fermetureFenetre

func installerFermeture(w webview2.WebView) {
	f := &fermetureApp
	f.vue, f.hwnd = w, uintptr(w.Window())
	_ = w.Bind("athanorFermer", func(choix string) {
		switch choix {
		case "affiche":
			f.affichee = true
		case "continuer", "arreter":
			if choix == "continuer" {
				decisionFermeture.Store(1)
			} else {
				decisionFermeture.Store(2)
			}
			f.permise = true
			cPostMessageW.Call(f.hwnd, wmClose, 0, 0)
		case "annuler":
			f.affichee = false
			f.tour++
		}
	})
}

// message: WM_CLOSE. True: the window stays open while the page asks.
func (f *fermetureFenetre) message(hwnd uintptr) (uintptr, bool) {
	if f.permise || f.vue == nil {
		return 0, false
	}
	noms := pilotesAvantFermeture()
	if len(noms) == 0 {
		return 0, false
	}
	// closed from the taskbar while minimized: the question must be seen
	if r, _, _ := pIsIconic.Call(hwnd); r != 0 {
		pShowWindow.Call(hwnd, 9) // SW_RESTORE
	}
	pSetForegroundWindow.Call(hwnd)
	b, _ := json.Marshal(noms)
	f.tour++
	tour := f.tour
	f.affichee = false
	f.vue.Eval("window.athanorFermeture&&window.athanorFermeture(" + string(b) + ")")
	go func() {
		time.Sleep(3 * time.Second)
		f.vue.Dispatch(func() {
			if f.tour == tour && !f.affichee && !f.permise {
				f.permise = true // Windows' dialog asks once the window is closed
				cPostMessageW.Call(f.hwnd, wmClose, 0, 0)
			}
		})
	}()
	return 0, true
}
