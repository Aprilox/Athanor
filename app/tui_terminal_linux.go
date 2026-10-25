//go:build linux

package main

import "syscall"

const (
	ioctlLireTermios   = syscall.TCGETS
	ioctlEcrireTermios = syscall.TCSETS
)

// dupliquerSortie: the file descriptor nouveau becomes a copy of ancien (Linux has dup3 only on some
// processors).
func dupliquerSortie(ancien, nouveau int) error { return syscall.Dup3(ancien, nouveau, 0) }

// viderEntree: what is waiting in the terminal's input (typed ahead, the rest of a pasted block) is
// dropped, never taken for the answer to the question asked next.
func viderEntree(fd int) {
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), 0x540B, 0) // TCFLSH, TCIFLUSH (x86 and ARM)
}
