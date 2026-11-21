//go:build windows

package main

// Which programs hold a folder's files open (Windows' Restart Manager, as Explorer asks before a delete), so a
// refused recycle bin names them, and the owner can have Athanor stop them.

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"unsafe"
)

var (
	rstrtmgr             = syscall.NewLazyDLL("rstrtmgr.dll")
	pRmStartSession      = rstrtmgr.NewProc("RmStartSession")
	pRmEndSession        = rstrtmgr.NewProc("RmEndSession")
	pRmRegisterResources = rstrtmgr.NewProc("RmRegisterResources")
	pRmGetList           = rstrtmgr.NewProc("RmGetList")
)

type rmProcessInfo struct { // RM_PROCESS_INFO
	PID          uint32
	Debut        syscall.Filetime
	Nom          [256]uint16
	Service      [64]uint16
	Type         uint32 // RM_APP_TYPE: 3 a service, 4 Explorer, 5 a console program, 1000 critical
	Statut       uint32
	Session      uint32
	Redemarrable int32
}

// fichiersARegarder: the folder's files, node_modules and .git apart (up to 4000), then what a program maps from
// them (.exe, .node, .dll: a dev server's own engine).
func fichiersARegarder(dossier string) []string {
	var l, lourds []string
	_ = filepath.WalkDir(dossier, func(c string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		lourd := strings.Contains(c, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) || strings.Contains(c, string(filepath.Separator)+".git"+string(filepath.Separator))
		switch {
		case d.IsDir() || d.Type()&fs.ModeSymlink != 0:
		case !lourd && len(l) < 4000:
			l = append(l, c)
		case lourd && len(lourds) < 1000 && slices.Contains([]string{".exe", ".node", ".dll"}, strings.ToLower(filepath.Ext(c))):
			lourds = append(lourds, c)
		}
		return nil
	})
	return append(l, lourds...)
}

func programmesDuDossier(dossier string) []ProgrammeBloquant {
	fichiers := fichiersARegarder(dossier)
	if len(fichiers) == 0 {
		return nil
	}
	var session uint32
	var cle [33]uint16
	if r, _, _ := pRmStartSession.Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&cle[0]))); r != 0 {
		return nil
	}
	defer pRmEndSession.Call(uintptr(session))
	noms := make([]*uint16, 0, len(fichiers))
	for _, f := range fichiers {
		if p, err := syscall.UTF16PtrFromString(f); err == nil {
			noms = append(noms, p)
		}
	}
	r, _, _ := pRmRegisterResources.Call(uintptr(session), uintptr(len(noms)), uintptr(unsafe.Pointer(&noms[0])), 0, 0, 0, 0)
	runtime.KeepAlive(noms)
	if r != 0 {
		return nil
	}
	var besoin, n, raisons uint32
	var infos []rmProcessInfo
	for essai := 0; essai < 3; essai++ {
		var p uintptr
		if n > 0 {
			infos = make([]rmProcessInfo, n)
			p = uintptr(unsafe.Pointer(&infos[0]))
		}
		r, _, _ = pRmGetList.Call(uintptr(session), uintptr(unsafe.Pointer(&besoin)), uintptr(unsafe.Pointer(&n)), p, uintptr(unsafe.Pointer(&raisons)))
		if r != 234 { // ERROR_MORE_DATA: asked again with room for every one
			break
		}
		n = besoin
	}
	if r != 0 {
		return nil
	}
	var l []ProgrammeBloquant
	for _, x := range infos[:n] {
		if int(x.PID) == os.Getpid() || int(x.PID) == os.Getppid() {
			continue // Athanor itself (or what launched it) is never shown nor stopped
		}
		l = append(l, ProgrammeBloquant{PID: int(x.PID), Nom: syscall.UTF16ToString(x.Nom[:]), Arretable: x.Type != 3 && x.Type != 4 && x.Type != 1000})
	}
	return l
}
