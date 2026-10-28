package main

// Windows: Blender is Windows' own (outil_passerelle.go); blender_natif.go never runs here.
func (p *passerelle) blenderNatif(args []string) int { return p.blender(args) }
