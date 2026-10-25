//go:build darwin

package main

import (
	"syscall"
	"unsafe"
)

const (
	ioctlLireTermios   = syscall.TIOCGETA
	ioctlEcrireTermios = syscall.TIOCSETA
)

func dupliquerSortie(ancien, nouveau int) error { return syscall.Dup2(ancien, nouveau) }

// viderEntree: what is waiting in the terminal's input (typed ahead, the rest of a pasted block) is
// dropped, never taken for the answer to the question asked next.
func viderEntree(fd int) {
	lecture := 1 // FREAD
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCFLUSH, uintptr(unsafe.Pointer(&lecture)))
}
