//go:build windows

package main

// The application's own title bar. The window keeps its standard frame, so Windows still draws the
// shadow, the rounded corners and the resize borders on the left, right and bottom, and keeps the
// animations, snapping and Win + arrows. Only the title bar is taken away (WM_NCCALCSIZE), and the page
// draws its own bar there (#fenetre-barre in static/).
//
// Two invisible windows lie over the page where Windows itself must answer: the top edge (resizing) and
// the three buttons (minimize; maximize, whose hover shows Windows 11's snap layouts; close). They answer
// Windows' hit tests (WM_NCHITTEST) and pass the clicks on to the window. The page draws the hover and
// pressed states Go sends it. The rest of the bar moves the window through athanorFenetre("deplacer").
//
// If anything is missing (Windows before 10, an invisible window that cannot be created, a page that
// never reports its bar), the window goes back to Windows' title bar.
// Everything here runs on the window's thread (window messages, and the page's calls, which WebView2
// delivers on that thread): no lock.

import (
	"encoding/json"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

var (
	cScreenToClient         = user32.NewProc("ScreenToClient")
	cIsZoomed               = user32.NewProc("IsZoomed")
	cSendMessageW           = user32.NewProc("SendMessageW")
	cPostMessageW           = user32.NewProc("PostMessageW")
	cTrackMouseEvent        = user32.NewProc("TrackMouseEvent")
	cReleaseCapture         = user32.NewProc("ReleaseCapture")
	cGetCursorPos           = user32.NewProc("GetCursorPos")
	cGetSystemMenu          = user32.NewProc("GetSystemMenu")
	cEnableMenuItem         = user32.NewProc("EnableMenuItem")
	cSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	cTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	cMonitorFromWindow      = user32.NewProc("MonitorFromWindow")
	cGetMonitorInfoW        = user32.NewProc("GetMonitorInfoW")
	cGetDpiForWindow        = user32.NewProc("GetDpiForWindow")
	cGetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
	cGetAsyncKeyState       = user32.NewProc("GetAsyncKeyState")
	cSHAppBarMessage        = syscall.NewLazyDLL("shell32.dll").NewProc("SHAppBarMessage")
)

const (
	wmSize            = 0x0005
	wmActivate        = 0x0006
	wmClose           = 0x0010
	wmEraseBkgnd      = 0x0014
	wmNcCalcSize      = 0x0083
	wmNcHitTest       = 0x0084
	wmNcMouseMove     = 0x00A0
	wmNcLButtonDown   = 0x00A1
	wmNcLButtonUp     = 0x00A2
	wmNcLButtonDblClk = 0x00A3
	wmNcRButtonDown   = 0x00A4
	wmNcRButtonUp     = 0x00A5
	wmSysCommand      = 0x0112
	wmEnterSizeMove   = 0x0231
	wmExitSizeMove    = 0x0232
	wmNcMouseLeave    = 0x02A2
	wmCadrePlacer     = 0x8000 + 0x41 // WM_APP + n: placed again once WebView2 has handled a resize
	wmCadreAgir       = 0x8000 + 0x42 // WM_APP + n: a button of the page, done outside the page's call

	scSize     = 0xF000
	scMove     = 0xF010
	scMinimize = 0xF020
	scMaximize = 0xF030
	scClose    = 0xF060
	scRestore  = 0xF120

	swpNoSize, swpNoMove, swpNoZOrder, swpNoActivate = 0x1, 0x2, 0x4, 0x10
	swpFrameChanged, swpShow, swpHide                = 0x20, 0x40, 0x80

	actionMenu = 1000 // athanorFenetre("menu"): Windows' window menu at the mouse
)

type pointW struct{ X, Y int32 }
type rectW struct{ G, H, D, B int32 }

type infoEcran struct {
	cb             uint32
	ecran, travail rectW
	drapeaux       uint32
}

type barreAppli struct { // APPBARDATA
	cb      uint32
	hwnd    uintptr
	message uint32
	bord    uint32
	rc      rectW
	lparam  uintptr
}

type suiviSouris struct { // TRACKMOUSEEVENT
	cb, drapeaux uint32
	hwnd         uintptr
	delai        uint32
}

// etatCadre: what the page draws.
type etatCadre struct {
	Cadre  bool   `json:"cadre"`  // false: the page takes its bar away (Windows' title bar is back)
	Max    bool   `json:"max"`    // maximized: the middle button shows "restore"
	Actif  bool   `json:"actif"`  // the window has the focus
	Survol string `json:"survol"` // button under the mouse: reduire, agrandir, fermer
	Appui  string `json:"appui"`  // button pressed
}

type cadreFenetre struct {
	voulu         bool // our bar replaces Windows' title bar
	hwnd          uintptr
	haut, boutons uintptr // the invisible windows over the top edge and over the three buttons
	page          zonesPage
	echelle       float64 // 0 until the page reports its buttons
	survol, appui uintptr
	survolPar     uintptr // the invisible window the hover belongs to
	enBoucle      bool    // Windows is moving or resizing the window: the page's sizes lag behind
	actif         bool
	vue           webview2.WebView
	dernier       string // the state last sent to the page
}

var (
	cadreApp     cadreFenetre
	classeCalque *uint16
	procCalque   = syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr { return cadreApp.calque(h, msg, wp, lp) })
)

