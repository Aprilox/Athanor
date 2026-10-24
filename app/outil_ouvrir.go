package main

// The program an MCP server drives, opened by the pilot before each cycle (Blender, a game editor…),
// described in the catalog or in the library instead of being written in the pilot ("ouvrir"):
//   "ouvrir": {
//     "programme":   "blender"            a gateway of system/bin, or a Windows program ("C:\\…\\x.exe", "${X_EXE}")
//     "args":        ["--open", "{FICHIER}"]   {FICHIER}: the project's file found; ${NAME}: the server's settings
//     "fichiers":    ["*.blend"]           looked for in the project (3 levels; not .athanor, .git, node_modules)
//     "sans_fichier": true                 open even without such a file ({FICHIER} then left out)
//     "processus":   "blender.exe"         already running: nothing to open
//     "port":        "${PORT}"             already answering: nothing to open; after opening: waited for
//     "delai":       60                    seconds to wait for the port
//   }
// athanor extensions preparer writes the servers' openings of the cycle into PRIVATE_DIR/ouvrir.json;
// athanor mcp-ouvrir KIT PROJECT FILE opens them (the pilot, before each cycle).

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

type OuvrirMCP struct {
	Programme   string   `json:"programme"`
	Args        []string `json:"args,omitempty"`
	Fichiers    []string `json:"fichiers,omitempty"`
	SansFichier bool     `json:"sans_fichier,omitempty"`
	Processus   string   `json:"processus,omitempty"`
	Port        string   `json:"port,omitempty"`
	Delai       int      `json:"delai,omitempty"`
}

type ouvertureCycle struct {
	ID string `json:"id"`
	OuvrirMCP
}

// passerellesOuverture: the gateways a description may name (the others are Windows programs).
var passerellesOuverture = map[string]bool{"blender": true, "roblox-studio": true, "unreal-editor": true}

var reProgrammeWindows = regexp.MustCompile(`(?i)^[a-z]:[\\/][^"*?<>|]+\.(exe|bat|cmd)$`)

var reMotifFichier = regexp.MustCompile(`^[A-Za-z0-9_.*?\[\]-]{1,60}$`)

// verifierOuvrir: what the app accepts in a description (the library is the user's, never Claude's).
func verifierOuvrir(o *OuvrirMCP) error {
	if o == nil {
		return nil
	}
	o.Programme = strings.TrimSpace(o.Programme)
	if o.Programme == "" {
		return errors.New("“ouvrir” needs a “programme”.")
	}
	if !passerellesOuverture[o.Programme] && !strings.HasPrefix(o.Programme, "${") && !reProgrammeWindows.MatchString(o.Programme) {
		return errors.New("“programme” is a gateway (" + strings.Join(clesTriees(passerellesOuverture), ", ") + ") or a Windows program (C:\\…\\name.exe).")
	}
	for _, a := range append(append([]string{o.Programme, o.Processus, o.Port}, o.Args...), o.Fichiers...) {
		if strings.ContainsAny(a, "\n\r\x00") {
			return errors.New("A value of “ouvrir” has a line break.")
		}
	}
	for _, m := range o.Fichiers {
		if !reMotifFichier.MatchString(m) {
			return errors.New("Invalid file pattern (a name with * and ?, no folder): " + m)
		}
	}
	if o.Processus != "" && !regexp.MustCompile(`^[A-Za-z0-9 _.-]{1,80}$`).MatchString(o.Processus) {
		return errors.New("Invalid process name: " + o.Processus)
	}
	if o.Delai < 0 || o.Delai > 600 {
		return errors.New("“delai” is between 0 and 600 seconds.")
	}
	return nil
}

func clesTriees(m map[string]bool) []string {
	var l []string
	for k := range m {
		l = append(l, k)
	}
	sort.Strings(l)
	return l
}

