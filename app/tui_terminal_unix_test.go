//go:build linux || darwin

package main

import (
	"testing"
	"time"
)

// Enter sent as CR, then its LF in a read of its own (some terminals over SSH): one Enter, never two (the
// second saved at once the form the first opened). An LF alone, long after, is still an Enter.
func TestTUILFApresCR(t *testing.T) {
	dernierCR.Store(0)
	if g := string(sansLFApresCR([]byte("\r"))); g != "\r" {
		t.Fatalf("the CR: %q", g)
	}
	if g := string(sansLFApresCR([]byte("\n"))); g != "" {
		t.Fatalf("its LF, apart: %q", g)
	}
	if g := string(sansLFApresCR([]byte("\n"))); g != "\n" {
		t.Fatalf("a second LF: %q", g)
	}
	sansLFApresCR([]byte("x\r"))
	dernierCR.Store(time.Now().Add(-2 * time.Second).UnixNano())
	if g := string(sansLFApresCR([]byte("\nab"))); g != "\nab" {
		t.Fatalf("an LF long after: %q", g)
	}
	sansLFApresCR([]byte("\r"))
	if g := string(sansLFApresCR([]byte("\x00q"))); g != "q" {
		t.Fatalf("CR NUL apart: %q", g)
	}
}
