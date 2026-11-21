package main

// This computer's load, for the home page (and a paired phone): processors, memory, each graphics card (machine_gpu.go),
// each disk (space used and, where
// the system says it, how busy it is) and each network link (Wi-Fi, Ethernet…) in and out. Read by machine_*.go for
// Windows, Linux and macOS; the rates are the change between two readings, taken every 2 s while someone looks.

import (
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// OrdinateurVue: what the page shows; a value the system does not give is -1 (CPU, activity) or absent.
type OrdinateurVue struct {
	CPU      float64     `json:"cpu"` // %, all processors
	Coeurs   int         `json:"coeurs"`
	RAMTotal uint64      `json:"ram_total"`
	RAMUtil  uint64      `json:"ram_util"`
	GPU      []GPUVue    `json:"gpus"` // machine_gpu.go
	Disques  []DisqueVue `json:"disques"`
	Reseaux  []ReseauVue `json:"reseaux"`
	Lu       string      `json:"lu"`
	Systeme  string      `json:"systeme"`
}

type DisqueVue struct {
	Nom      string  `json:"nom"` // C:, /, /Volumes/Data…
	Total    uint64  `json:"total"`
	Libre    uint64  `json:"libre"`
	Activite float64 `json:"activite"` // % of the time busy reading or writing, -1 unknown
	Lecture  float64 `json:"lecture"`  // bytes/s, -1 unknown
	Ecriture float64 `json:"ecriture"` // bytes/s, -1 unknown
}

type ReseauVue struct {
	Nom    string  `json:"nom"`    // the system's name for the link (Wi-Fi, Ethernet, en0, wlan0…)
	Genre  string  `json:"genre"`  // wifi | ethernet | autre
	Entree float64 `json:"entree"` // bytes/s received
	Sortie float64 `json:"sortie"` // bytes/s sent
}

// lectureMachine: one raw reading of the counters (cumulative), as each system gives them.
type lectureMachine struct {
	t         time.Time
	cpuOccupe uint64 // ticks or 100 ns units, any unit as long as both are the same
	cpuTotal  uint64
	ramTotal  uint64
	ramUtil   uint64
	gpus      []GPUVue // already in percent and bytes: each system gives them so
	disques   []disqueBrut
	reseaux   []reseauBrut
	coeurs    int
}

type disqueBrut struct {
	nom            string
	total, libre   uint64
	occupe, duree  uint64 // busy time and elapsed time counters in the same unit; duree 0: the clock's (ms)
	lus, ecrits    uint64 // bytes
	activiteConnue bool
}

type reseauBrut struct {
	nom, genre     string
	entree, sortie uint64
}

var machine = struct {
	sync.Mutex
	avant, apres *lectureMachine
	vu           time.Time // last time someone asked: the readings stop 30 s later
	marche       bool
}{}

// echantillonner reads the counters every 2 s while the page asks (30 s after the last ask it stops).
func echantillonner() {
	for {
		l := lireMachine()
		machine.Lock()
		machine.avant, machine.apres = machine.apres, &l
		if time.Since(machine.vu) > 30*time.Second {
			machine.marche = false
			machine.Unlock()
			return
		}
		machine.Unlock()
		time.Sleep(2 * time.Second)
	}
}

// vueMachine: the last two readings turned into percentages and rates.
func vueMachine(a, b *lectureMachine) OrdinateurVue {
	v := OrdinateurVue{CPU: -1, Coeurs: b.coeurs, RAMTotal: b.ramTotal, RAMUtil: b.ramUtil, GPU: append([]GPUVue{}, b.gpus...), Disques: []DisqueVue{}, Reseaux: []ReseauVue{},
		Lu: b.t.UTC().Format(time.RFC3339), Systeme: systemeMachine()}
	dt := 0.0
	if a != nil {
		dt = b.t.Sub(a.t).Seconds()
	}
	if a != nil && b.cpuTotal > a.cpuTotal {
		v.CPU = borne(100 * float64(b.cpuOccupe-a.cpuOccupe) / float64(b.cpuTotal-a.cpuTotal))
	}
	for _, d := range b.disques {
		dv := DisqueVue{Nom: d.nom, Total: d.total, Libre: d.libre, Activite: -1, Lecture: -1, Ecriture: -1}
		if a != nil && dt > 0 {
			if i := slices.IndexFunc(a.disques, func(x disqueBrut) bool { return x.nom == d.nom }); i >= 0 {
				p := a.disques[i]
				if d.activiteConnue && p.activiteConnue && d.occupe >= p.occupe {
					duree := float64(d.duree - p.duree)
					if d.duree == 0 { // the counter is in ms of the clock
						duree = dt * 1000
					}
					if duree > 0 {
						dv.Activite = borne(100 * float64(d.occupe-p.occupe) / duree)
					}
				}
				if d.lus >= p.lus && d.ecrits >= p.ecrits && (d.lus > 0 || d.ecrits > 0) {
					dv.Lecture, dv.Ecriture = float64(d.lus-p.lus)/dt, float64(d.ecrits-p.ecrits)/dt
				}
			}
		}
		v.Disques = append(v.Disques, dv)
	}
	for _, r := range b.reseaux {
		rv := ReseauVue{Nom: r.nom, Genre: r.genre}
		if a != nil && dt > 0 {
			if i := slices.IndexFunc(a.reseaux, func(x reseauBrut) bool { return x.nom == r.nom }); i >= 0 {
				rv.Entree, rv.Sortie = debit(a.reseaux[i].entree, r.entree, dt), debit(a.reseaux[i].sortie, r.sortie, dt)
			}
		}
		v.Reseaux = append(v.Reseaux, rv)
	}
	sort.SliceStable(v.Reseaux, func(i, j int) bool {
		return v.Reseaux[i].Entree+v.Reseaux[i].Sortie > v.Reseaux[j].Entree+v.Reseaux[j].Sortie
	})
	return v
}

// debit: bytes/s between two counters; a 32-bit counter that wrapped (Windows' older table) is counted across.
func debit(avant, apres uint64, dt float64) float64 {
	if apres >= avant {
		return float64(apres-avant) / dt
	}
	if avant < 1<<32 {
		return float64(apres+1<<32-avant) / dt
	}
	return 0
}

func borne(x float64) float64 { return max(0, min(100, x)) }

// apiMachine: GET /api/machine: this computer's load now (the first ask starts the readings: its rates come with
// the next one, 2 s later).
func (s *Serveur) apiMachine(w http.ResponseWriter, r *http.Request) {
	machine.Lock()
	machine.vu = time.Now()
	if !machine.marche {
		machine.marche = true
		l := lireMachine()
		machine.avant, machine.apres = nil, &l
		go func() { time.Sleep(2 * time.Second); echantillonner() }()
	}
	a, b := machine.avant, machine.apres
	machine.Unlock()
	s.ecrireJSON(w, vueMachine(a, b))
}

// genreReseau: wifi, ethernet or autre from a link's name or description.
func genreReseau(nom string) string {
	n := strings.ToLower(nom)
	switch {
	case strings.Contains(n, "wi-fi") || strings.Contains(n, "wifi") || strings.Contains(n, "wlan") || strings.Contains(n, "wireless") || strings.HasPrefix(n, "wl"):
		return "wifi"
	case strings.Contains(n, "ethernet") || strings.HasPrefix(n, "eth") || strings.HasPrefix(n, "en") || strings.Contains(n, "lan"):
		return "ethernet"
	}
	return "autre"
}
