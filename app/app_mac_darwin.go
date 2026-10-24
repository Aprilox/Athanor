//go:build darwin

package main

// Athanor.app: Athanor opened from its bundle is a Mac application (Cocoa through the Objective-C runtime,
// without cgo: purego). An icon in the Dock while Athanor runs; a click on it opens the interface's
// window again; its menu (right click) says which pilots run; quitting with pilots running asks whether they
// keep working (they do without the application, and are found again when it reopens) or stop.
// Started from a terminal, Athanor stays as before (no Dock icon).

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Cocoa's loop must run on the process's first thread: main() starts on it and stays there.
func init() { runtime.LockOSThread() }

func sel(nom string) objc.SEL { return objc.RegisterName(nom) }

func nsChaine(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(sel("stringWithUTF8String:"), s)
}

// boucleAppMac runs Cocoa's loop (it never returns once started); false when Cocoa cannot be reached
// (the application then goes on without its Dock icon).
func boucleAppMac(a actionsAppMac) bool {
	app, _, ok := preparerAppMac(a)
	if !ok {
		return false
	}
	app.Send(sel("run")) // never returns: terminate: ends the process
	return true
}

// preparerAppMac: the application, its delegate (the Dock icon's behaviour) and its menu bar, on the first
// thread; false when Cocoa cannot be reached.
func preparerAppMac(a actionsAppMac) (objc.ID, objc.ID, bool) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return 0, 0, false
	}
	classeApp := objc.GetClass("NSApplication")
	if classeApp == 0 || objc.GetClass("NSMenu") == 0 || objc.GetClass("NSAlert") == 0 {
		return 0, 0, false
	}
	app := objc.ID(classeApp).Send(sel("sharedApplication"))
	if app == 0 {
		return 0, 0, false
	}
	tx := func() texteAppMac { return textesAppMac(a.langue()) }
	var delegue objc.ID
	element := func(titre string, action objc.SEL, touche string) objc.ID {
		e := objc.ID(objc.GetClass("NSMenuItem")).Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"), nsChaine(titre), action, nsChaine(touche))
		return e
	}
	classe, err := objc.RegisterClass("AthanorDelegue", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{ // the Dock icon clicked while Athanor runs
			Cmd: sel("applicationShouldHandleReopen:hasVisibleWindows:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, _ bool) bool {
				go a.ouvrir()
				return false
			},
		},
		{ // the Dock icon's menu: the pilots at work, and the window
			Cmd: sel("applicationDockMenu:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) objc.ID {
				t := tx()
				menu := objc.ID(objc.GetClass("NSMenu")).Send(sel("new"))
				etat := t.aucun
				if p := a.pilotes(); len(p) > 0 {
					etat = fmt.Sprintf(t.enCours, strings.Join(p, ", "))
				}
				e := element(etat, 0, "")
				e.Send(sel("setEnabled:"), false)
				menu.Send(sel("addItem:"), e)
				o := element(t.ouvrir, sel("ouvrirAthanor:"), "")
				o.Send(sel("setTarget:"), self)
				menu.Send(sel("addItem:"), o)
				return menu
			},
		},
		{
			Cmd: sel("ouvrirAthanor:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				go a.ouvrir()
			},
		},
		{ // Quit (Dock menu, ⌘Q): with pilots running, they keep working or stop
			Cmd: sel("applicationShouldTerminate:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) uint {
				p := a.pilotes()
				arreter := false
				if len(p) > 0 {
					t := tx()
					app.Send(sel("activateIgnoringOtherApps:"), true)
					alerte := objc.ID(objc.GetClass("NSAlert")).Send(sel("new"))
					alerte.Send(sel("setMessageText:"), nsChaine(fmt.Sprintf(t.titreQuitter, strings.Join(p, ", "))))
					alerte.Send(sel("setInformativeText:"), nsChaine(t.texteQuitter))
					alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.continuer)) // 1000, the default one
					alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.arreter))   // 1001
					alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.annuler))   // 1002
					switch alerte.Send(sel("runModal")) {
					case 1001:
						arreter = true
					case 1000:
					default:
						return 0 // NSTerminateCancel
					}
				}
				a.quitter(arreter)
				return 1 // NSTerminateNow
			},
		},
	})
	if err != nil || classe == 0 {
		return 0, 0, false
	}
	delegue = objc.ID(classe).Send(sel("new"))
	app.Send(sel("setActivationPolicy:"), 0) // NSApplicationActivationPolicyRegular: the Dock icon
	app.Send(sel("setDelegate:"), delegue)
	// the menu bar while Athanor is in front: Open Athanor, Quit Athanor (⌘Q)
	t := tx()
	barre := objc.ID(objc.GetClass("NSMenu")).Send(sel("new"))
	itemApp := objc.ID(objc.GetClass("NSMenuItem")).Send(sel("new"))
	barre.Send(sel("addItem:"), itemApp)
	menuApp := objc.ID(objc.GetClass("NSMenu")).Send(sel("new"))
	o := element(t.ouvrir, sel("ouvrirAthanor:"), "o")
	o.Send(sel("setTarget:"), delegue)
	menuApp.Send(sel("addItem:"), o)
	menuApp.Send(sel("addItem:"), objc.ID(objc.GetClass("NSMenuItem")).Send(sel("separatorItem")))
	menuApp.Send(sel("addItem:"), element(t.quitter, sel("terminate:"), "q"))
	itemApp.Send(sel("setSubmenu:"), menuApp)
	app.Send(sel("setMainMenu:"), barre)
	return app, delegue, true
}
