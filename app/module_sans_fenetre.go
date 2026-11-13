package main

// The module without a window (ARCHITECTURE.md §Ouverture): athanor module servers [--nom NAME] runs Athanor
// on a server (a VPS) as its Servers module only: no window, no terminal interface, the API on 127.0.0.1
// and the remote access tunnel started so that a hub can pair with it. athanor module appairer opens the
// 6-digit code in this terminal and takes the owner's yes there, since there is no window to give it.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// modeModule: this process is a module without a window (the terminal pairing routes answer only then).
var modeModule bool

// modulesSansFenetre: what a module without a window keeps (the hub, its settings, the vault for the keys).
var modulesSansFenetre = []string{"hub", "reglages", "coffre", "servers"}

// outilsModule: what the terminal grants the hub that pairs (another Athanor only).
var outilsModule = []string{"etat", "serveurs", "taches_serveur", "envoyer_bus"}

func init() {
	enregistrerCommande("module", "Run Athanor without a window as its Servers module (athanor module servers [--nom NAME]); pair it with a hub from this terminal (athanor module appairer)", lancerModule)
}

func lancerModule(args []string) int {
	if len(args) > 0 && args[0] == "appairer" {
		return appairerModule(os.Stdin, os.Stdout)
	}
	if len(args) == 0 || args[0] != "servers" {
		fmt.Fprintln(os.Stderr, "Usage: athanor module servers [--nom NAME]   or   athanor module appairer")
		return 2
	}
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--nom" && i+1 < len(args):
			i++
			nomInstance = args[i]
		case strings.HasPrefix(a, "--nom="):
			nomInstance = strings.TrimPrefix(a, "--nom=")
		}
	}
	if instanceExistante("ping") {
		fmt.Fprintln(os.Stderr, "An Athanor already runs for this user: stop it first.")
		return 1
	}
	modeModule = true
	modulesMu.Lock() // before anything reads the registry: Dev and Missions are not loaded
	modules = slices.DeleteFunc(modules, func(m Module) bool { return !slices.Contains(modulesSansFenetre, m.ID()) })
	modulesMu.Unlock()

	config := lireConfig()
	s := nouveauServeur(config)
	s.cleAcces = aleatoire(24)
	if os.Getenv("ATHANOR_TEST") != "1" { // shortcut: the tests run without cloudflared; a real module always opens its tunnel
		s.mu.Lock()
		s.config.Distance.Actif = true
		s.mu.Unlock()
	}
	go s.fichesServeursFond()
	mux := http.NewServeMux()
	s.routes(mux)
	s.routesInterface(mux)
	s.demarrerDistance(mux)
	ecoute, err := ecouterLocal(config.Port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	port := servirLocal(ecoute, mux)
	ecrireInstance(port)
	fmt.Printf("Athanor (module servers) runs with no window on http://127.0.0.1:%d/. To pair it with your hub: athanor module appairer\n", port)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	effacerInstance()
	return 0
}

// apiInstanceModule: the terminal pairing (POST /api/instance/module-code|module-demandes|module-decider,
// the secret of instance.json, a file of the user's own): in a module without a window only.
func (s *Serveur) apiInstanceModule(w http.ResponseWriter, r *http.Request) {
	if !modeModule {
		s.erreur(w, http.StatusForbidden, "Only an Athanor module without a window pairs from its terminal: use Settings › MCP clients.")
		return
	}
	m := s.cli()
	switch r.PathValue("action") {
	case "module-code":
		if s.urgenceClients() {
			s.erreur(w, http.StatusForbidden, "Athanor is in emergency stop: pairing is closed until Resume.")
			return
		}
		m.mu.Lock()
		m.code, m.codeFin, m.essais = sixChiffres(), time.Now().Add(dureeCodeDirect), 0
		code, fin := m.code, m.codeFin
		m.mu.Unlock()
		s.ecrireJSON(w, map[string]any{"code": code, "fin": fin.Unix()})
	case "module-demandes":
		s.ecrireJSON(w, map[string]any{"demandes": s.vueClients()["demandes"]})
	default: // module-decider
		var c struct {
			Ref      string `json:"ref"`
			Accepter bool   `json:"accepter"`
		}
		if lireCorps(r, &c) != nil {
			s.erreur(w, http.StatusBadRequest, "Invalid request.")
			return
		}
		// every server, the module's tools; deciderClient refuses them to anything but another Athanor
		if err := s.deciderClient(c.Ref, c.Accepter, []string{"*"}, outilsModule); err != nil {
			s.erreur(w, http.StatusBadRequest, err.Error())
			return
		}
		s.ecrireJSON(w, map[string]bool{"ok": true})
	}
}

