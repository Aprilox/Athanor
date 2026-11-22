// Athanor: local dashboard of the pilot.
// A single executable: it serves the interface on 127.0.0.1 (random or chosen port) and opens it
// in an application window; downloaded alone, it installs Athanor's folder first (embarque.go). Options: --kit=FOLDER (--reduit, from older startup entries, is ignored).
package main

import (
	"embed"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed static
var statique embed.FS

//go:embed tools/VirtualDesktop11.cs
var sourceVirtualDesktop []byte

func main() {
	// called by the name of one of Claude's commands (a link in system/bin): that command (passerelle_appel.go)
	if code, ok := lancerParNom(); ok {
		os.Exit(code)
	}
	// athanor tui / athanor gui: the terminal interface or the window (interface_mode.go)
	os.Args = append(os.Args[:1:1], lireInterface(os.Args[1:])...)
	// the downloaded file, outside Athanor's folder: it installs or updates that folder (after the user's
	// consent the first time), then the copy kept there runs (embarque.go)
	if code, ok := lancerDepuisTelechargement(os.Args[1:]); ok {
		os.Exit(code)
	}
	// athanor COMMAND ARGS…: the sub-commands of the single executable (commandes.go); without one, the app
	if code, ok := lancerSousCommande(os.Args[1:]); ok {
		os.Exit(code)
	}
	mettreAJourSurPlace() // the tool folder's files follow this executable's version
	migrerConfig()        // autopilote-config.json → athanor-config.json (previous name)
	migrerSysteme()       // Windows: startup entry and local data folder of the previous name
	config := lireConfig()
	for _, a := range os.Args[1:] {
		switch {
		case strings.HasPrefix(a, "--kit="):
			config.Kit = strings.TrimPrefix(a, "--kit=")
		}
	}
	// started by a terminal (lancerEnFond): this Athanor serves in the background, with no window nor terminal
	fond := os.Getenv("ATHANOR_FOND") == "1"
	athanorFond.Store(fond)
	terminal := interfaceTerminal() && !fond
	attendreInstanceQuiDemarre(terminal) // two launches at the same moment: the second joins the first
	// Already running: it shows its window, this launch stops (never two Athanor at once); in a terminal,
	// the terminal interface on the Athanor that runs.
	if terminal {
		if lien, existe := lienInstanceTerminal(); existe {
			if lien == "" {
				fmt.Fprintln(os.Stderr, "Athanor is already open, in a version without the terminal interface: close it, then start Athanor again.")
				os.Exit(1)
			}
			os.Exit(lancerTUI(lien, false, ""))
		}
		// none runs: Athanor starts in the background, out of this terminal's reach (closing it, or the SSH
		// session, leaves it running), and this terminal shows it (tui_fond_unix.go)
		if code, ok := lancerEnFond(); ok {
			os.Exit(code)
		}
	} else if fond {
		if instanceExistante("ping") {
			return
		}
	} else if instanceExistante("montrer") {
		return
	}
	if config.Kit == "" {
		config.Kit = devinerKit()
	}
	s := nouveauServeur(config)
	s.cleAcces = aleatoire(24) // only the windows Athanor opens can use the interface (acces.go)
	s.demarrerProjetsAuto()
	s.fermerTachesInterrompues() // M312c: a Servers task cut by Athanor's stop is closed, its first deployment waits again
	s.refuserLancementsMission() // M302: what an old mission still asked the owner is refused, with its reason
	s.surveillerVerrouAuto()     // the window locks itself after the owner's inactivity (verrou_auto.go)
	if config.MDNS {
		_ = s.demarrerMDNS() // the owner turned the local network discovery on (hub_mdns.go)
	}
	go s.surveillerDistants()   // the paired remote Athanors, every 15 s (hub_distants.go)
	go s.surveillerTaches()     // system notifications for the tasks of the user (api_pourvous.go)
	go s.surveillerJalonsPush() // milestones done, projects delivered: on the paired devices (distance_push.go)
	go s.verifierMajFond()      // a new version of Athanor (maj.go)
	go s.battementsPCs()        // several computers on a project: this one says it is there (api_pcs.go)
	go s.battementsDirects()    // the direct link between them: the masters open, the nodes beat (equipe_directe.go)
	go s.fichesServeursFond()   // the servers' sheets, every 6 hours while their keys can be opened (serveurs_fiche.go)
	go s.santeEnvsFond()        // the deployed sites' health and their incidents (sante_envs.go)
	// logs/ of the projects removed long ago, and the old team tunnels (nettoyage_journaux.go)
	go nettoyerJournaux(s.kitCourant().Racine, config.Projets)

	mux := http.NewServeMux()
	s.routes(mux)
	s.routesInterface(mux)
	s.demarrerDistance(mux) // remote access, as it was left (distance.go)

	ecoute, err := ecouterLocal(config.Port)
	if err != nil {
		log.Fatal(err)
	}
	url := "http://" + ecoute.Addr().String() + "/"
	// the address alone opens nothing (only the windows Athanor opens can use it, acces.go)
	if fond {
		fmt.Println(time.Now().Format(time.DateTime) + " Athanor runs in the background (" + url + ", pid " + strconv.Itoa(os.Getpid()) + "). ./athanor shows it in a terminal.")
	} else if !terminal {
		fmt.Println("Athanor is running (" + url + "). To open its window again, start Athanor again: the address alone does not open it. The pilots keep working even when this terminal is closed.")
	}
	port := servirLocal(ecoute, mux)
	lienApp = func() string { return s.lienOuverture(url) }
	ecrireInstance(port)
	pilotesAvantFermeture = func() []string { _, noms := s.pilotesNatifs(); return noms }
	if os.Getenv("ATHANOR_SANS_FENETRE") == "1" { // the real executable under test (TestMissionRedemarrage): it serves, no window
		select {}
	}
	if fond {
		// in the background: the window when the terminal asks for it (w), until a terminal's "Stop Athanor
		// completely" or a stop signal
		go func() {
			for range demandesFenetre {
				OuvrirFenetreApp(lienApp())
			}
		}()
		attendreArretFond()
		s.fermetureNative()
		effacerInstance()
		remettreBureau()
		os.Exit(0)
	}
	if terminal {
		// the terminal interface (tui_app.go); Athanor's window asked meanwhile (w, a second launch) opens
		go func() {
			for range demandesFenetre {
				OuvrirFenetreApp(lienApp())
			}
		}()
		code := lancerTUI(lienApp(), true, filepath.Join(s.kitCourant().Journaux(), "terminal.log"))
		s.fermetureNative()
		effacerInstance()
		remettreBureau()
		os.Exit(code)
	}
	if !FenetreApp(lienApp()) {
		// Athanor.app: its own window (WebKit), its Dock icon and its icon near the clock until it is quit
		// (app_mac_darwin.go)
		if bundleAthanorMac() != "" {
			go func() {
				for range demandesFenetre {
					if !demanderFenetreMac() {
						OuvrirFenetreApp(lienApp())
					}
				}
			}()
			boucleAppMac(actionsAppMac{
				lien:       func() string { return lienApp() },
				navigateur: func() { OuvrirFenetreApp(lienApp()) },
				pilotes:    s.nomsPilotesActifs,
				quitter: func(arreter bool) {
					if arreter {
						s.arreterTousPilotes()
					}
					effacerInstance()
				},
				langue: func() string { s.mu.Lock(); defer s.mu.Unlock(); return s.config.Langue },
			}) // returns only when Cocoa cannot be reached: Athanor then goes on as from a terminal
		}
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

// ecouterLocal: Athanor's API listens on 127.0.0.1 only (I7): the port asked, else a free one.
func ecouterLocal(port int) (net.Listener, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil && port != 0 {
		l, err = net.Listen("tcp", "127.0.0.1:0")
	}
	return l, err
}

// servirLocal: serves mux on ecoute and returns its port. Only local names are accepted in the Host header:
// a web page pointing its own domain to 127.0.0.1 ("DNS rebinding") is refused before reaching anything.
func servirLocal(ecoute net.Listener, mux http.Handler) int {
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
	return port
}

// lienApp: a single-use address of the interface, for a window opened now (acces.go).
var lienApp = func() string { return "" }
