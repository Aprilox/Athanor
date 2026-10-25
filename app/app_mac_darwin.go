//go:build darwin

package main

// Athanor.app: Athanor opened from its bundle is a Mac application (Cocoa and WebKit through the
// Objective-C runtime, without cgo: purego).
//   - Its own window, the interface in WebKit (the Mac's web engine, as WebView2 on Windows): no browser.
//     The red button hides it; the Dock icon, the icon near the clock or ⌘O show it again.
//   - An icon near the clock (the menu bar) while Athanor runs: the pilots at work, Open Athanor, Quit.
//   - Quit with pilots running: Athanor stays in the menu bar (the Dock icon goes, the pilots go on), or
//     stops them and quits. Quit from the menu bar: Athanor quits, the pilots going on or not.
// When WebKit cannot be reached, the window is a browser's as before. Started from a terminal, Athanor stays
// as before (no Dock icon).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// Cocoa's loop must run on the process's first thread: main() starts on it and stays there.
func init() { runtime.LockOSThread() }

func sel(nom string) objc.SEL { return objc.RegisterName(nom) }

func classe(nom string) objc.ID { return objc.ID(objc.GetClass(nom)) }

func nsChaine(s string) objc.ID {
	return classe("NSString").Send(sel("stringWithUTF8String:"), s)
}

type nsRect struct{ X, Y, L, H float64 }
type nsTaille struct{ L, H float64 }

// blocObjC: the start of an Objective-C block (a completion handler): its function is called with the block
// itself, then the arguments.
type blocObjC struct {
	isa             uintptr
	drapeaux, resvd int32
	invoke          uintptr
}

func appelerBloc(b unsafe.Pointer, arg uintptr) {
	if b == nil {
		return
	}
	if f := (*blocObjC)(b).invoke; f != 0 {
		purego.SyscallN(f, uintptr(b), arg)
	}
}

// the application's state on the first thread (Cocoa's)
var mac struct {
	sync.Mutex
	app, delegue, fenetre, vue, icone objc.ID
	webkit                            bool
	a                                 actionsAppMac
}

// boucleAppMac runs Cocoa's loop (it never returns once started); false when Cocoa cannot be reached
// (the application then goes on as from a terminal).
func boucleAppMac(a actionsAppMac) bool {
	app, _, ok := preparerAppMac(a)
	if !ok {
		return false
	}
	app.Send(sel("run")) // never returns: terminate: ends the process
	return true
}

// demanderFenetreMac: the window shown (from any thread: Cocoa does it on its own).
func demanderFenetreMac() bool {
	mac.Lock()
	d := mac.delegue
	mac.Unlock()
	if d == 0 {
		return false
	}
	d.Send(sel("performSelectorOnMainThread:withObject:waitUntilDone:"), sel("ouvrirAthanor:"), objc.ID(0), false)
	return true
}

// montrerFenetreMac: on the first thread. The window is created the first time (WebKit), then shown again;
// the Dock icon comes back with it.
func montrerFenetreMac() {
	app := mac.app
	app.Send(sel("setActivationPolicy:"), 0) // Regular: the Dock icon
	if mac.fenetre == 0 && mac.webkit {
		creerFenetre()
	}
	if mac.fenetre == 0 { // no WebKit: a browser's window
		go mac.a.navigateur()
		return
	}
	mac.fenetre.Send(sel("makeKeyAndOrderFront:"), objc.ID(0))
	app.Send(sel("activateIgnoringOtherApps:"), true)
}

