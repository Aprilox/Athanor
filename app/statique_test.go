package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStatiqueVersionne: index.html names each file with the hash of its content; the files are checked
// again at each load, a 304 when unchanged.
func TestStatiqueVersionne(t *testing.T) {
	sous, _ := fs.Sub(statique, "static")
	b, _ := fs.ReadFile(sous, "index.html")
	page := pageVersionnee(sous, string(b))
	for _, f := range []string{"app.css", "i18n.js", "app.js"} {
		e := empreinteStatique(sous, f)
		if len(e) != 12 || !strings.Contains(page, `"`+f+`?v=`+e+`"`) || strings.Contains(page, `"`+f+`"`) {
			t.Fatalf("%s not versioned in index.html (%q)", f, e)
		}
	}
	h := servirStatique(sous, http.FileServer(http.FS(sous)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/app.css?v=x", nil))
	etag := w.Header().Get("ETag")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" || etag == "" {
		t.Fatalf("app.css: %d %q %q", w.Code, w.Header().Get("Cache-Control"), etag)
	}
	r := httptest.NewRequest("GET", "/app.css", nil)
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotModified {
		t.Fatalf("unchanged app.css: %d", w.Code)
	}
}
