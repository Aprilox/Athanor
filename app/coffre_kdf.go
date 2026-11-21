package main

// Athanor's password: stored as a slow PBKDF2 hash (standard library, no dependency), so that a copied
// config cannot be brute-forced quickly. The vault derives its own key from the same password with
// another salt (coffre.go).

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const (
	prefixeMdp = "pbkdf2-sha256"
	iterMdp    = 600_000 // OWASP 2023 figure for PBKDF2-HMAC-SHA256
)

// deriverMdp: PBKDF2-HMAC-SHA256, 32 bytes.
func deriverMdp(mdp string, sel []byte, iter int) []byte {
	k, err := pbkdf2.Key(sha256.New, mdp, sel, iter, 32)
	if err != nil { // only on FIPS-140 constraints, never with these parameters
		panic(err)
	}
	return k
}

// hacherMotDePasse: "pbkdf2-sha256$<iterations>$<salt hex>$<hash hex>", with a fresh salt.
func hacherMotDePasse(mdp string) string {
	sel := make([]byte, 16)
	_, _ = rand.Read(sel)
	return prefixeMdp + "$" + strconv.Itoa(iterMdp) + "$" + hex.EncodeToString(sel) + "$" + hex.EncodeToString(deriverMdp(mdp, sel, iterMdp))
}

// ancienHashMdp: the format of versions before the hub, sha256(salt:password), kept only to accept
// it once and replace it.
func ancienHashMdp(sel, mdp string) string {
	h := sha256.Sum256([]byte(sel + ":" + mdp))
	return hex.EncodeToString(h[:])
}

// verifierMotDePasse checks mdp against the stored hash (sel: the config's salt, used only by the old
// format). aRemplacer: right password stored in the old format, to be rewritten with hacherMotDePasse.
func verifierMotDePasse(sel, mdp, stocke string) (ok, aRemplacer bool) {
	if !strings.HasPrefix(stocke, prefixeMdp+"$") {
		ok = stocke != "" && egal(ancienHashMdp(sel, mdp), stocke)
		return ok, ok
	}
	p := strings.Split(stocke, "$")
	if len(p) != 4 {
		return false, false
	}
	iter, err1 := strconv.Atoi(p[1])
	selB, err2 := hex.DecodeString(p[2])
	if err1 != nil || err2 != nil || iter < 1 || iter > 10_000_000 {
		return false, false
	}
	ok = egal(hex.EncodeToString(deriverMdp(mdp, selB, iter)), p[3])
	return ok, ok && iter < iterMdp // a stronger default later upgrades old hashes too
}
