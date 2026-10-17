//go:build windows && 386

package main

import "errors"

// 32-bit Windows: the shell structure is packed differently there; Athanor only ships 64-bit builds.
func mettreALaCorbeille(chemin string) error {
	return errors.New("the recycle bin is only used by the 64-bit application")
}