// construireOuvertures: the openings of the servers of this cycle (their ${NAME} filled with the
// settings), into fichier; removed when there is none.
func construireOuvertures(kit string, ids []string, fichier, dossier string) []string {
	biblio := lireJSONOrdonne(filepath.Join(kit, "extensions", "bibliotheque.json")).objet("mcp")
	secrets := lireJSONOrdonne(filepath.Join(kit, "extensions", "secrets.json"))
	catalogue := map[string]map[string]any{}
	for _, m := range liste(catalogueKit(kit).brut("mcp")) {
		if d := dict(m); d != nil {
			if id, ok := d["id"].(string); ok {
				catalogue[id] = d
			}
		}
	}
	var out []ouvertureCycle
	var notes []string
	for _, sid := range ids {
		d := dict(biblio.brut(sid))
		brut := d["ouvrir"]
		cat := catalogue[texteJSON(d["catalogue"])] // a server added by hand: only its own opening
		if brut == nil && cat != nil {
			brut = cat["ouvrir"]
		}
		if brut == nil || texteJSON(dict(brut)["programme"]) == "" && dict(brut) != nil {
			continue // none (an empty opening: the user removed the catalog's)
		}
		b, _ := json.Marshal(brut)
		var o OuvrirMCP
		if json.Unmarshal(b, &o) != nil || verifierOuvrir(&o) != nil {
			notes = append(notes, sid+": opening not understood")
			continue
		}
		// the settings: the catalog's defaults, the library's, then what the user set
		valeurs := map[string]string{}
		if cat != nil {
			for _, r := range liste(cat["reglages"]) {
				if x := dict(r); x != nil {
					valeurs[texteJSON(x["nom"])] = texteJSON(x["defaut"])
				}
			}
		}
		for k, v := range dict(d["reglages"]) {
			valeurs[k] = textePython(v)
		}
		for k, v := range dict(secrets.brut(sid)) {
			if s := textePython(v); s != "" {
				valeurs[k] = s
			}
		}
		manquants := map[string]bool{}
		remplir := func(s string) string { return remplacerExt(s, valeurs, dossier, manquants) }
		o.Programme, o.Processus, o.Port = remplir(o.Programme), remplir(o.Processus), remplir(o.Port)
		for i := range o.Args {
			if o.Args[i] != "{FICHIER}" {
				o.Args[i] = remplir(o.Args[i])
			}
		}
		if len(manquants) > 0 {
			notes = append(notes, sid+": opening skipped (missing setting "+strings.Join(trier(manquants), ", ")+")")
			continue
		}
		if !passerellesOuverture[o.Programme] && !reProgrammeWindows.MatchString(o.Programme) {
			notes = append(notes, sid+": opening skipped (not a Windows program: "+tronquerRunes(o.Programme, 120)+")")
			continue
		}
		if o.Delai == 0 {
			o.Delai = 60
		}
		if o.Processus == "" && !passerellesOuverture[o.Programme] { // a Windows program: running already, by its name
			o.Processus = o.Programme[strings.LastIndexAny(o.Programme, `\/`)+1:]
		}
		out = append(out, ouvertureCycle{sid, o})
	}
	if len(out) == 0 {
		os.Remove(fichier)
		return notes
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	_ = os.MkdirAll(filepath.Dir(fichier), 0o755)
	_ = os.WriteFile(fichier, b, 0o644)
	return notes
}

// fichierDuProjet: the first file of the project matching a pattern (3 levels down, sorted), "" when none.
func fichierDuProjet(projet string, motifs []string) string {
	var trouves []string
	racine := filepath.Clean(projet)
	_ = filepath.WalkDir(racine, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		r, _ := filepath.Rel(racine, p)
		profondeur := len(strings.Split(filepath.ToSlash(r), "/"))
		if e.IsDir() {
			if p != racine && (e.Name() == ".athanor" || e.Name() == ".git" || e.Name() == "node_modules" || profondeur >= 3) {
				return filepath.SkipDir
			}
			return nil
		}
		if !e.Type().IsRegular() {
			return nil
		}
		for _, m := range motifs {
			if ok, _ := path.Match(strings.ToLower(m), strings.ToLower(e.Name())); ok {
				trouves = append(trouves, p)
				break
			}
		}
		return nil
	})
	sort.Strings(trouves)
	if len(trouves) == 0 {
		return ""
	}
	return trouves[0]
}

