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
	"strings"
	"sync"
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
	for _, f := range []string{"app.css", "i18n.js", "app.js"} {
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
