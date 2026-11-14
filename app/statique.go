package main

// The interface's files (static/, embedded): each one carries the hash of its content, so a browser, or
// Cloudflare in front of remote access (it keeps .css and .js files when nothing says otherwise), never
// keeps the files of a former version: index.html (never kept) names app.css?v=<hash>, and every file is
// checked again at each load ("no-cache"; its ETag makes that a short 304).

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var empreintesStatiques sync.Map // path → hash of its content

func empreinteStatique(sous fs.FS, nom string) string {
	if v, ok := empreintesStatiques.Load(nom); ok {
		return v.(string)
	}
	b, err := fs.ReadFile(sous, nom)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	e := hex.EncodeToString(h[:6])
	empreintesStatiques.Store(nom, e)
	return e
}

// pageVersionnee: index.html with the hash of each of its files in their addresses.
func pageVersionnee(sous fs.FS, page string) string {
	for _, f := range []string{"app.css", "splash.js", "i18n.js", "app.js", "email.js"} {
		if e := empreinteStatique(sous, f); e != "" {
			page = strings.ReplaceAll(page, `"`+f+`"`, `"`+f+`?v=`+e+`"`)
		}
	}
	return page
}

// servirStatique: a file of the interface, checked again by the browser at each load.
func servirStatique(sous fs.FS, fichiers http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		if e := empreinteStatique(sous, strings.TrimPrefix(r.URL.Path, "/")); e != "" {
			w.Header().Set("ETag", `"`+e+`"`)
		}
		fichiers.ServeHTTP(w, r)
	})
}

// idLancement changes at each start of Athanor: the launch animation (splash.js) plays once per id,
// not at a page reload nor when the window is opened again.
var idLancement = strconv.FormatInt(time.Now().UnixNano(), 36)

// routesInterface serves the interface: the embedded files, and index.html behind the access check.
func (s *Serveur) routesInterface(mux *http.ServeMux) {
	sous, _ := fs.Sub(statique, "static")
	fichiers := servirStatique(sous, http.FileServer(http.FS(sous))) // never the files of a former version
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			fichiers.ServeHTTP(w, r)
			return
		}
		if !s.pageAcces(w, r) {
			return
		}
		b, _ := fs.ReadFile(sous, "index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
		s.mu.Lock()
		lancement := idLancement
		if s.config.PasAnimation {
			lancement = ""
		}
		s.mu.Unlock()
		_, _ = w.Write([]byte(strings.NewReplacer("__CSRF__", s.csrf, "__LANCEMENT__", lancement).Replace(pageVersionnee(sous, string(b)))))
	})
}
