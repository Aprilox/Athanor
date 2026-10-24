//go:build windows && embarque

package main

import _ "embed"

// athanorLinuxGz: the Linux executable of the WSL mode, built just before this one (same processor),
// gzip-compressed.
//
//go:embed embarque/athanor-linux.gz
var athanorLinuxGz []byte