// portRepond: a program listens on this local port of Windows (or of this PC).
func portRepond(port string) bool {
	if _, err := strconv.Atoi(port); err != nil {
		return false
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 2*time.Second); err == nil {
		c.Close()
		return true
	}
	if runtime.GOOS == "linux" && interopWindows() { // WSL without "mirrored" networking: ask Windows
		c := commande("netstat.exe", "-ano", "-p", "TCP")
		c.Dir = "/mnt/c"
		out, err := c.Output()
		if err != nil {
			return false
		}
		for _, l := range strings.Split(string(out), "\n") {
			f := strings.Fields(l)
			if len(f) >= 4 && strings.EqualFold(f[3], "LISTENING") && strings.HasSuffix(f[1], ":"+port) {
				return true
			}
		}
	}
	return false
}

// processusTourne: a Windows process of that name runs (tasklist).
func processusTourne(nom string) bool {
	c := commande("tasklist.exe", "/FI", "IMAGENAME eq "+nom, "/NH")
	if runtime.GOOS == "windows" {
		c = commande("tasklist", "/FI", "IMAGENAME eq "+nom, "/NH")
	} else {
		c.Dir = "/mnt/c"
	}
	cacherFenetre(c)
	c.Env = append(os.Environ(), "MSYS2_ARG_CONV_EXCL=*")
	out, err := c.Output()
	return err == nil && strings.Contains(strings.ToLower(string(out)), strings.ToLower(nom))
}

// ouvrirServeurs: each opening of the file; one line per server for the pilot's log.
func ouvrirServeurs(kit, projet, fichier string, attente func(time.Duration)) []string {
	b, err := os.ReadFile(fichier)
	if err != nil {
		return nil
	}
	var l []ouvertureCycle
	if json.Unmarshal(b, &l) != nil {
		return []string{"MCP openings: unreadable " + fichier}
	}
	var lignes []string
	for _, o := range l {
		if runtime.GOOS == "darwin" {
			if app := appMac(o.Programme); app.nom != "" && macTourne(app.processus) {
				continue
			}
		}
		if (o.Port != "" && portRepond(o.Port)) || (runtime.GOOS != "darwin" && o.Processus != "" && processusTourne(o.Processus)) {
			continue // already open (the user's, or a previous cycle's)
		}
		f := ""
		if len(o.Fichiers) > 0 {
			f = fichierDuProjet(projet, o.Fichiers)
			if f == "" && !o.SansFichier {
				lignes = append(lignes, fmt.Sprintf("%s: no %s in the project, %s not opened.", o.ID, strings.Join(o.Fichiers, " or "), o.Programme))
				continue
			}
		}
		var args []string
		for _, a := range o.Args {
			if a == "{FICHIER}" {
				if f != "" {
					args = append(args, filepath.ToSlash(f))
				}
				continue
			}
			args = append(args, a)
		}
		if runtime.GOOS == "darwin" {
			if ligne, ok := ouvrirMac(o, f); !ok {
				lignes = append(lignes, ligne)
				continue
			}
			if o.Port == "" {
				lignes = append(lignes, fmt.Sprintf("%s: %s opened for its MCP server.", o.ID, o.Programme))
				continue
			}
			pret := false
			for i := 0; i < o.Delai && !pret; i += 2 {
				attente(2 * time.Second)
				pret = portRepond(o.Port)
			}
			if pret {
				lignes = append(lignes, fmt.Sprintf("%s: %s opened, its MCP server answers on port %s.", o.ID, o.Programme, o.Port))
			} else {
				lignes = append(lignes, fmt.Sprintf("%s: %s opened, but nothing answers on port %s after %d s (is its MCP add-on started, on that port?).", o.ID, o.Programme, o.Port, o.Delai))
			}
			continue
		}
		// a gateway by its name, else the Windows program through athanor passerelle ouvrir-windows
		var c *exec.Cmd
		if passerellesOuverture[o.Programme] {
			c = commandePasserelleKit(kit, projet, o.Programme, args...)
		} else {
			c = commandePasserelleKit(kit, projet, "athanor", append([]string{"passerelle", "ouvrir-windows", o.Programme}, args...)...)
		}
		minuteur := time.AfterFunc(time.Duration(o.Delai+60)*time.Second, func() {
			if c.Process != nil {
				_ = c.Process.Kill()
			}
		})
		c.WaitDelay = 5 * time.Second
		cacherFenetre(c)
		out, err := c.CombinedOutput()
		minuteur.Stop()
		dernier := strings.TrimSpace(string(out))
		if i := strings.LastIndex(dernier, "\n"); i >= 0 {
			dernier = dernier[i+1:]
		}
		if err != nil {
			lignes = append(lignes, fmt.Sprintf("%s: %s unavailable for this cycle: %s", o.ID, o.Programme, tronquerRunes(dernier, 200)))
			continue
		}
		quoi := o.Programme
		if f != "" {
			quoi += " on " + filepath.Base(f)
		}
		if o.Port == "" {
			lignes = append(lignes, fmt.Sprintf("%s: %s opened for its MCP server.", o.ID, quoi))
			continue
		}
		pret := false
		for i := 0; i < o.Delai && !pret; i += 2 {
			attente(2 * time.Second)
			pret = portRepond(o.Port)
		}
		if pret {
			lignes = append(lignes, fmt.Sprintf("%s: %s opened, its MCP server answers on port %s.", o.ID, quoi, o.Port))
		} else {
			lignes = append(lignes, fmt.Sprintf("%s: %s opened, but nothing answers on port %s after %d s (is its MCP add-on started, on that port?).", o.ID, quoi, o.Port, o.Delai))
		}
	}
	return lignes
}

