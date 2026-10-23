package main

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

func sha256Somme(b []byte) [32]byte { return sha256.Sum256(b) }
func runtimeGOOS() string           { return runtime.GOOS }
func runtimeGOARCH() string         { return runtime.GOARCH }

// httptestServeur: a local server answering each path with its bytes (any query string).
func httptestServeur(t *testing.T, reponses map[string][]byte) string {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := reponses[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s.URL
}
