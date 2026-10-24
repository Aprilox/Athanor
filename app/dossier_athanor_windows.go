//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var pGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

// langueSystemeOS: Windows' display language (French: primary language 0x0C).
func langueSystemeOS() string {
	r, _, _ := pGetUserDefaultUILanguage.Call()
	if r&0x3ff == 0x0c {
		return "fr"
	}
	return "en"
}

// demanderConsentement: a Windows dialog (Yes / No), shown before any window of the application.
func demanderConsentement(titre, texte string) bool {
	t, _ := syscall.UTF16PtrFromString(titre)
	m, _ := syscall.UTF16PtrFromString(texte)
	const mbYesNo, mbIconQuestion, mbTopmost, mbSetForeground, idYes = 0x4, 0x20, 0x40000, 0x10000, 6
	r, _, _ := pMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbYesNo|mbIconQuestion|mbTopmost|mbSetForeground)
	return r == idYes
}

// dossierDonneesApp: %LocalAppData%\Athanor (the window's WebView2 data is there too).
func dossierDonneesApp() string {
	if d := os.Getenv("LocalAppData"); d != "" {
		return filepath.Join(d, "Athanor")
	}
	return filepath.Join(os.TempDir(), "Athanor")
}