// appelInstance: one call to the running module, with the secret of instance.json.
func appelInstance(action string, corps, rep any) error {
	b, err := os.ReadFile(fichierInstance())
	var i infoInstance
	if err != nil || json.Unmarshal(b, &i) != nil || i.Port <= 0 {
		return errors.New("No Athanor module runs for this user: start it with athanor module servers.")
	}
	j, _ := json.Marshal(corps)
	req, _ := http.NewRequest("POST", "http://127.0.0.1:"+strconv.Itoa(i.Port)+"/api/instance/"+action, bytes.NewReader(j))
	req.Header.Set("X-Athanor-Instance", i.Jeton)
	r, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return errors.New("The Athanor module does not answer: start it with athanor module servers.")
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		var e struct{ Erreur string }
		_ = json.NewDecoder(r.Body).Decode(&e)
		return errors.New(firstNonEmpty(e.Erreur, r.Status))
	}
	return json.NewDecoder(r.Body).Decode(rep)
}

// appairerModule: shows the code, then asks the owner, in this terminal, about each request that gives it.
func appairerModule(in io.Reader, out io.Writer) int {
	var c struct {
		Code string `json:"code"`
		Fin  int64  `json:"fin"`
	}
	if err := appelInstance("module-code", nil, &c); err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	fmt.Fprintf(out, "Pairing code: %s (valid until %s).\nOn your hub: Settings › Remote Athanors › Add, with this module's remote access address and this code.\n",
		c.Code, time.Unix(c.Fin, 0).Format("15:04"))
	lecteur := bufio.NewScanner(in)
	for time.Now().Unix() < c.Fin {
		var v struct{ Demandes []demandeClient }
		if err := appelInstance("module-demandes", nil, &v); err != nil {
			fmt.Fprintln(out, err)
			return 1
		}
		if len(v.Demandes) > 0 {
			d := v.Demandes[0]
			fmt.Fprintf(out, "%q (%s, from %s) asks to pair with this module. Accept? [y/N] ", d.Nom, d.Sorte, d.IP)
			reponse := ""
			if lecteur.Scan() {
				reponse = strings.ToLower(strings.TrimSpace(lecteur.Text()))
			}
			oui := slices.Contains([]string{"y", "yes", "o", "oui"}, reponse)
			var r struct{ OK bool }
			if err := appelInstance("module-decider", map[string]any{"ref": d.Ref, "accepter": oui}, &r); err != nil {
				fmt.Fprintln(out, err)
				return 1
			}
			if !oui {
				fmt.Fprintln(out, "Refused.")
				return 1
			}
			fmt.Fprintln(out, "Paired: your hub now sees this module.")
			return 0
		}
		time.Sleep(time.Second)
	}
	fmt.Fprintln(out, "No request came in 10 minutes: run athanor module appairer again.")
	return 1
}

// The Servers module on the bus: its sheet (reading) and its refresh (I6 on a prod server: the owner of the
// Athanor that holds the server accepts first).
func (moduleServeurs) Actions() []string { return []string{"fiche", "rafraichir_fiche"} }
func (moduleServeurs) Risque(action string) string {
	if action == "fiche" {
		return "lecture"
	}
	return ""
}
func (moduleServeurs) Recevoir(s *Serveur, m MessageHub) (MessageHub, error) {
	id, _ := m.Donnees["serveur"].(string)
	l, err := lireServeurs()
	if err != nil {
		return MessageHub{}, err
	}
	i := slices.IndexFunc(l, func(v ServeurInventaire) bool { return v.ID == id })
	if i < 0 || !idServeurValide.MatchString(id) {
		return MessageHub{}, errServeurInconnu
	}
	if m.Action == "rafraichir_fiche" {
		ctx, annuler := context.WithTimeout(context.Background(), 2*time.Minute)
		defer annuler()
		f, err := s.rafraichirFiche(ctx, l[i])
		if err != nil {
			return MessageHub{}, err
		}
		return MessageHub{Donnees: map[string]any{"fiche": f}}, nil
	}
	b, err := os.ReadFile(dossierModule("servers") + string(os.PathSeparator) + id + string(os.PathSeparator) + "FICHE.md")
	if errors.Is(err, os.ErrNotExist) {
		return MessageHub{Donnees: map[string]any{"texte": "No sheet yet for this server."}}, nil
	}
	if err != nil {
		return MessageHub{}, err
	}
	return MessageHub{Donnees: map[string]any{"texte": s.masquerSecrets(string(b))}}, nil
}
