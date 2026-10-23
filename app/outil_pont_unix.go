//go:build !windows

package main

// athanor pont KIT [PROJECT]: the bridge between Claude's sandbox and the PC tools (replaces
// system/bridge.py, same files). Inside the sandbox, the gateways (unreal-cmd, blender, tool…) drop a
// request in PROJECT/.athanor/pont/; this service, started by the pilot outside the sandbox, runs it and
// streams its output back live. Only the listed gateways, only from the project or tools/.
// Native Windows has no sandbox: no bridge there.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

var passerellesPont = map[string]bool{"tool": true, "unreal-editor": true, "unreal-cmd": true, "unreal-uat": true, "blender": true,
	"roblox-studio": true, "install-windows": true, "pc-exec": true, "windows-var": true, "adb": true}

type pont struct {
	kit, systeme, tools, projet, dossier string
	mu                                   sync.Mutex
	enCours                              map[string]*exec.Cmd
}

// ouvrirPont: a file of the bridge folder (Claude can change that folder): never through a link, and
// recreated when written whole.
func ouvrirPont(chemin string, ajout bool) (*os.File, error) {
	if st, err := os.Lstat(chemin); err == nil && (st.Mode()&os.ModeSymlink != 0 || !ajout) {
		os.Remove(chemin)
	}
	d := os.O_WRONLY | os.O_CREATE | syscall.O_NOFOLLOW
	if ajout {
		d |= os.O_APPEND
	} else {
		d |= os.O_EXCL
	}
	return os.OpenFile(chemin, d, 0o644)
}

func (p *pont) dossierSur() {
	for _, d := range []string{filepath.Join(p.projet, ".athanor"), p.dossier} {
		if estLien(d) {
			os.Remove(d)
		}
	}
	_ = os.MkdirAll(p.dossier, 0o777)
}

func dedansPont(chemin, racine string) bool {
	c, r := realpathPython(chemin), realpathPython(racine)
	return c == r || strings.HasPrefix(c, r+"/")
}

func (p *pont) executer(ident string, demande any) {
	sortie := filepath.Join(p.dossier, "sortie-"+ident+".log")
	codeF := filepath.Join(p.dossier, "code-"+ident)
	code := 126
	journal, err := ouvrirPont(sortie, true)
	if err != nil {
		return
	}
	func() {
		d, ok := demande.(map[string]any)
		if !ok {
			fmt.Fprintf(journal, "Bridge: error the request is not an object\n")
			code = 125
			return
		}
		outil, _ := d["outil"].(string)
		if _, present := d["outil"]; present && !estTexte(d["outil"]) {
			outil = textePython(d["outil"])
		}
		var args []string
		for _, a := range liste(d["args"]) {
			args = append(args, textePython(a))
		}
		cwd := p.projet
		if !passerellesPont[outil] {
			fmt.Fprintf(journal, "Bridge: '%s' is not an allowed gateway.\n", outil)
			code = 2
			return
		}
		if vraiPython(d["cwd"]) {
			s, ok := d["cwd"].(string)
			if !ok {
				fmt.Fprintf(journal, "Bridge: error the folder is not a text\n")
				code = 125
				return
			}
			cwd = s
		}
		switch {
		case !(dedansPont(cwd, p.projet) || dedansPont(cwd, p.tools)):
			fmt.Fprintf(journal, "Bridge: folder refused (%s).\n", cwd)
			code = 2
		default:
			env := append(os.Environ(), "PONT_DIRECT=1", "PROJECT="+p.projet)
			for cle, val := range dict(d["env"]) {
				if cle == "UNREAL_MCP_URL" || cle == "TZ" || cle == "LANG" {
					env = append(env, cle+"="+textePython(val))
				}
			}
			c := exec.Command(filepath.Join(p.systeme, "bin", outil), args...)
			c.Dir, c.Env, c.Stdout, c.Stderr = cwd, env, journal, journal
			c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if err := c.Start(); err != nil {
				fmt.Fprintf(journal, "Bridge: error %v\n", err)
				code = 125
				return
			}
			p.mu.Lock()
			p.enCours[ident] = c
			p.mu.Unlock()
			_ = c.Wait()
			code = codeDeSortie(c.ProcessState)
		}
	}()
	p.mu.Lock()
	delete(p.enCours, ident)
	p.mu.Unlock()
	journal.Close()
	if f, err := ouvrirPont(codeF+".tmp", false); err == nil {
		fmt.Fprint(f, code)
		f.Close()
		_ = os.Rename(codeF+".tmp", codeF)
	}
}