func creerFenetre() {
	conf := classe("WKWebViewConfiguration").Send(sel("new"))
	if conf == 0 {
		return
	}
	cadre := nsRect{0, 0, 1240, 820}
	vue := classe("WKWebView").Send(sel("alloc")).Send(sel("initWithFrame:configuration:"), cadre, conf)
	if vue == 0 {
		return
	}
	vue.Send(sel("setUIDelegate:"), mac.delegue)         // file choosers
	vue.Send(sel("setNavigationDelegate:"), mac.delegue) // a web process that stops is replaced
	fen := classe("NSWindow").Send(sel("alloc")).Send(sel("initWithContentRect:styleMask:backing:defer:"),
		cadre, uint(1|2|4|8), uint(2), false) // titled, closable, miniaturizable, resizable; buffered
	if fen == 0 {
		return
	}
	fen.Send(sel("setReleasedWhenClosed:"), false)
	fen.Send(sel("setTitle:"), nsChaine("Athanor"))
	fen.Send(sel("setContentView:"), vue)
	fen.Send(sel("setContentMinSize:"), nsTaille{900, 600})
	fen.Send(sel("setDelegate:"), mac.delegue) // the red button hides it
	fen.Send(sel("center"))
	fen.Send(sel("setFrameAutosaveName:"), nsChaine("Athanor")) // its place and size kept for the next time
	fen.Send(sel("makeFirstResponder:"), vue)                   // the keyboard goes to the interface
	mac.fenetre, mac.vue = fen, vue
	charger()
}

// charger: the interface in the window, through a fresh single-use address.
func charger() {
	u := classe("NSURL").Send(sel("URLWithString:"), nsChaine(mac.a.lien()))
	if u == 0 {
		return
	}
	mac.vue.Send(sel("loadRequest:"), classe("NSURLRequest").Send(sel("requestWithURL:"), u))
}

// quitterAvecPilotes: the question asked when quitting with pilots running; true to quit (stop says
// whether they stop).
func quitterAvecPilotes(p []string, depuisBarre bool) (quitter, arreter bool) {
	t := textesAppMac(mac.a.langue())
	mac.app.Send(sel("activateIgnoringOtherApps:"), true)
	alerte := classe("NSAlert").Send(sel("new"))
	alerte.Send(sel("setMessageText:"), nsChaine(fmt.Sprintf(t.titreQuitter, strings.Join(p, ", "))))
	if depuisBarre {
		alerte.Send(sel("setInformativeText:"), nsChaine(t.texteSansFenetre))
		alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.quitterSansArret)) // 1000, the default one
	} else {
		alerte.Send(sel("setInformativeText:"), nsChaine(t.texteGarder))
		alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.garder)) // 1000, the default one
	}
	alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.arreterQuitter)) // 1001
	alerte.Send(sel("addButtonWithTitle:"), nsChaine(t.annuler))        // 1002
	switch alerte.Send(sel("runModal")) {
	case 1000:
		if depuisBarre {
			return true, false
		}
		// Athanor stays in the menu bar: the window hidden, the Dock icon gone
		if mac.fenetre != 0 {
			mac.fenetre.Send(sel("orderOut:"), objc.ID(0))
		}
		mac.app.Send(sel("setActivationPolicy:"), 1) // Accessory: no Dock icon
		return false, false
	case 1001:
		return true, true
	}
	return false, false
}

// menuPilotes: the menu of the icon near the clock, and of the Dock icon.
func menuPilotes(menu objc.ID) {
	t := textesAppMac(mac.a.langue())
	menu.Send(sel("removeAllItems"))
	etat := t.aucun
	if p := mac.a.pilotes(); len(p) > 0 {
		etat = fmt.Sprintf(t.enCours, strings.Join(p, ", "))
	}
	e := element(etat, 0, "")
	e.Send(sel("setEnabled:"), false)
	menu.Send(sel("addItem:"), e)
	menu.Send(sel("addItem:"), classe("NSMenuItem").Send(sel("separatorItem")))
	o := element(t.ouvrir, sel("ouvrirAthanor:"), "")
	o.Send(sel("setTarget:"), mac.delegue)
	menu.Send(sel("addItem:"), o)
}

func element(titre string, action objc.SEL, touche string) objc.ID {
	return classe("NSMenuItem").Send(sel("alloc")).Send(sel("initWithTitle:action:keyEquivalent:"), nsChaine(titre), action, nsChaine(touche))
}

