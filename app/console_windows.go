package main

// The sub-commands of Athanor.exe (a Windows-subsystem program: no console of its own) share the console of
// the program that started them. The pilot runs under a Git Bash whose console has no window: without
// this, every console program it starts (git, bash, the tools) opened a console window of its own, which
// flashed on the screen. Started from the application (no console to share), nothing changes.

var pAttachConsole = kernel32.NewProc("AttachConsole")

func partagerConsoleParent() bool {
	r, _, _ := pAttachConsole.Call(uintptr(^uint32(0))) // ATTACH_PARENT_PROCESS
	return r != 0
}
