//go:build !(windows && embarque)

package main

// athanorLinuxGz: none outside Windows, nor in a plain Windows build (the WSL mode then uses the athanor
// already in the tool folder).
var athanorLinuxGz []byte
