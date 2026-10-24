package main

// The mouse guard's decisions (garde_souris_windows.go), apart from Windows to be tested everywhere.

type pointG struct{ X, Y int32 }
type rectG struct{ G, H, D, B int32 } // left, top, right, bottom

func (r rectG) contient(p pointG) bool { return p.X >= r.G && p.X < r.D && p.Y >= r.H && p.Y < r.B }
func (r rectG) dans(o rectG) bool      { return r.G >= o.G && r.H >= o.H && r.D <= o.D && r.B <= o.B }

// confinementDeClaude: the mouse is confined (clip) by a window of Claude's desktop, not by the user's
// own program: a lock on one point (LockCenter) is the user's only when their cursor is hidden (their
// game); a larger area is the user's when their window in front holds it.
func confinementDeClaude(clip rectG, claude []rectG, devant rectG, devantOk, curseurVisible bool) bool {
	deClaude := false
	for _, r := range claude {
		if clip.dans(r) {
			deClaude = true
		}
	}
	if !deClaude {
		return false
	}
	petit := clip.D-clip.G <= 4 && clip.B-clip.H <= 4
	aLUtilisateur := (petit && !curseurVisible) || (!petit && devantOk && clip.dans(devant))
	return !aLUtilisateur
}

// sautDeClaude: a jump made by no hand (checked by the caller) that lands in a window of Claude's
// desktop, farther than a hand's tremor.
func sautDeClaude(avant, apres pointG, claude []rectG) bool {
	dx, dy := apres.X-avant.X, apres.Y-avant.Y
	if dx*dx+dy*dy <= 40*40 {
		return false
	}
	for _, r := range claude {
		if r.contient(apres) {
			return true
		}
	}
	return false
}
