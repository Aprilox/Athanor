//go:build windows

package main

// Claude's desktop, quiet: while a pilot runs (Athanor.exe --eveil, the application open or not), the
// programs on Claude's desktop are not heard (their mouse: garde_souris_windows.go).
//   - Sound: the audio of the programs with a window on Claude's desktop (and of what they started) is
//     muted, unless the user chose to hear Claude's desktop (Settings › “Claude” desktop) or is looking at
//     it. Windows keeps a program's mute for its next start: Athanor gives the sound back as soon as the
//     mute is no longer wanted, when the program ends (its session is still held then), when the pilot
//     ends, and, for what an Athanor stopped short could not give back, at the next guard or the next start
//     of the application (logs/sourdine.txt). Only what Athanor muted is ever unmuted.
// One at a time in the session (a named object): with several pilots, the next one takes over.
// (An invisible window taking the focus on Claude's desktop, 2.27 to 2.30, was refused by Windows every
// time: a desktop not on the screen has no window in front. The mouse guard alone gives the mouse back.)

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32Calme             = syscall.NewLazyDLL("ole32.dll")
	pCoCreateInstance      = ole32Calme.NewProc("CoCreateInstance")
	clsidEnumerateur, _    = windows.GUIDFromString("{BCDE0395-E52F-467C-8E3D-C4579291692E}") // MMDeviceEnumerator
	iidEnumerateur, _      = windows.GUIDFromString("{A95664D2-9614-4F35-A746-DE8DB63617E6}") // IMMDeviceEnumerator
	iidGestionSessions2, _ = windows.GUIDFromString("{77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F}") // IAudioSessionManager2
	iidControleSession2, _ = windows.GUIDFromString("{BFB7FF88-7239-4FC9-8FA2-07C950BE9C6D}") // IAudioSessionControl2
	iidVolumeSimple, _     = windows.GUIDFromString("{87CE5498-68D6-44E5-9215-6DA47EF883D8}") // ISimpleAudioVolume
)

