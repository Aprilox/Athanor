package main

// The sub-commands of Athanor.exe (a Windows-subsystem program: no console of its own) share the console of
// the program that started them. The pilot runs under a Git Bash whose console has no window: without
// this, every console program it starts (git, bash, the tools) opened a console window of its own, which
// flashed on the screen. Started from the application (no console to share), nothing changes.

import (
	"os"
	"syscall"
	"unsafe"
)

var pAttachConsole = kernel32.NewProc("AttachConsole")

func partagerConsoleParent() bool {
	r, _, _ := pAttachConsole.Call(uintptr(^uint32(0))) // ATTACH_PARENT_PROCESS
	return r != 0
}

var (
	pAllocConsole     = kernel32.NewProc("AllocConsole")
	pSetConsoleTitleW = kernel32.NewProc("SetConsoleTitleW")
	pSetStdHandle     = kernel32.NewProc("SetStdHandle")
)

// ouvrirConsole: a console window of its own for an interactive step (athanor --fenetre TITLE …): its
// questions and answers go there, and so do those of the programs it starts.
func ouvrirConsole(titre string) {
	pAllocConsole.Call()
	if t, err := syscall.UTF16PtrFromString(titre); err == nil {
		pSetConsoleTitleW.Call(uintptr(unsafe.Pointer(t)))
	}
	const stdIn, stdOut, stdErr = ^uintptr(9), ^uintptr(10), ^uintptr(11) // -10, -11, -12
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = f, f
		pSetStdHandle.Call(stdOut, f.Fd())
		pSetStdHandle.Call(stdErr, f.Fd())
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
		pSetStdHandle.Call(stdIn, f.Fd())
	}
}
