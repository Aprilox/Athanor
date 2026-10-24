package main

import "testing"

func TestGardeSouris(t *testing.T) {
	studio := []rectG{{0, 0, 1920, 1040}} // Roblox Studio maximized on Claude's desktop
	centre := rectG{960, 520, 961, 521}
	navigateur := rectG{0, 0, 1920, 1040} // the user's maximized browser in front
	if !confinementDeClaude(centre, studio, navigateur, true, true) {
		t.Fatal("Studio's lock on the centre kept while the user browses (cursor shown)")
	}
	if confinementDeClaude(centre, studio, navigateur, true, false) {
		t.Fatal("the user's own game (cursor hidden) lost its mouse lock")
	}
	jeuFenetre := rectG{100, 100, 900, 700}
	if confinementDeClaude(rectG{108, 130, 892, 692}, studio, jeuFenetre, true, true) {
		t.Fatal("the confinement of the user's window in front was lifted")
	}
	if !confinementDeClaude(rectG{1000, 100, 1800, 900}, studio, jeuFenetre, true, true) {
		t.Fatal("an area of Studio outside the user's window kept")
	}
	if confinementDeClaude(centre, nil, navigateur, true, true) {
		t.Fatal("nothing on Claude's desktop, yet a confinement was lifted")
	}
	if !sautDeClaude(pointG{100, 100}, pointG{960, 520}, studio) || sautDeClaude(pointG{950, 515}, pointG{960, 520}, studio) || sautDeClaude(pointG{100, 100}, pointG{960, 520}, nil) {
		t.Fatal("jumps misjudged")
	}
}