// preparerAppMac: the application, its delegate and its menus, on the first thread; false when Cocoa
// cannot be reached. The icon near the clock and the window come when Cocoa's loop starts.
func preparerAppMac(a actionsAppMac) (objc.ID, objc.ID, bool) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return 0, 0, false
	}
	_, errWK := purego.Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if objc.GetClass("NSApplication") == 0 || objc.GetClass("NSMenu") == 0 || objc.GetClass("NSAlert") == 0 {
		return 0, 0, false
	}
	app := classe("NSApplication").Send(sel("sharedApplication"))
	if app == 0 {
		return 0, 0, false
	}
	methodes := []objc.MethodDef{
		{ // the loop has started: the icon near the clock, the window
			Cmd: sel("applicationDidFinishLaunching:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				creerIcone()
				montrerFenetreMac()
			},
		},
		{ // the Dock icon clicked
			Cmd: sel("applicationShouldHandleReopen:hasVisibleWindows:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, _ bool) bool {
				montrerFenetreMac()
				return false
			},
		},
		{ // the Dock icon's menu
			Cmd: sel("applicationDockMenu:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) objc.ID {
				m := classe("NSMenu").Send(sel("new"))
				menuPilotes(m)
				return m
			},
		},
		{ // the menu of the icon near the clock, filled when it opens
			Cmd: sel("menuNeedsUpdate:"),
			Fn: func(self objc.ID, _ objc.SEL, menu objc.ID) {
				menuPilotes(menu)
				t := textesAppMac(mac.a.langue())
				menu.Send(sel("addItem:"), classe("NSMenuItem").Send(sel("separatorItem")))
				q := element(t.quitter, sel("quitterDepuisBarre:"), "")
				q.Send(sel("setTarget:"), self)
				menu.Send(sel("addItem:"), q)
			},
		},
		{
			Cmd: sel("ouvrirAthanor:"),
			Fn:  func(self objc.ID, _ objc.SEL, _ objc.ID) { montrerFenetreMac() },
		},
		{ // Quit from the icon near the clock: Athanor goes, the pilots going on or not
			Cmd: sel("quitterDepuisBarre:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				arreter := false
				if p := mac.a.pilotes(); len(p) > 0 {
					q, s := quitterAvecPilotes(p, true)
					if !q {
						return
					}
					arreter = s
				}
				mac.a.quitter(arreter)
				os.Exit(0)
			},
		},
		{ // Quit (⌘Q, the Dock menu): with pilots running, Athanor stays in the menu bar or stops them
			Cmd: sel("applicationShouldTerminate:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) uint {
				arreter := false
				if p := mac.a.pilotes(); len(p) > 0 {
					q, s := quitterAvecPilotes(p, false)
					if !q {
						return 0 // NSTerminateCancel
					}
					arreter = s
				}
				mac.a.quitter(arreter)
				return 1 // NSTerminateNow
			},
		},
		{ // the red button: the window hides, Athanor goes on
			Cmd: sel("windowShouldClose:"),
			Fn: func(self objc.ID, _ objc.SEL, fen objc.ID) bool {
				fen.Send(sel("orderOut:"), objc.ID(0))
				return false
			},
		},
		{ // <input type="file"> (attach files to a request): the Mac's file chooser
			Cmd: sel("webView:runOpenPanelWithParameters:initiatedByFrame:completionHandler:"),
			Fn: func(self objc.ID, _ objc.SEL, _ objc.ID, params objc.ID, _ objc.ID, fin unsafe.Pointer) {
				panneau := classe("NSOpenPanel").Send(sel("openPanel"))
				panneau.Send(sel("setCanChooseFiles:"), true)
				panneau.Send(sel("setCanChooseDirectories:"), false)
				panneau.Send(sel("setAllowsMultipleSelection:"), objc.Send[bool](params, sel("allowsMultipleSelection")))
				var urls objc.ID
				if panneau.Send(sel("runModal")) == 1 { // NSModalResponseOK
					urls = panneau.Send(sel("URLs"))
				}
				appelerBloc(fin, uintptr(urls))
			},
		},
		{ // the page's web process stopped (memory, crash): the interface again
			Cmd: sel("webViewWebContentProcessDidTerminate:"),
			Fn:  func(self objc.ID, _ objc.SEL, _ objc.ID) { charger() },
		},
	}
	cl, err := objc.RegisterClass("AthanorDelegue", objc.GetClass("NSObject"), nil, nil, methodes)
	if err != nil || cl == 0 {
		return 0, 0, false
	}
	delegue := objc.ID(cl).Send(sel("new"))
	mac.Lock()
	mac.app, mac.delegue, mac.a = app, delegue, a
	mac.webkit = errWK == nil && objc.GetClass("WKWebView") != 0
	mac.Unlock()
	app.Send(sel("setActivationPolicy:"), 0) // Regular: the Dock icon
	app.Send(sel("setDelegate:"), delegue)
	// the Dock icon and the menu bar's name: the bundle's (the copy of Athanor's folder runs outside it)
	if b := bundleAthanorMac(); b != "" {
		if img := classe("NSImage").Send(sel("alloc")).Send(sel("initWithContentsOfFile:"), nsChaine(filepath.Join(b, "Contents", "Resources", "athanor.icns"))); img != 0 {
			app.Send(sel("setApplicationIconImage:"), img)
		}
	}
	classe("NSProcessInfo").Send(sel("processInfo")).Send(sel("setProcessName:"), nsChaine("Athanor"))
	t := textesAppMac(a.langue())
	// the menu bar while Athanor is in front: Athanor (Open, Quit), Edit (the copy and paste of the
	// interface's fields need it), Window
	barre := classe("NSMenu").Send(sel("new"))
	sousMenu := func(titre string, elements ...objc.ID) {
		item := classe("NSMenuItem").Send(sel("new"))
		m := classe("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsChaine(titre))
		for _, e := range elements {
			m.Send(sel("addItem:"), e)
		}
		item.Send(sel("setSubmenu:"), m)
		barre.Send(sel("addItem:"), item)
	}
	o := element(t.ouvrir, sel("ouvrirAthanor:"), "o")
	o.Send(sel("setTarget:"), delegue)
	refaire := element(t.refaire, sel("redo:"), "z")
	refaire.Send(sel("setKeyEquivalentModifierMask:"), uint(1<<17|1<<20)) // ⇧⌘Z
	sousMenu("Athanor", o, classe("NSMenuItem").Send(sel("separatorItem")), element(t.quitter, sel("terminate:"), "q"))
	sousMenu(t.edition, element(t.defaire, sel("undo:"), "z"), refaire, classe("NSMenuItem").Send(sel("separatorItem")),
		element(t.couper, sel("cut:"), "x"), element(t.copier, sel("copy:"), "c"), element(t.coller, sel("paste:"), "v"),
		element(t.toutSelectionner, sel("selectAll:"), "a"))
	sousMenu(t.fenetre, element(t.fermerFenetre, sel("performClose:"), "w"), element(t.reduire, sel("performMiniaturize:"), "m"))
	app.Send(sel("setMainMenu:"), barre)
	return app, delegue, true
}

