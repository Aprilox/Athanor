package main

// The announcement (ARCHITECTURE.md §Ouverture, stage 11): each running Athanor writes
// UserConfigDir()/athanor-instances/<id>.json {id, nom, version, port, pid, modules, dossier} when its server
// starts and removes it when it stops. An entry whose pid is dead or whose port does not answer
// GET /api/annonce with the same id is skipped, then deleted. "athanor instances" lists them; Settings ›
// Remote Athanors › On this PC shows the others with Pair: a discovered instance is only a proposal, never
// a link without the 6-digit code the other one shows.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Annonce struct {
	ID      string   `json:"id"`
	Nom     string   `json:"nom"`
	Version string   `json:"version"`
	Port    int      `json:"port"`
	PID     int      `json:"pid"`
	Modules []string `json:"modules"`
	Dossier string   `json:"dossier"`
}

func dossierAnnonces() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "athanor-instances")
}

// idInstance: stable for an Athanor folder (the same instance after a restart keeps its id).
func idInstance() string {
	h := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dossierAthanor()))))
	return hex.EncodeToString(h[:6])
}

func fichierAnnonce() string { return filepath.Join(dossierAnnonces(), idInstance()+".json") }

func ecrireAnnonce(port int) {
	nom, _ := os.Hostname()
	a := Annonce{ID: idInstance(), Nom: nom, Version: versionAthanor, Port: port, PID: os.Getpid(), Dossier: dossierAthanor()}
	for _, m := range modulesHub() {
		a.Modules = append(a.Modules, m.ID())
	}
	_ = os.MkdirAll(dossierAnnonces(), 0o700)
	b, _ := json.Marshal(a)
	_ = ecrireAtomique(fichierAnnonce(), b, 0o600)
}

func effacerAnnonce() {
	var a Annonce
	if b, err := os.ReadFile(fichierAnnonce()); err == nil && json.Unmarshal(b, &a) == nil && a.PID == os.Getpid() {
		_ = os.Remove(fichierAnnonce())
	}
}

// repondreAnnonce: GET /api/annonce, public and minimal (the id and the version, nothing else).
func repondreAnnonce(id func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		repondreDirect(w, http.StatusOK, map[string]string{"id": id(), "version": versionAthanor})
	}
}

// lireAnnonces: the live instances of dossier; a dead entry (pid gone, port silent or another id) is deleted.
func lireAnnonces(dossier string) []Annonce {
	entrees, _ := os.ReadDir(dossier)
	client := &http.Client{Timeout: 2 * time.Second}
	var res []Annonce
	for _, e := range entrees {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		f := filepath.Join(dossier, e.Name())
		var a Annonce
		if b, err := os.ReadFile(f); err != nil || json.Unmarshal(b, &a) != nil || a.ID+".json" != e.Name() ||
			!processusVivantPilote(a.PID) || !annonceRepond(client, a) {
			_ = os.Remove(f)
			continue
		}
		res = append(res, a)
	}
	return res
}

func annonceRepond(client *http.Client, a Annonce) bool {
	if a.Port <= 0 || a.Port > 65535 {
		return false
	}
	r, err := client.Get("http://127.0.0.1:" + strconv.Itoa(a.Port) + "/api/annonce")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	var v struct{ ID string }
	return r.StatusCode == http.StatusOK && json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&v) == nil && v.ID == a.ID
}

func init() {
	enregistrerCommande("instances", "instances: the Athanors running on this computer (their announcement)", func(args []string) int {
		liste := lireAnnonces(dossierAnnonces())
		if len(liste) == 0 {
			fmt.Println("No Athanor is running on this computer.")
			return 0
		}
		for _, a := range liste {
			moi := ""
			if a.ID == idInstance() {
				moi = "  (this folder)"
			}
			fmt.Printf("%s  %s  %s  port %d  pid %d  modules %s  %s%s\n", a.ID, a.Nom, a.Version, a.Port, a.PID, strings.Join(a.Modules, ","), a.Dossier, moi)
		}
		return 0
	})
}

// demandesInstances: the pairing requests sent to a discovered instance (its id → the request's secret),
// taken over by the remote Athanors (M187) once the other one's owner accepts.
var demandesInstances = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

// apiInstances: GET the other instances of this computer; POST {id, code}: pair with one (its 6 digits,
// sorte athanor; the other one's owner then accepts on its screen).
func (s *Serveur) apiInstances(w http.ResponseWriter, r *http.Request) {
	autres := []map[string]any{}
	for _, a := range lireAnnonces(dossierAnnonces()) {
		if a.ID == idInstance() {
			continue
		}
		demandesInstances.Lock()
		_, attente := demandesInstances.m[a.ID]
		demandesInstances.Unlock()
		autres = append(autres, map[string]any{"id": a.ID, "nom": a.Nom, "version": a.Version, "modules": a.Modules, "dossier": a.Dossier, "attente": attente})
	}
	if r.Method == http.MethodGet {
		s.mu.Lock()
		mdns := s.config.MDNS
		s.mu.Unlock()
		s.ecrireJSON(w, map[string]any{"instances": autres, "mdns": mdns, "reseau": s.instancesReseau(), "tunnel": s.adresseTunnel() != "", "distants": s.vueDistants()})
		return
	}
	var c struct{ ID, Code string }
	if lireCorps(r, &c) != nil {
		s.erreur(w, 400, "Invalid request.")
		return
	}
	if err := s.appairerInstance(dossierAnnonces(), c.ID, c.Code); err != nil {
		s.erreur(w, http.StatusBadRequest, err.Error())
		return
	}
	s.msg(w, "Code accepted: accept this Athanor in the other one's window.")
}

// appairerInstance: sends the code to the discovered instance id; nothing is linked without it.
func (s *Serveur) appairerInstance(dossier, id, code string) error {
	code = strings.TrimSpace(code)
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		return errors.New("Type the 6-digit code shown by the other Athanor (Settings › MCP clients › Connect).")
	}
	for _, a := range lireAnnonces(dossier) {
		if a.ID != id || id == idInstance() {
			continue
		}
		nom, _ := os.Hostname()
		corps, _ := json.Marshal(map[string]string{"code": code, "nom": "Athanor " + nom + " " + idInstance(), "sorte": "athanor"})
		r, err := (&http.Client{Timeout: 15 * time.Second}).Post("http://127.0.0.1:"+strconv.Itoa(a.Port)+"/api/mcp/appairer", "application/json", bytes.NewReader(corps))
		if err != nil {
			return errors.New("This Athanor does not answer any more.")
		}
		defer r.Body.Close()
		var v struct{ Demande, Erreur string }
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&v)
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "instance:" + a.ID, Action: "appairage athanor", Resultat: strconv.Itoa(r.StatusCode)})
		if r.StatusCode != http.StatusAccepted || v.Demande == "" {
			return errors.New(firstNonEmpty(v.Erreur, "The other Athanor refused the code."))
		}
		demandesInstances.Lock()
		demandesInstances.m[a.ID] = v.Demande
		demandesInstances.Unlock()
		s.suivreAppairageDistant(Distant{Nom: a.Nom + " · " + a.ID, Instance: a.ID}, v.Demande) // the token, once the other one accepts (hub_distants.go)
		return nil
	}
	return errors.New("This Athanor is no longer running on this computer.")
}
