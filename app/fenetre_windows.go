//go:build windows

package main

// A second launch of Athanor brings the running window back to the front (see instance.go).

import "sync/atomic"

var (
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pShowWindow          = user32.NewProc("ShowWindow")
	pIsIconic            = user32.NewProc("IsIconic")
	pAllowSetForeground  = user32.NewProc("AllowSetForegroundWindow")
)

// fenetreHWND: the application window while it is open (0 otherwise).
var fenetreHWND atomic.Uintptr

// permettrePremierPlan: called by a second launch, which the user just started (so it may give the
// foreground away): the running instance can then bring its window to the front.
func permettrePremierPlan() { pAllowSetForeground.Call(^uintptr(0)) } // ASFW_ANY

// montrerFenetre brings the open application window back (restored if minimized) to the front.
func montrerFenetre() {
	h := fenetreHWND.Load()
	if h == 0 {
		return
	}
	if r, _, _ := pIsIconic.Call(h); r != 0 {
		pShowWindow.Call(h, 9) // SW_RESTORE
	} else {
		pShowWindow.Call(h, 5) // SW_SHOW
	}
	pSetForegroundWindow.Call(h)
}
