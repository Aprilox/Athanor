//go:build !windows

package main

// Outside Windows, the kit already runs natively: no native Windows mode.

import "sync/atomic"

var modeNatif atomic.Bool

func bashNatif() string                       { return "" }
func questionOuiNon(titre, texte string) bool { return true }
