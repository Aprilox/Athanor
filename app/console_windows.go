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
	return garderHandles(func() bool {
		r, _, _ := pAttachConsole.Call(uintptr(^uint32(0))) // ATTACH_PARENT_PROCESS
		return r != 0
	})
}

// garderHandles: runs attacher (a call that gives the process a console); the standard handles already
// given before it stay in place.
func garderHandles(attacher func() bool) bool {
	nums := [3]int{syscall.STD_INPUT_HANDLE, syscall.STD_OUTPUT_HANDLE, syscall.STD_ERROR_HANDLE}
	var avant [3]syscall.Handle
	for i, n := range nums {
		avant[i], _ = syscall.GetStdHandle(n)
	}
	if !attacher() {
		return false
	}
	for i, n := range nums {
		if avant[i] != 0 && avant[i] != syscall.InvalidHandle {
			pSetStdHandle.Call(uintptr(uint32(n)), uintptr(avant[i]))
		}
	}
	return true
}

var pGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")

// consoleCachee: a hidden console of its own for a pilot started without one (by the application). The
// console programs it starts share it; each used to get a console of its own, which Windows 11 opens in
// a Windows Terminal window on the user's desktop, taking the focus. CREATE_NEW_CONSOLE at launch gives
// no console to a Windows-subsystem program such as Athanor.exe, hence AllocConsole (the process was
// started with SW_HIDE, so the console is created hidden). montrer: the "Show the pilot's terminal"
// option, a minimised window that never takes the focus, where the pilot's output is copied.
func consoleCachee(montrer bool) bool {
	if !garderHandles(func() bool { r, _, _ := pAllocConsole.Call(); return r != 0 }) {
		return false
	}
	h, _, _ := pGetConsoleWindow.Call()
	if h != 0 {
		pShowWindow.Call(h, swConsolePilote(montrer))
	}
	if montrer {
		if t, err := syscall.UTF16PtrFromString("Athanor — pilot"); err == nil {
			pSetConsoleTitleW.Call(uintptr(unsafe.Pointer(t)))
		}
		if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
			consoleMontree = f
		}
	}
	return true
}

// swConsolePilote: the show state of the pilot's console: SW_SHOWMINNOACTIVE when shown, else SW_HIDE.
func swConsolePilote(montrer bool) uintptr {
	if montrer {
		return 7
	}
	return 0
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
