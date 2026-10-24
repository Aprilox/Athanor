//go:build windows

package main

// The mouse guard: the programs Claude drives on its own desktop (Roblox Studio during a play test,
// Unreal's play in editor, a game under test) may lock the mouse for their game: they confine it
// (ClipCursor) or bring it back to their centre (SetCursorPos) every frame, and the cursor belongs to the
// whole session, not to one desktop. Seen from the user's desktop, the mouse jumps to the middle of the
// screen while no window opened. While a pilot runs (Athanor.exe --eveil, app open or not), the guard
// gives the mouse back to the user:
//   - a confinement inside a window of Claude's desktop is lifted, unless it is the user's (a point while
//     the user's cursor is hidden, an area inside the user's window in front: a game of the user's);
//   - a jump made by no hand (no input since the last look) into a window of Claude's desktop, while the
//     user's cursor is shown (a game of the user's hides it when it locks the mouse), is undone.
// Each time, the focus is taken on Claude's desktop (bureau_calme_windows.go): the program then lets the
// mouse go by itself. Nothing is done while the user looks at Claude's desktop. Each correction is written to
// logs/souris.log (what was in front, what runs on Claude's desktop), at most once a minute per kind, and
// so is a jump by no hand elsewhere (only written down: what moved the mouse can then be found).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

var (
	pGetCursorPosGarde    = user32.NewProc("GetCursorPos")
	pSetCursorPosGarde    = user32.NewProc("SetCursorPos")
	pGetClipCursorGarde   = user32.NewProc("GetClipCursor")
	pClipCursorGarde      = user32.NewProc("ClipCursor")
	pGetLastInputInfo     = user32.NewProc("GetLastInputInfo")
	pGetForegroundWindowG = user32.NewProc("GetForegroundWindow")
	pGetWindowRectGarde   = user32.NewProc("GetWindowRect")
	pGetCursorInfoGarde   = user32.NewProc("GetCursorInfo")
	pIsIconicGarde        = user32.NewProc("IsIconic")
	pCreateMutexGarde     = kernel32.NewProc("CreateMutexW")
)

func positionCurseur() (pointG, bool) {
	var p pointG
	r, _, _ := pGetCursorPosGarde.Call(uintptr(unsafe.Pointer(&p)))
	return p, r != 0
}

func dernierGeste() uint32 {
	var li struct{ Taille, Temps uint32 }
	li.Taille = 8
	pGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	return li.Temps
}

func curseurAffiche() bool {
	var ci struct {
		Taille, Drapeaux uint32
		Curseur          uintptr
		Pos              pointG
	}
	ci.Taille = uint32(unsafe.Sizeof(ci))
	if r, _, _ := pGetCursorInfoGarde.Call(uintptr(unsafe.Pointer(&ci))); r == 0 {
		return true
	}
	return ci.Drapeaux&1 != 0 // CURSOR_SHOWING
}

func rectFenetre(h uintptr) (rectG, bool) {
	var r rectG
	ok, _, _ := pGetWindowRectGarde.Call(h, uintptr(unsafe.Pointer(&r)))
	return r, ok != 0 && r.D > r.G && r.B > r.H
}

func ecranVirtuel() rectG {
	x, _, _ := pGetSystemMetrics.Call(76) // SM_XVIRTUALSCREEN
	y, _, _ := pGetSystemMetrics.Call(77)
	l, _, _ := pGetSystemMetrics.Call(78)
	h, _, _ := pGetSystemMetrics.Call(79)
	return rectG{int32(x), int32(y), int32(x) + int32(l), int32(y) + int32(h)}
}

// fenetresClaude: the windows open on Claude's desktop (their rectangles and programs).
func fenetresClaude() ([]rectG, []string) {
	h := ouvrirBureauIsoleExistant()
	if h == 0 {
		return nil, nil
	}
	defer pCloseDesktop.Call(h)
	var rects []rectG
	var progs []string
	for _, f := range fenetresDuBureau(h) {
		if v, _, _ := pIsIconicGarde.Call(f.hwnd); v != 0 {
			continue
		}
		if r, ok := rectFenetre(f.hwnd); ok {
			rects = append(rects, r)
			progs = append(progs, filepath.Base(f.exe))
		}
	}
	return rects, progs
}