// unSeulDansSession waits until no other Athanor of the session holds the named object (another pilot's),
// then holds it until the returned function is called; false when fin is closed first.
func unSeulDansSession(nom string, fin <-chan struct{}) (func(), bool) {
	n, _ := syscall.UTF16PtrFromString(`Local\` + nom)
	for {
		m, _, e := pCreateMutexGarde.Call(0, 1, uintptr(unsafe.Pointer(n)))
		if m != 0 && e != syscall.Errno(183) { // ERROR_ALREADY_EXISTS
			return func() { syscall.CloseHandle(syscall.Handle(m)) }, true
		}
		if m != 0 {
			syscall.CloseHandle(syscall.Handle(m))
		}
		select {
		case <-fin:
			return nil, false
		case <-time.After(10 * time.Second):
		}
	}
}

// existeDansSession: another Athanor holds the named object.
func existeDansSession(nom string) bool {
	n, _ := syscall.UTF16PtrFromString(`Local\` + nom)
	m, _, e := pCreateMutexGarde.Call(0, 0, uintptr(unsafe.Pointer(n)))
	if m != 0 {
		syscall.CloseHandle(syscall.Handle(m))
	}
	return e == syscall.Errno(183)
}

// ---------------------------------------------------------------------------------------- sound

// comObj: a COM object, seen through its table of methods (0 QueryInterface, 1 AddRef, 2 Release).
type comObj struct{ vt *[32]uintptr }

func (o *comObj) appel(i int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vt[i], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObj) liberer() {
	if o != nil {
		o.appel(2)
	}
}

func (o *comObj) interfaceCom(iid *windows.GUID) *comObj {
	var r *comObj
	if o.appel(0, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&r))) != 0 {
		return nil
	}
	return r
}

// sessionAudio: the sound of one program on one output.
type sessionAudio struct {
	cle string // the session's instance (output and program)
	pid uint32
	exe string // lower case, full path
	ctl *comObj
	vol *comObj
}

func (s sessionAudio) liberer() {
	s.vol.liberer()
	s.ctl.liberer()
}

func (s sessionAudio) muette() bool {
	var m int32
	return s.vol.appel(6, uintptr(unsafe.Pointer(&m))) == 0 && m != 0 // GetMute
}

func (s sessionAudio) rendreMuette(oui bool) bool {
	b := uintptr(0)
	if oui {
		b = 1
	}
	return s.vol.appel(5, b, 0) == 0 // SetMute
}

// sessionsAudio: the programs' sounds on every active output (the caller frees them). The thread has COM.
func sessionsAudio() []sessionAudio {
	var enum *comObj
	if r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidEnumerateur)), 0, 0x17, uintptr(unsafe.Pointer(&iidEnumerateur)), uintptr(unsafe.Pointer(&enum))); r != 0 || enum == nil {
		return nil
	}
	defer enum.liberer()
	var coll *comObj
	if enum.appel(3, 0, 1, uintptr(unsafe.Pointer(&coll))) != 0 || coll == nil { // EnumAudioEndpoints(eRender, DEVICE_STATE_ACTIVE)
		return nil
	}
	defer coll.liberer()
	var n uint32
	coll.appel(3, uintptr(unsafe.Pointer(&n)))
	var r []sessionAudio
	for i := uint32(0); i < n && i < 32; i++ {
		var dev *comObj
		if coll.appel(4, uintptr(i), uintptr(unsafe.Pointer(&dev))) != 0 || dev == nil {
			continue
		}
		var gest *comObj
		if dev.appel(3, uintptr(unsafe.Pointer(&iidGestionSessions2)), 0x17, 0, uintptr(unsafe.Pointer(&gest))) == 0 && gest != nil { // Activate
			var liste *comObj
			if gest.appel(5, uintptr(unsafe.Pointer(&liste))) == 0 && liste != nil { // GetSessionEnumerator
				var nb int32
				liste.appel(3, uintptr(unsafe.Pointer(&nb)))
				for j := int32(0); j < nb && j < 256; j++ {
					var ctl *comObj
					if liste.appel(4, uintptr(j), uintptr(unsafe.Pointer(&ctl))) != 0 || ctl == nil {
						continue
					}
					if s, ok := lireSession(ctl, int(i)); ok {
						r = append(r, s)
					} else {
						ctl.liberer()
					}
				}
				liste.liberer()
			}
			gest.liberer()
		}
		dev.liberer()
	}
	return r
}

func lireSession(ctl *comObj, sortie int) (sessionAudio, bool) {
	c2 := ctl.interfaceCom(&iidControleSession2)
	if c2 == nil {
		return sessionAudio{}, false
	}
	defer c2.liberer()
	if c2.appel(15) == 0 { // IsSystemSoundsSession: S_OK for Windows' own sounds
		return sessionAudio{}, false
	}
	var pid uint32
	if c2.appel(14, uintptr(unsafe.Pointer(&pid))) != 0 || pid == 0 { // GetProcessId (one process only)
		return sessionAudio{}, false
	}
	cle := fmt.Sprintf("%d/%d", sortie, pid) // when Windows gives no instance (one session per program and output)
	var id *uint16
	if c2.appel(13, uintptr(unsafe.Pointer(&id))) == 0 && id != nil { // GetSessionInstanceIdentifier
		cle = windows.UTF16PtrToString(id)
		windows.CoTaskMemFree(unsafe.Pointer(id))
	}
	vol := ctl.interfaceCom(&iidVolumeSimple)
	if vol == nil {
		return sessionAudio{}, false
	}
	return sessionAudio{cle: cle, pid: pid, exe: strings.ToLower(exeDuPid(pid)), ctl: ctl, vol: vol}, true
}

// pidsBureauClaude: the processes with a window on Claude's desktop, and those they started (seen from a
// thread of the user's desktop).
func pidsBureauClaude() map[uint32]bool {
	h := ouvrirBureauIsoleExistant()
	if h == 0 {
		return nil
	}
	defer pCloseDesktop.Call(h)
	// a process with a window on the user's desktop too is the user's (Explorer opening a folder for Claude
	// is the user's Explorer, and what the user starts descends from it): it and what it started stay heard
	utilisateur := map[uint32]bool{}
	for _, f := range listerFenetres() { // this thread is on the user's desktop
		utilisateur[f.pid] = true
	}
	depart := map[uint32]bool{}
	for _, f := range fenetresDuBureau(h) {
		b := strings.ToLower(filepath.Base(f.exe))
		if b == "athanor.exe" || b == "explorer.exe" || utilisateur[f.pid] {
			continue
		}
		depart[f.pid] = true
	}
	if len(depart) == 0 {
		return depart
	}
	parents, noms := processus()
	for p := range parents {
		if utilisateur[p] || noms[p] == "explorer.exe" {
			delete(parents, p) // never reached through a program of Claude's
		}
	}
	return descendants(depart, parents)
}

// processus: each running process's parent and executable name (lower case).
func processus() (map[uint32]uint32, map[uint32]string) {
	parents, noms := map[uint32]uint32{}, map[uint32]string{}
	s, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return parents, noms
	}
	defer windows.CloseHandle(s)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(s, &e); err == nil; err = windows.Process32Next(s, &e) {
		parents[e.ProcessID] = e.ParentProcessID
		noms[e.ProcessID] = strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
	}
	return parents, noms
}

// silencerBureau: the sound of Claude's desktop, until fin is closed (then everything is given back).
func silencerBureau(kit string, fin <-chan struct{}) {
	liberer, ok := unSeulDansSession("AthanorSilence", fin)
	if !ok {
		return
	}
	defer liberer()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
		return
	}
	defer windows.CoUninitialize()
	tenues := map[string]sessionAudio{} // muted by this Athanor
	oubliees := lireSourdine(kit)       // muted by an earlier one that could not give the sound back
	ecrit := ""
	rendre := func(s sessionAudio) {
		s.rendreMuette(false)
		s.liberer()
	}
	defer func() {
		for _, s := range tenues {
			rendre(s)
		}
		ecrireSourdine(kit, oubliees)
	}()
	entendre, lu := false, time.Time{}
	for {
		if time.Since(lu) > 3*time.Second {
			entendre, lu = lireConfig().EntendreBureauClaude, time.Now()
		}
		regarde := nomBureauActif() == nomBureauIsole
		surClaude := pidsBureauClaude()
		for _, s := range sessionsAudio() {
			veut := sourdineVoulue(entendre, regarde, surClaude[s.pid])
			if t, deja := tenues[s.cle]; deja {
				if !veut {
					rendre(t)
					delete(tenues, s.cle)
				}
				s.liberer()
				continue
			}
			if veut && !s.muette() && s.rendreMuette(true) {
				tenues[s.cle] = s
				continue
			}
			if veut && oubliees[s.exe] && s.muette() { // still muted by an earlier Athanor: this one takes it over
				tenues[s.cle] = s
				continue
			}
			if !surClaude[s.pid] && s.exe != "" && oubliees[s.exe] && s.muette() && s.rendreMuette(false) {
				journalSouris(kit, "sound given back to "+filepath.Base(s.exe)+" (muted on Claude's desktop by an Athanor stopped since)")
				delete(oubliees, s.exe)
			}
			s.liberer()
		}
		// a program that ended: its sound is given back while its session is still held, so that Windows
		// does not keep it muted for its next start
		for k, t := range tenues {
			if !processusVivantPilote(int(t.pid)) {
				rendre(t)
				delete(tenues, k)
			}
		}
		exes := map[string]bool{}
		for e := range oubliees {
			exes[e] = true
		}
		for _, t := range tenues {
			if t.exe != "" {
				exes[t.exe] = true
			}
		}
		if v := fmt.Sprint(exes); v != ecrit {
			ecrireSourdine(kit, exes)
			ecrit = v
		}
		select {
		case <-fin:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// rendreSonOublie (the application's start): gives the sound back to the programs an Athanor stopped short
// left muted, unless a pilot's guard is at work (it does it).
func rendreSonOublie(kit string) {
	oubliees := lireSourdine(kit)
	if len(oubliees) == 0 || existeDansSession("AthanorSilence") {
		return
	}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil {
			return
		}
		defer windows.CoUninitialize()
		surClaude := pidsBureauClaude()
		for _, s := range sessionsAudio() {
			if !surClaude[s.pid] && oubliees[s.exe] && s.muette() && s.rendreMuette(false) {
				journalSouris(kit, "sound given back to "+filepath.Base(s.exe)+" (muted on Claude's desktop by an Athanor stopped since)")
			}
			s.liberer()
		}
		// what runs nowhere now gets its sound back at its next start, when a guard sees it
		ecrireSourdine(kit, oubliees)
	}()
}
