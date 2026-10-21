package edge

// Added for Athanor: whether a navigation loaded its page.

import "unsafe"

// Reussie: false when the page could not be loaded (WebView2 then shows its error page). True when
// the runtime does not say.
func (i *ICoreWebView2NavigationCompletedEventArgs) Reussie() bool {
	if i == nil {
		return true
	}
	var ok int32 = 1
	if r, _, _ := i.vtbl.GetIsSuccess.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&ok))); r != 0 {
		return true
	}
	return ok != 0
}