// preparerCadre: before the window is created, so that it opens without Windows' title bar.
func preparerCadre() {
	if barreDeWindows.Load() || windows.RtlGetVersion().MajorVersion < 10 {
		return
	}
	cadreApp.voulu, cadreApp.actif = true, true
}

// messageFenetre: the window's messages, before the library's handling (true: handled here).
func messageFenetre(hwnd, msg, wp, lp uintptr) (uintptr, bool) {
	if msg == wmClose {
		if r, ok := fermetureApp.message(hwnd); ok {
			return r, true
		}
	}
	return cadreApp.message(hwnd, msg, wp, lp)
}

// installerCadre: once the window and its page exist, before the page is loaded.
func installerCadre(w webview2.WebView) {
	c := &cadreApp
	if !c.voulu {
		return
	}
	c.hwnd, c.vue = uintptr(w.Window()), w
	var inst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &inst)
	classeCalque, _ = syscall.UTF16PtrFromString("AthanorBarreTitre")
	curseur, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	type wndclassex struct {
		cbSize, style                uint32
		proc                         uintptr
		clsExtra, wndExtra           int32
		instance, icon, cursor, fond uintptr
		menu, classe                 *uint16
		iconSm                       uintptr
	}
	wc := wndclassex{style: 0x8, proc: procCalque, instance: uintptr(inst), cursor: curseur, classe: classeCalque} // CS_DBLCLKS
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	creer := func() uintptr {
		// WS_EX_LAYERED | WS_EX_NOREDIRECTIONBITMAP: nothing drawn, the page shows through; WS_CHILD
		h, _, _ := pCreateWindowExW.Call(0x80000|0x200000, uintptr(unsafe.Pointer(classeCalque)), 0,
			0x40000000, 0, 0, 0, 0, c.hwnd, 0, uintptr(inst), 0)
		if h != 0 {
			pSetLayeredWindowAttributes.Call(h, 0, 255, 2) // LWA_ALPHA: shown (it has nothing to draw)
		}
		return h
	}
	c.boutons = creer()
	c.haut = creer()
	if c.haut == 0 || c.boutons == 0 {
		c.retirer()
		return
	}
	_ = w.Bind("athanorFenetre", func(action string) {
		if !c.voulu {
			return
		}
		switch action {
		case "deplacer": // the bar is dragged: Windows moves the window, with its snapping
			// not when the button is already up (a quick flick): Windows would move the window
			// with the mouse until the next click
			bouton := uintptr(1)                                   // VK_LBUTTON
			if inv, _, _ := pGetSystemMetrics.Call(23); inv != 0 { // SM_SWAPBUTTON
				bouton = 2 // VK_RBUTTON
			}
			if r, _, _ := cGetAsyncKeyState.Call(bouton); r&0x8000 == 0 {
				return
			}
			cReleaseCapture.Call()
			var p pointW
			cGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
			cPostMessageW.Call(c.hwnd, wmNcLButtonDown, htCaption, uintptr(uint16(p.X))|uintptr(uint16(p.Y))<<16)
		case "reduire":
			cPostMessageW.Call(c.hwnd, wmCadreAgir, htMinimize, 0)
		case "agrandir":
			cPostMessageW.Call(c.hwnd, wmCadreAgir, htMaximize, 0)
		case "fermer":
			cPostMessageW.Call(c.hwnd, wmCadreAgir, htClose, 0)
		case "menu":
			cPostMessageW.Call(c.hwnd, wmCadreAgir, actionMenu, 0)
		}
	})
	_ = w.Bind("athanorZones", func(z zonesPage) {
		if c.voulu && !c.enBoucle { // while Windows resizes, the page's width lags behind the window's
			if e := z.echelle(c.largeur()); e > 0 {
				c.page, c.echelle = z, e
				c.placer()
			}
		}
		c.dernier = "" // a page (re)loaded: it gets the whole state again
		c.peindre()
	})
	w.Init("window.__athanorCadre=1")
	// a page that could not load (WebView2's error page) has no bar: Windows' comes back
	webview2.NavigationHook = func(ok bool) {
		if !ok && c.voulu {
			c.retirer()
		}
		c.placer() // above the page again, should WebView2 have raised it
	}
	c.placer()
	// the window was created with Windows' title bar: the frame is computed again without it
	pSetWindowPos.Call(c.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	// the page never reported its buttons (not loaded, or a script error): Windows' title bar comes back,
	// so that the window can always be moved and closed
	go func() {
		time.Sleep(20 * time.Second)
		w.Dispatch(func() {
			if c.voulu && c.echelle == 0 {
				c.retirer()
			}
		})
	}()
}

