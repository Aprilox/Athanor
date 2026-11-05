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

// partagerConsoleParent: attached to the parent's console. Standard handles that were already given (pipes:
// an MCP server's, a gateway's output read by Claude) stay those: the console only fills in the missing ones.
func partagerConsoleParent() bool {
	nums := [3]int{syscall.STD_INPUT_HANDLE, syscall.STD_OUTPUT_HANDLE, syscall.STD_ERROR_HANDLE}
	var avant [3]syscall.Handle
	for i, n := range nums {
		avant[i], _ = syscall.GetStdHandle(n)
	}
	r, _, _ := pAttachConsole.Call(uintptr(^uint32(0))) // ATTACH_PARENT_PROCESS
	if r == 0 {
		return false
	}
	for i, n := range nums {
		if avant[i] != 0 && avant[i] != syscall.InvalidHandle {
			pSetStdHandle.Call(uintptr(uint32(n)), uintptr(avant[i]))
		}
	}
	return true
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