// macOS: the programs of the MCP servers are applications, opened with open -a (never a Windows program).
type applicationMac struct{ nom, processus string }

func appMac(programme string) applicationMac {
	switch programme {
	case "blender":
		return applicationMac{"Blender", "Blender"}
	case "roblox-studio":
		return applicationMac{"RobloxStudio", "RobloxStudio"}
	case "unreal-editor":
		return applicationMac{"UnrealEditor", "UnrealEditor"}
	}
	return applicationMac{}
}

func macTourne(processus string) bool {
	return commande("pgrep", "-x", processus).Run() == nil
}

// ouvrirMac: opens the program of o on the project's file f; false with the log line when it cannot.
func ouvrirMac(o ouvertureCycle, f string) (string, bool) {
	app := appMac(o.Programme)
	if app.nom == "" {
		return fmt.Sprintf("%s: %s is a Windows program, not opened on macOS.", o.ID, o.Programme), false
	}
	a := []string{"-a", app.nom}
	if app.nom == "UnrealEditor" {
		ue := os.Getenv("UE_DIR")
		if ue == "" {
			return fmt.Sprintf("%s: Unreal Engine not detected, %s not opened.", o.ID, o.Programme), false
		}
		a = []string{"-a", filepath.Join(ue, "Engine", "Binaries", "Mac", "UnrealEditor.app")}
	}
	if f != "" {
		a = append(a, f)
	}
	if out, err := commande("open", a...).CombinedOutput(); err != nil {
		return fmt.Sprintf("%s: %s unavailable for this cycle: %s", o.ID, o.Programme, tronquerRunes(strings.TrimSpace(string(out)), 200)), false
	}
	for i := 0; i < 20 && !macTourne(app.processus); i++ {
		time.Sleep(time.Second)
	}
	return "", true
}

func lancerMCPOuvrir(args []string) int {
	args = cheminsOutil(args)
	if len(args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: athanor mcp-ouvrir KIT PROJECT OPENINGS.json")
		return 2
	}
	for _, l := range ouvrirServeurs(args[0], args[1], args[2], time.Sleep) {
		fmt.Println(l)
	}
	return 0
}