// journalSouris: one line of logs/souris.log (kept under 256 KB).
func journalSouris(kit, ligne string) {
	if kit == "" {
		return
	}
	f := filepath.Join(kit, "logs", "souris.log")
	if st, err := os.Stat(f); err == nil && st.Size() > 256<<10 {
		_ = os.Rename(f, f+".1")
	}
	o, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer o.Close()
	fmt.Fprintf(o, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), ligne)
}

// garderSouris: the guard, until fin is closed; one guard at a time in the session (a named object).
func garderSouris(kit string, fin <-chan struct{}) {
	// another pilot's guard at work: this one waits, and takes over when that pilot ends
	liberer, ok := unSeulDansSession("AthanorGardeSouris", fin)
	if !ok {
		return
	}
	defer liberer()
	runtime.LockOSThread()
	var (
		avant, _     = positionCurseur()
		gesteAvant   = dernierGeste()
		rects        []rectG
		progs        []string
		relu         time.Time
		dernierJourn = map[string]time.Time{}
	)
	noter := func(genre, detail string) {
		if time.Since(dernierJourn[genre]) < time.Minute {
			return
		}
		dernierJourn[genre] = time.Now()
		premier := ""
		if fg, _, _ := pGetForegroundWindowG.Call(); fg != 0 {
			var pid uint32
			pGetWindowThreadProcessId.Call(fg, uintptr(unsafe.Pointer(&pid)))
			premier = filepath.Base(exeDuPid(pid))
		}
		journalSouris(kit, fmt.Sprintf("%s: %s (in front: %s; on Claude's desktop: %s)", genre, detail, premier, strings.Join(progs, ", ")))
	}
	for {
		select {
		case <-fin:
			return
		case <-time.After(20 * time.Millisecond):
		}
		if nomBureauActif() != "Default" { // the user looks at Claude's desktop, or a UAC prompt is up
			time.Sleep(400 * time.Millisecond)
			avant, _ = positionCurseur()
			gesteAvant = dernierGeste()
			continue
		}
		if time.Since(relu) > time.Second {
			rects, progs = fenetresClaude()
			relu = time.Now()
		}
		pos, ok := positionCurseur()
		geste := dernierGeste()
		if !ok {
			avant, gesteAvant = pos, geste
			continue
		}
		var devant rectG
		devantOk := false
		if fg, _, _ := pGetForegroundWindowG.Call(); fg != 0 {
			devant, devantOk = rectFenetre(fg)
		}
		// a confinement that belongs to a window of Claude's desktop
		var clip rectG
		if r, _, _ := pGetClipCursorGarde.Call(uintptr(unsafe.Pointer(&clip))); r != 0 && clip != ecranVirtuel() {
			if confinementDeClaude(clip, rects, devant, devantOk, curseurAffiche()) {
				pClipCursorGarde.Call(0)
				demanderFocusClaude() // the program in front on Claude's desktop then lets the mouse go
				if pos != avant && geste == gesteAvant {
					pSetCursorPosGarde.Call(uintptr(avant.X), uintptr(avant.Y))
					pos = avant
				}
				noter("confinement lifted", fmt.Sprintf("%d,%d-%d,%d", clip.G, clip.H, clip.D, clip.B))
			}
		}
		// a jump by no hand into a window of Claude's desktop, the user's cursor shown
		if pos != avant && geste == gesteAvant && curseurAffiche() {
			dx, dy := pos.X-avant.X, pos.Y-avant.Y
			switch {
			case sautDeClaude(avant, pos, rects):
				pSetCursorPosGarde.Call(uintptr(avant.X), uintptr(avant.Y))
				demanderFocusClaude()
				noter("jump undone", fmt.Sprintf("%d,%d → %d,%d", avant.X, avant.Y, pos.X, pos.Y))
				pos = avant
			case dx*dx+dy*dy > 200*200:
				// outside Claude's windows: only written down (to find what moves the mouse)
				noter("jump seen", fmt.Sprintf("%d,%d → %d,%d", avant.X, avant.Y, pos.X, pos.Y))
			}
		}
		avant, gesteAvant = pos, geste
	}
}
