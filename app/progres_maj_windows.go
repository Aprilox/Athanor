//go:build windows

package main

// The update's small window: while the new executable puts itself in place (athanor mise-a-jour), a
// window in the middle of the screen says what it does, with a bar, then closes once Athanor is open again.

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	pInvalidateRectMaj = user32.NewProc("InvalidateRect")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pDestroyWindowMaj  = user32.NewProc("DestroyWindow")
	classeProgres, _   = syscall.UTF16PtrFromString("AthanorMiseAJour")
)

type progresMaj struct {
	mu      sync.Mutex
	hwnd    uintptr
	titre   string
	etape   string
	pct     int
	fini    chan struct{}
	ouverte chan struct{}
}

// ouvrirProgresMaj: the window, shown at once (its own thread).
func ouvrirProgresMaj(titre string) *progresMaj {
	p := &progresMaj{titre: titre, fini: make(chan struct{}), ouverte: make(chan struct{})}
	go p.boucle()
	<-p.ouverte
	return p
}

// etapeMaj: what is being done, and how far (percent).
func (p *progresMaj) etapeMaj(texte string, pct int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.etape, p.pct = texte, pct
	h := p.hwnd
	p.mu.Unlock()
	if h != 0 {
		pInvalidateRectMaj.Call(h, 0, 1)
	}
}

func (p *progresMaj) fermer() {
	if p == nil {
		return
	}
	p.mu.Lock()
	h := p.hwnd
	p.hwnd = 0
	p.mu.Unlock()
	if h != 0 {
		cPostMessageW.Call(h, wmClose, 0, 0)
		<-p.fini
	}
}

func (p *progresMaj) boucle() {
	runtime.LockOSThread()
	defer close(p.fini)
	fond, _, _ := pCreateSolidBrush.Call(0x302418) // BGR: the app's dark slate
	barreFond, _, _ := pCreateSolidBrush.Call(0x49392d)
	barre, _, _ := pCreateSolidBrush.Call(0xa5c63f) // the accent
	police, _ := syscall.UTF16PtrFromString("Segoe UI")
	grande, _, _ := pCreateFontW.Call(uintptr(0xFFFFFFEC), 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(police))) // -20 px, semi-bold
	petite, _, _ := pCreateFontW.Call(uintptr(0xFFFFFFF1), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(police))) // -15 px
	dessiner := func(dc uintptr, rc [4]int32, f uintptr, couleur uintptr, s string) {
		t, _ := syscall.UTF16FromString(s)
		pSelectObject.Call(dc, f)
		pSetTextColor.Call(dc, couleur)
		pSetBkMode.Call(dc, 1)
		pDrawTextW.Call(dc, uintptr(unsafe.Pointer(&t[0])), uintptr(len(t)-1), uintptr(unsafe.Pointer(&rc[0])), 0x20|0x4000) // one line, ellipsis
	}
	proc := syscall.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		switch msg {
		case 0x000F: // WM_PAINT
			p.mu.Lock()
			titre, etape, pct := p.titre, p.etape, p.pct
			p.mu.Unlock()
			var ps [72]byte
			dc, _, _ := pBeginPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
			var rc [4]int32
			pGetClientRect.Call(h, uintptr(unsafe.Pointer(&rc[0])))
			pFillRect.Call(dc, uintptr(unsafe.Pointer(&rc[0])), fond)
			dessiner(dc, [4]int32{28, 24, rc[2] - 28, 54}, grande, 0xF3EEE9, titre)
			dessiner(dc, [4]int32{28, 62, rc[2] - 28, 86}, petite, 0xC2B3A3, etape)
			fondBarre := [4]int32{28, 104, rc[2] - 28, 112}
			pFillRect.Call(dc, uintptr(unsafe.Pointer(&fondBarre[0])), barreFond)
			if pct > 0 {
				if pct > 100 {
					pct = 100
				}
				plein := fondBarre
				plein[2] = plein[0] + (fondBarre[2]-fondBarre[0])*int32(pct)/100
				pFillRect.Call(dc, uintptr(unsafe.Pointer(&plein[0])), barre)
			}
			pEndPaint.Call(h, uintptr(unsafe.Pointer(&ps[0])))
			return 0
		case 0x0010: // WM_CLOSE
			pDestroyWindowMaj.Call(h)
			return 0
		case 0x0002: // WM_DESTROY
			pPostQuitMessage.Call(0)
			return 0
		}
		r0, _, _ := pDefWindowProcW.Call(h, msg, wp, lp)
		return r0
	})
	curseur, _, _ := pLoadCursorW.Call(0, 32650) // IDC_APPSTARTING: the arrow with the hourglass
	type wndclassex struct {
		cbSize, style                uint32
		proc                         uintptr
		clsExtra, wndExtra           int32
		instance, icon, cursor, fond uintptr
		menu, classe                 *uint16
		iconSm                       uintptr
	}
	wc := wndclassex{proc: proc, cursor: curseur, fond: fond, classe: classeProgres}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	le, _, _ := pGetSystemMetrics.Call(0) // SM_CXSCREEN
	he, _, _ := pGetSystemMetrics.Call(1)
	l, ht := int32(480), int32(140)
	titre, _ := syscall.UTF16PtrFromString("Athanor")
	h, _, _ := pCreateWindowExW.Call(0x8, uintptr(unsafe.Pointer(classeProgres)), uintptr(unsafe.Pointer(titre)), // WS_EX_TOPMOST
		0x80000000|0x10000000|0x00800000, uintptr((int32(le)-l)/2), uintptr((int32(he)-ht)/2), uintptr(l), uintptr(ht), 0, 0, 0, 0) // WS_POPUP | WS_VISIBLE | WS_BORDER
	p.mu.Lock()
	p.hwnd = h
	p.mu.Unlock()
	close(p.ouverte)
	if h == 0 {
		return
	}
	var msg [48]byte
	for {
		if v, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(v) <= 0 {
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
	}
}
