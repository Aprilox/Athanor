//go:build windows

package main

// Windows: no such flags; the type of the file is checked after opening (api_pourvous.go).
const (
	oSansLien    = 0
	oNonBloquant = 0
)