// retirer: Windows' title bar again.
func (c *cadreFenetre) retirer() {
	c.voulu = false
	for _, h := range []uintptr{c.haut, c.boutons} {
		if h != 0 {
			pShowWindow.Call(h, 0) // SW_HIDE
		}
	}
	if c.hwnd != 0 {
		pSetWindowPos.Call(c.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	}
	if c.vue != nil {
		c.vue.Eval(`window.athanorCadre&&window.athanorCadre({"cadre":false})`)
	}
}

func agrandie(h uintptr) bool { r, _, _ := cIsZoomed.Call(h); return r != 0 }

func (c *cadreFenetre) largeur() int32 {
	var rc rectW
	pGetClientRect.Call(c.hwnd, uintptr(unsafe.Pointer(&rc)))
	return rc.D
}

// bordHaut: the height of the resize edge at the top (that of Windows' own frame).
func bordHaut(h uintptr) int32 {
	if cGetDpiForWindow.Find() == nil && cGetSystemMetricsForDpi.Find() == nil {
		if dpi, _, _ := cGetDpiForWindow.Call(h); dpi != 0 {
			a, _, _ := cGetSystemMetricsForDpi.Call(33, dpi) // SM_CYSIZEFRAME
			b, _, _ := cGetSystemMetricsForDpi.Call(92, dpi) // SM_CXPADDEDBORDER
			return int32(a + b)
		}
	}
	a, _, _ := pGetSystemMetrics.Call(33)
	b, _, _ := pGetSystemMetrics.Call(92)
	return int32(a + b)
}

func entier(v int32) uintptr { return uintptr(int(v)) } // negative coordinates kept negative

// message: the window's messages, before the default handling (true: handled here).
func (c *cadreFenetre) message(hwnd, msg, wp, lp uintptr) (uintptr, bool) {
	if !c.voulu {
		return 0, false
	}
	if c.hwnd == 0 {
		c.hwnd = hwnd // during CreateWindowEx: the only window of this class
	}
	switch msg {
	case wmNcCalcSize:
		// lp is the address Windows gives: NCCALCSIZE_PARAMS (wp TRUE), or the window's RECT alone
		// (wp FALSE, when the window is created); the first rectangle is the same in both
		r := *(**rectW)(unsafe.Pointer(&lp))
		haut := r.H
		pDefWindowProcW.Call(hwnd, msg, wp, lp) // the standard frame on the left, right and bottom
		r.H = haut                              // no title bar
		if agrandie(hwnd) {
			// maximized, the window overflows the screen by its frame: the page starts at the screen's top
			r.H += bordHaut(hwnd)
			laisserBarreDesTaches(hwnd, r)
		}
		return 0, true
	case wmNcLButtonDown:
		// moving or resizing: straight to Windows, without the library's SetFocus on the window,
		// which took the keyboard away from the page
		r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
		return r, true
	case wmEnterSizeMove:
		c.enBoucle = true
	case wmExitSizeMove:
		c.enBoucle = false
		if c.vue != nil {
			c.vue.Eval("window.envoyerZones&&window.envoyerZones()") // the page's sizes, now settled
		}
	case wmSize:
		c.placer()
		cPostMessageW.Call(hwnd, wmCadrePlacer, 0, 0)
		c.peindre()
	case wmActivate:
		c.actif = wp&0xFFFF != 0
		c.peindre()
	case wmCadrePlacer:
		c.placer()
		return 0, true
	case wmCadreAgir:
		c.agir(wp)
		return 0, true
	}
	return 0, false
}

// laisserBarreDesTaches: maximized, the window must leave a line of the screen to a taskbar that hides
// itself, or that taskbar could never come back.
func laisserBarreDesTaches(h uintptr, r *rectW) {
	m, _, _ := cMonitorFromWindow.Call(h, 2) // MONITOR_DEFAULTTONEAREST
	mi := infoEcran{}
	mi.cb = uint32(unsafe.Sizeof(mi))
	if ok, _, _ := cGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return
	}
	for bord := uint32(0); bord < 4; bord++ { // ABE_LEFT, TOP, RIGHT, BOTTOM
		ab := barreAppli{bord: bord, rc: mi.ecran}
		ab.cb = uint32(unsafe.Sizeof(ab))
		if b, _, _ := cSHAppBarMessage.Call(0xB, uintptr(unsafe.Pointer(&ab))); b != 0 { // ABM_GETAUTOHIDEBAREX
			switch bord {
			case 0:
				r.G += 2
			case 1:
				r.H += 2
			case 2:
				r.D -= 2
			case 3:
				r.B -= 2
			}
		}
	}
}