// codeDeSortie: as Python's returncode: the exit code, or minus the signal that stopped it.
func codeDeSortie(s *os.ProcessState) int {
	if w, ok := s.Sys().(syscall.WaitStatus); ok && w.Signaled() {
		return -int(w.Signal())
	}
	return s.ExitCode()
}

func estTexte(v any) bool { _, ok := v.(string); return ok }

// identPont: the request's name, letters and digits (any alphabet) with - and _.
func identPont(s string) bool {
	s = strings.NewReplacer("-", "", "_", "").Replace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return true
}

func (p *pont) boucle() {
	p.dossierSur()
	if es, err := os.ReadDir(p.dossier); err == nil { // requests from a previous session
		for _, e := range es {
			if !e.IsDir() {
				os.Remove(filepath.Join(p.dossier, e.Name()))
			}
		}
	}
	battement := filepath.Join(p.dossier, "vivant")
	var dernier time.Time
	parent := os.Getppid()
	for {
		if os.Getppid() != parent { // the pilot is gone: no orphan bridge
			os.Remove(battement)
			return
		}
		if time.Since(dernier) > 2*time.Second {
			p.dossierSur()
			if f, err := ouvrirPont(battement, false); err == nil {
				fmt.Fprint(f, os.Getpid())
				f.Close()
			}
			dernier = time.Now()
			if es, err := os.ReadDir(p.dossier); err == nil { // leftovers of cancelled commands
				for _, e := range es {
					n := e.Name()
					if strings.HasPrefix(n, "code-") || strings.HasPrefix(n, "sortie-") {
						if st, err := os.Stat(filepath.Join(p.dossier, n)); err == nil && time.Since(st.ModTime()) > time.Hour {
							os.Remove(filepath.Join(p.dossier, n))
						}
					}
				}
			}
		}
		es, _ := os.ReadDir(p.dossier)
		var noms []string
		for _, e := range es {
			noms = append(noms, e.Name())
		}
		sort.Strings(noms)
		for _, nom := range noms {
			chemin := filepath.Join(p.dossier, nom)
			switch {
			case strings.HasPrefix(nom, "demande-") && strings.HasSuffix(nom, ".json") && len(nom) >= len("demande-.json"):
				ident := nom[len("demande-") : len(nom)-len(".json")]
				if !identPont(ident) || estLien(chemin) {
					os.Remove(chemin)
					continue
				}
				b, err := os.ReadFile(chemin)
				var demande any
				if err != nil || !jsonPythonValide(b, &demande) {
					continue // being written: read again next time
				}
				os.Remove(chemin)
				go p.executer(ident, demande)
			case strings.HasPrefix(nom, "annuler-"):
				ident := nom[len("annuler-"):]
				os.Remove(chemin)
				p.mu.Lock()
				c := p.enCours[ident]
				p.mu.Unlock()
				if c != nil && c.Process != nil {
					_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// jsonPythonValide: json.load of a UTF-8 file (any JSON value).
func jsonPythonValide(b []byte, v *any) bool {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if d.Decode(v) != nil {
		return false
	}
	var reste any
	return d.Decode(&reste) != nil // nothing after the value
}

func lancerPont(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: athanor pont KIT [PROJECT]")
		return 2
	}
	kit := realpathPython(args[0])
	projet := filepath.Join(kit, "projet")
	if len(args) > 1 {
		projet = abspathPython(args[1])
	}
	p := &pont{kit: kit, systeme: filepath.Join(kit, "system"), tools: filepath.Join(kit, "tools"), projet: projet,
		dossier: filepath.Join(projet, ".athanor", "pont"), enCours: map[string]*exec.Cmd{}}
	fin := make(chan os.Signal, 1)
	signal.Notify(fin, syscall.SIGTERM)
	go func() { <-fin; os.Exit(0) }()
	p.boucle()
	return 0
}
