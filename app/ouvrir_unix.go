//go:build !windows

package main

import "syscall"

// Flags for reading a workshop file Claude may have replaced: never through a link, never blocking on a pipe.
const (
	oSansLien    = syscall.O_NOFOLLOW
	oNonBloquant = syscall.O_NONBLOCK
)