// placer: the invisible windows over the top edge (not when maximized) and over the buttons.
func (c *cadreFenetre) placer() {
	if c.haut == 0 || c.boutons == 0 || !c.voulu {
		return
	}
	l := c.largeur()
	drap := uintptr(swpNoActivate | swpShow)
	if agrandie(c.hwnd) || l <= 0 {
		drap = swpNoActivate | swpHide
	}
	pSetWindowPos.Call(c.haut, 0, 0, 0, entier(l), entier(bordHaut(c.hwnd)), drap) // HWND_TOP: above the page
	u := union(c.page.boutons(l, c.echelle))
	if u.vide() {
		pSetWindowPos.Call(c.boutons, 0, 0, 0, 0, 0, swpNoActivate|swpHide)
		return
	}
	pSetWindowPos.Call(c.boutons, 0, entier(u.G), entier(u.H), entier(u.D-u.G), entier(u.B-u.H), swpNoActivate|swpShow)
}

func (c *cadreFenetre) etat() etatCadre {
	return etatCadre{Cadre: c.voulu, Max: c.hwnd != 0 && agrandie(c.hwnd), Actif: c.actif, Survol: nomBouton(c.survol), Appui: nomBouton(c.appui)}
}

// peindre: the state to the page, when it changed.
func (c *cadreFenetre) peindre() {
	if c.vue == nil || !c.voulu {
		return
	}
	b, _ := json.Marshal(c.etat())
	if string(b) == c.dernier {
		return
	}
	c.dernier = string(b)
	c.vue.Eval("window.athanorCadre&&window.athanorCadre(" + string(b) + ")")
}

