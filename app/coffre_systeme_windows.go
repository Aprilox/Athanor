package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// entropieDPAPI binds the DPAPI blobs to the vault: another program of the same user
// cannot open them by calling CryptUnprotectData without this value.
var entropieDPAPI = []byte("athanor-coffre:code:v1")

// dpapi protects the code key with the user's DPAPI scope (no machine scope, no prompt).
type dpapi struct{}

// enveloppeSystemeLocale returns the keystore of this system, or nil when there is none.
func enveloppeSystemeLocale() enveloppeSysteme { return dpapi{} }

func blobDe(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

// copierBlob copies DPAPI's output then frees it.
func copierBlob(out *windows.DataBlob) []byte {
	if out.Data == nil {
		return nil
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...)
}

func (dpapi) Proteger(donnees []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blobDe(donnees), nil, blobDe(entropieDPAPI), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return copierBlob(&out), nil
}

func (dpapi) Ouvrir(protege []byte) ([]byte, error) {
	if len(protege) == 0 {
		return nil, errors.New("empty DPAPI blob")
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blobDe(protege), nil, blobDe(entropieDPAPI), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return copierBlob(&out), nil
}
