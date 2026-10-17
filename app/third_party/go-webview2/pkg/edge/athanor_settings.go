package edge

// Added for Athanor: settings that make the window behave like an application rather than a browser.
// Those of newer WebView2 runtimes are applied only when the runtime implements them (QueryInterface),
// so an older runtime never gets a call outside its vtable.

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidSettings3, _ = windows.GUIDFromString("{fdb5ab74-af33-4854-84f0-0a631deb5eba}") // ICoreWebView2Settings3
	iidSettings4, _ = windows.GUIDFromString("{cb56846c-4168-4d53-b04f-03b6d6796ff2}") // ICoreWebView2Settings4
)

// implemente reports whether the settings object implements the interface iid.
func (i *ICoreWebViewSettings) implemente(iid *windows.GUID) bool {
	var autre uintptr
	r, _, _ := i.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&autre)))
	if r != 0 || autre == 0 {
		return false
	}
	(*ICoreWebViewSettings)(unsafe.Pointer(autre)).vtbl.Release.Call(autre)
	return true
}

func (i *ICoreWebViewSettings) mettre(p ComProc, actif bool) {
	_, _, _ = p.Call(uintptr(unsafe.Pointer(i)), uintptr(boolToInt(actif)))
}

// CommeUneApp: no link preview at the bottom of the window, no browser shortcuts (find, print, reload,
// zoom; copy and paste stay), no form autofill or password saving.
func (i *ICoreWebViewSettings) CommeUneApp() {
	i.mettre(i.vtbl.PutIsStatusBarEnabled, false)
	if i.implemente(&iidSettings3) {
		i.mettre(i.vtbl.PutAreBrowserAcceleratorKeysEnabled, false)
	}
	if i.implemente(&iidSettings4) {
		i.mettre(i.vtbl.PutIsPasswordAutosaveEnabled, false)
		i.mettre(i.vtbl.PutIsGeneralAutofillEnabled, false)
	}
}