func (c *cadreFenetre) agir(code uintptr) {
	switch code {
	case htMinimize:
		cPostMessageW.Call(c.hwnd, wmSysCommand, scMinimize, 0)
	case htMaximize:
		if agrandie(c.hwnd) {
			cPostMessageW.Call(c.hwnd, wmSysCommand, scRestore, 0)
		} else {
			cPostMessageW.Call(c.hwnd, wmSysCommand, scMaximize, 0)
		}
	case htClose:
		cPostMessageW.Call(c.hwnd, wmSysCommand, scClose, 0)
	case actionMenu:
		c.menu()
	}
}

// menu: Windows' window menu (restore, move, size, minimize, maximize, close) at the mouse.
func (c *cadreFenetre) menu() {
	var p pointW
	cGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	m, _, _ := cGetSystemMenu.Call(c.hwnd, 0)
	if m == 0 {
		return
	}
	z := agrandie(c.hwnd)
	gris := func(actif bool) uintptr {
		if actif {
			return 0 // MF_BYCOMMAND | MF_ENABLED
		}
		return 1 // MF_GRAYED
	}
	cEnableMenuItem.Call(m, scRestore, gris(z))
	cEnableMenuItem.Call(m, scMove, gris(!z))
	cEnableMenuItem.Call(m, scSize, gris(!z))
	cEnableMenuItem.Call(m, scMinimize, 0)
	cEnableMenuItem.Call(m, scMaximize, gris(!z))
	cEnableMenuItem.Call(m, scClose, 0)
	cSetMenuDefaultItem.Call(m, scClose, 0)
	cmd, _, _ := cTrackPopupMenu.Call(m, 0x0100|0x0002, entier(p.X), entier(p.Y), 0, c.hwnd, 0) // TPM_RETURNCMD | TPM_RIGHTBUTTON
	if cmd != 0 {
		cPostMessageW.Call(c.hwnd, wmSysCommand, cmd, 0)
	}
}

// calque: the invisible windows' messages.
func (c *cadreFenetre) calque(h, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmNcHitTest:
		p := pointW{int32(int16(lp)), int32(int16(lp >> 16))}
		cScreenToClient.Call(c.hwnd, uintptr(unsafe.Pointer(&p)))
		l := c.largeur()
		return zoneTitre(p.X, p.Y, l, bordHaut(c.hwnd), agrandie(c.hwnd), c.page.boutons(l, c.echelle))
	case wmNcMouseMove:
		s := suiviSouris{drapeaux: 0x2 | 0x10, hwnd: h} // TME_LEAVE | TME_NONCLIENT
		s.cb = uint32(unsafe.Sizeof(s))
		cTrackMouseEvent.Call(uintptr(unsafe.Pointer(&s)))
		code := wp
		if nomBouton(code) == "" {
			code = 0
		}
		c.survolPar = h
		if code != c.survol {
			c.survol = code
			c.peindre()
		}
		return 0
	case wmNcMouseLeave:
		if h == c.survolPar { // not when the mouse already went over the other invisible window
			c.survol, c.appui, c.survolPar = 0, 0, 0
			c.peindre()
		}
		return 0
	case wmNcLButtonDown, wmNcLButtonDblClk:
		if nomBouton(wp) != "" {
			c.appui = wp
			c.peindre()
			return 0
		}
	case wmNcLButtonUp:
		if nomBouton(wp) != "" {
			appui := c.appui
			c.appui = 0
			c.peindre()
			if appui == wp { // released on the button pressed
				c.agir(wp)
			}
			return 0
		}
	case wmNcRButtonDown:
		return 0
	case wmNcRButtonUp:
		if nomBouton(wp) == "" {
			c.menu()
		}
		return 0
	case wmEraseBkgnd:
		return 1
	}
	switch msg {
	case wmNcLButtonDown, wmNcLButtonDblClk, wmNcLButtonUp:
		// the top edge and the rest of the bar: Windows resizes, moves or maximizes the window itself
		r, _, _ := cSendMessageW.Call(c.hwnd, msg, wp, lp)
		return r
	}
	r, _, _ := pDefWindowProcW.Call(h, msg, wp, lp)
	return r
}
