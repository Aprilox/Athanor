//go:build windows && !386

package main

// The Windows recycle bin (SHFileOperation, FOF_ALLOWUNDO): a deleted project can be restored.

import (
	"errors"
	"syscall"
	"unsafe"
)

var pSHFileOperationW = syscall.NewLazyDLL("shell32.dll").NewProc("SHFileOperationW")

type shFileOp struct { // SHFILEOPSTRUCTW, 56 bytes on amd64
	hwnd                  uintptr
	wFunc                 uint32
	pFrom, pTo            *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

func mettreALaCorbeille(chemin string) error {
	de, err := syscall.UTF16FromString(chemin)
	if err != nil {
		return err
	}
	de = append(de, 0) // a list ends with two zeros
	op := shFileOp{
		wFunc: 3, // FO_DELETE
		pFrom: &de[0],
		// FOF_ALLOWUNDO | FOF_NOCONFIRMATION | FOF_SILENT | FOF_NOERRORUI | FOF_WANTNUKEWARNING
		// (Windows asks before destroying something too large for the recycle bin)
		fFlags: 0x40 | 0x10 | 0x4 | 0x400 | 0x4000,
	}
	if r, _, _ := pSHFileOperationW.Call(uintptr(unsafe.Pointer(&op))); r != 0 {
		return errors.New("Windows refused (a file in use?)")
	}
	if op.fAnyOperationsAborted != 0 {
		return errors.New("cancelled")
	}
	return nil
}