// creerIcone: the icon near the clock (once Cocoa's loop has started, as macOS expects): Athanor's furnace
// (an SF Symbol, macOS 11), else its name.
func creerIcone() {
	if mac.icone != 0 {
		return
	}
	icone := classe("NSStatusBar").Send(sel("systemStatusBar")).Send(sel("statusItemWithLength:"), float64(-1)) // variable width
	if icone == 0 {
		return
	}
	// kept: an item no one keeps leaves the menu bar
	icone.Send(sel("retain"))
	icone.Send(sel("setVisible:"), true)
	bouton := icone.Send(sel("button"))
	if bouton != 0 {
		if img := classe("NSImage").Send(sel("imageWithSystemSymbolName:accessibilityDescription:"), nsChaine("flame"), nsChaine("Athanor")); img != 0 {
			img.Send(sel("setTemplate:"), true) // drawn in the menu bar's colour (light or dark)
			bouton.Send(sel("setImage:"), img)
		} else {
			bouton.Send(sel("setTitle:"), nsChaine("Athanor"))
		}
		bouton.Send(sel("setToolTip:"), nsChaine("Athanor"))
	}
	m := classe("NSMenu").Send(sel("new"))
	m.Send(sel("setDelegate:"), mac.delegue)
	icone.Send(sel("setMenu:"), m)
	mac.icone = icone
}
