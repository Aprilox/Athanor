//go:build !windows

package main

import "fmt"
import "os"

// progresMaj: outside Windows, the update's steps go to the terminal it was started from (if any).
type progresMaj struct{}

func ouvrirProgresMaj(titre string) *progresMaj { fmt.Fprintln(os.Stderr, titre); return &progresMaj{} }

func (p *progresMaj) etapeMaj(texte string, pct int) {
	fmt.Fprintf(os.Stderr, "  %s (%d %%)\n", texte, pct)
}

func (p *progresMaj) fermer() {}
