package main

// Background execution of commands (WSL or Windows) with the output read line by line,
// for checks, installations and repair sessions.

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Tache struct {
	mu     sync.Mutex
	ID     string    `json:"id"`
	Nom    string    `json:"nom"`
	Statut string    `json:"statut"` // jamais (never run), en_cours (running), ok, echec (failed)
	Code   int       `json:"code"`
	Sortie []string  `json:"sortie"`
	Debut  time.Time `json:"debut"`
	Fin    time.Time `json:"fin"`
}

type TacheVue struct {
	ID     string   `json:"id"`
	Nom    string   `json:"nom"`
	Statut string   `json:"statut"`
	Code   int      `json:"code"`
	Sortie []string `json:"sortie"`
	Duree  int      `json:"duree_s"`
	Lignes int      `json:"lignes"`
}

func (t *Tache) Vue(dernieres int) TacheVue {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.Sortie
	if dernieres > 0 && len(s) > dernieres {
		s = s[len(s)-dernieres:]
	}
	d := 0
	if !t.Debut.IsZero() {
		if t.Fin.IsZero() {
			d = int(time.Since(t.Debut).Seconds())
		} else {
			d = int(t.Fin.Sub(t.Debut).Seconds())
		}
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return TacheVue{t.ID, t.Nom, t.Statut, t.Code, cp, d, len(t.Sortie)}
}

func (t *Tache) Texte() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.Sortie, "\n")
}

func (t *Tache) EnCours() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Statut == "en_cours"
}

func (t *Tache) ajouter(l string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Sortie = append(t.Sortie, nettoyerANSI(l))
	if len(t.Sortie) > 20000 {
		t.Sortie = t.Sortie[5000:]
	}
}

// Task registry, by id.
var taches = struct {
	mu sync.Mutex
	m  map[string]*Tache
}{m: map[string]*Tache{}}

func NouvelleTache(id, nom string) *Tache {
	t := &Tache{ID: id, Nom: nom, Statut: "jamais"}
	taches.mu.Lock()
	taches.m[id] = t
	taches.mu.Unlock()
	return t
}

func TrouverTache(id string) *Tache {
	taches.mu.Lock()
	defer taches.mu.Unlock()
	return taches.m[id]
}

// Lancer runs the command and fills the task as it goes; fin() is called at the end.
func (t *Tache) Lancer(c *exec.Cmd, fin func(ok bool)) {
	t.mu.Lock()
	t.Statut, t.Sortie, t.Debut, t.Fin = "en_cours", nil, time.Now(), time.Time{}
	t.mu.Unlock()
	cacherFenetre(c)
	stdout, _ := c.StdoutPipe()
	stderr, _ := c.StderrPipe()
	go func() {
		if err := c.Start(); err != nil {
			t.ajouter("Cannot start: " + err.Error())
			t.mu.Lock()
			t.Statut, t.Code, t.Fin = "echec", -1, time.Now()
			t.mu.Unlock()
			if fin != nil {
				fin(false)
			}
			return
		}
		var wg sync.WaitGroup
		lire := func(r io.Reader) {
			defer wg.Done()
			sc := bufio.NewScanner(r)
			sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
			for sc.Scan() {
				t.ajouter(sc.Text())
			}
		}
		wg.Add(2)
		go lire(stdout)
		go lire(stderr)
		wg.Wait()
		err := c.Wait()
		code := 0
		if err != nil {
			code = -1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		t.mu.Lock()
		t.Code, t.Fin = code, time.Now()
		if code == 0 {
			t.Statut = "ok"
		} else {
			t.Statut = "echec"
		}
		t.mu.Unlock()
		if fin != nil {
			fin(code == 0)
		}
	}()
}

func nettoyerANSI(l string) string {
	var sb strings.Builder
	r := []rune(l)
	for i := 0; i < len(r); i++ {
		if r[i] == 0x1b {
			if i+1 < len(r) && r[i+1] == '[' {
				i += 2
				for i < len(r) && !((r[i] >= 'a' && r[i] <= 'z') || (r[i] >= 'A' && r[i] <= 'Z')) {
					i++
				}
			}
			continue
		}
		if r[i] != '\r' {
			sb.WriteRune(r[i])
		}
	}
	return sb.String()
}

func itoa(n int64) string   { return fmt.Sprintf("%d", n) }
func ftoa(f float64) string { return fmt.Sprintf("%.2f", f) }
func heureCourte() string   { return time.Now().Format("15:04:05") }
