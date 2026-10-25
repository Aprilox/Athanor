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
