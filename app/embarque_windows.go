//go:build windows

package main

import "errors"

// execRemplacer: Windows has no exec: relancerKit starts the kit's executable instead.
func execRemplacer(string, []string, []string) error { return errors.New("not on Windows") }
