// Athanor: local dashboard of the pilot.
// A single executable: it serves the interface on 127.0.0.1 (random or chosen port) and opens it
// in an application window. Options: --kit=FOLDER (--reduit, from older startup entries, is ignored).
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

//go:embed static
var statique embed.FS

//go:embed tools/VirtualDesktop11.cs
var sourceVirtualDesktop []byte

func main() {
	// athanor COMMAND ARGS…: the sub-commands of the single executable (commandes.go); without one, the app
	if code, ok := lancerSousCommande(os.Args[1:]); ok {
		os.Exit(code)
	}
	migrerConfig()  // autopilote-config.json → athanor-config.json (previous name)
	migrerSysteme() // Windows: startup entry and local data folder of the previous name
	config := lireConfig()
	for _, a := range os.Args[1:] {
		switch {
		case strings.HasPrefix(a, "--kit="):
			config.Kit = strings.TrimPrefix(a, "--kit=")
		}
	}
	// Already running: it shows its window, this launch stops (never two Athanor at once).
	if instanceExistante("montrer") {
		return
	}
	if config.Kit == "" {
		config.Kit = devinerKit()
	}
	s := nouveauServeur(config)
	s.cleAcces = aleatoire(24) // only the windows Athanor opens can use the interface (acces.go)
	s.demarrerProjetsAuto()
	go s.surveillerTaches() // system notifications for the tasks of the user (api_pourvous.go)

	mux := http.NewServeMux()
	s.routes(mux)
	sous, _ := fs.Sub(statique, "static")
	fichiers := http.FileServer(http.FS(sous))
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
		_, _ = w.Write([]byte(strings.Replace(string(b), "__CSRF__", s.csrf, 1)))
	})

	ecoute, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", config.Port))
	if err != nil {
		if ecoute, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			log.Fatal(err)
		}
	}
	url := "http://" + ecoute.Addr().String() + "/"
	fmt.Println("Athanor:", url)
	// Only local names are accepted in the Host header: a web page pointing its own domain to
	// 127.0.0.1 ("DNS rebinding") is refused before reaching anything.
	port := ecoute.Addr().(*net.TCPAddr).Port
	hotes := map[string]bool{fmt.Sprintf("127.0.0.1:%d", port): true, fmt.Sprintf("localhost:%d", port): true}
	filtre := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hotes[strings.ToLower(r.Host)] {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Handler: filtre, ReadHeaderTimeout: 10 * time.Second}
	go func() { log.Fatal(srv.Serve(ecoute)) }()
	time.Sleep(150 * time.Millisecond)
	lienApp = func() string { return s.lienOuverture(url) }
	ecrireInstance(port)
	barreDeWindows.Store(config.BarreWindows)
	pilotesAvantFermeture = func() []string { _, noms := s.pilotesNatifs(); return noms }
	if !FenetreApp(lienApp()) {
		// Edge/Chrome window or the browser: the application keeps serving; a new launch opens another window.
		OuvrirFenetreApp(lienApp())
		for range demandesFenetre {
			OuvrirFenetreApp(lienApp())
		}
	}
	// The window was closed: the application stops (the pilots keep running in WSL; in native Windows
	// mode, the user chooses).
	s.fermetureNative()
	effacerInstance()
	remettreBureau()
}

// lienApp: a single-use address of the interface, for a window opened now (acces.go).
var lienApp = func() string { return "" }
