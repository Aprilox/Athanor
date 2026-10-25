//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32Dossier               = syscall.NewLazyDLL("shell32.dll")
	pSHCreateItemFromParsingName = shell32Dossier.NewProc("SHCreateItemFromParsingName")
	pCoTaskMemFreeDossier        = syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree")
	pCoCreateInstanceDossier     = syscall.NewLazyDLL("ole32.dll").NewProc("CoCreateInstance")

	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidShellItem        = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

// choisirDossierSysteme: Windows' own folder dialog (IFileOpenDialog with FOS_PICKFOLDERS), owned by
// Athanor's window, on a thread of its own (COM, single-threaded apartment).
func choisirDossierSysteme(depart, titre string) (chemin string, ok, dispo bool) {
	type res struct {
		chemin    string
		ok, dispo bool
	}
	fin := make(chan res, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && err != windows.Errno(1) { // S_FALSE: already
			fin <- res{}
			return
		}
		defer windows.CoUninitialize()
		var d *comObj
		if r, _, _ := pCoCreateInstanceDossier.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, 1 /* CLSCTX_INPROC_SERVER */, uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&d))); r != 0 || d == nil {
			fin <- res{}
			return
		}
		defer d.liberer()
		var opts uint32
		d.appel(10, uintptr(unsafe.Pointer(&opts))) // GetOptions
		d.appel(9, uintptr(opts|0x20|0x40|0x800))   // SetOptions: FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST
		if titre != "" {
			if t, err := syscall.UTF16PtrFromString(titre); err == nil {
				d.appel(17, uintptr(unsafe.Pointer(t))) // SetTitle
			}
		}
		if p, err := syscall.UTF16PtrFromString(depart); err == nil && depart != "" {
			var item *comObj
			if r, _, _ := pSHCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item))); r == 0 && item != nil {
				d.appel(12, uintptr(unsafe.Pointer(item))) // SetFolder
				item.liberer()
			}
		}
		montrerFenetre()
		hr := d.appel(3, fenetreHWND.Load()) // Show, owned by Athanor's window
		if hr == 0x800704C7 {                // ERROR_CANCELLED
			fin <- res{dispo: true}
			return
		}
		if hr != 0 {
			fin <- res{}
			return
		}
		var item *comObj
		if d.appel(20, uintptr(unsafe.Pointer(&item))) != 0 || item == nil { // GetResult
			fin <- res{dispo: true}
			return
		}
		defer item.liberer()
		var p *uint16
		if item.appel(5, 0x80058000, uintptr(unsafe.Pointer(&p))) != 0 || p == nil { // GetDisplayName(SIGDN_FILESYSPATH)
			fin <- res{dispo: true}
			return
		}
		s := windows.UTF16PtrToString(p)
		pCoTaskMemFreeDossier.Call(uintptr(unsafe.Pointer(p)))
		fin <- res{chemin: s, ok: s != "", dispo: true}
	}()
	r := <-fin
	return r.chemin, r.ok, r.dispo
}
