//go:build darwin

package main

import "syscall"

const (
	ioctlLireTermios   = syscall.TIOCGETA
	ioctlEcrireTermios = syscall.TIOCSETA
)

func dupliquerSortie(ancien, nouveau int) error { return syscall.Dup2(ancien, nouveau) }
