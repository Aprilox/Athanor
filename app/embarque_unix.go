//go:build !windows

package main

import "syscall"

// execRemplacer: this process becomes exe (same pid, same terminal).
func execRemplacer(exe string, argv, env []string) error { return syscall.Exec(exe, argv, env) }
