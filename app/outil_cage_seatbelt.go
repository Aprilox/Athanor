package main

// The macOS cage of the MCP servers (outil_cage_darwin.go): its arguments, its Seatbelt profile and its
// environment, written here without a build constraint so that every system tests them.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type argsCage struct {
	nom, outils       string
	ecriture, lecture []string
	variables         [][2]string
	commande          []string
}

var reVariableCage = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// cheminCage: the PATH inside the cage: the system's folders first, then the toolchains installed in the
// home that the cage lets read (uvx and uv in ~/.local/bin, node from nvm or volta, bun, deno, cargo…).
// Without them, a server started with uvx or npx from the home was "not found" on Linux and macOS.
func cheminCage(systeme, maison string, existe func(string) bool) string {
	l := strings.Split(systeme, ":")
	if maison == "" || maison == "/" {
		return systeme
	}
	for _, d := range []string{".local/bin", ".cargo/bin", ".bun/bin", ".deno/bin", ".volta/bin", ".npm-global/bin", ".pyenv/shims", ".pyenv/bin", ".local/share/pnpm"} {
		if existe(maison + "/" + d) {
			l = append(l, maison+"/"+d)
		}
	}
	// nvm: its default version, else the newest one installed
	if vs, _ := filepath.Glob(maison + "/.nvm/versions/node/v*/bin"); len(vs) > 0 {
		choisi := ""
		if b, err := os.ReadFile(maison + "/.nvm/alias/default"); err == nil {
			v := strings.TrimPrefix(strings.TrimSpace(string(b)), "v")
			for _, x := range vs {
				if v != "" && strings.HasPrefix(filepath.Base(filepath.Dir(x)), "v"+v) {
					choisi = x
				}
			}
		}
		if choisi == "" {
			sort.Slice(vs, func(i, j int) bool {
				return versionPlusRecente(strings.TrimPrefix(filepath.Base(filepath.Dir(vs[i])), "v"), strings.TrimPrefix(filepath.Base(filepath.Dir(vs[j])), "v"))
			})
			choisi = vs[0]
		}
		l = append(l, choisi)
	}
	return strings.Join(l, ":")
}

// lireArgsCage: NAME TOOLS [--rw DIR]... [--ro DIR]... [--env NAME VALUE]... -- COMMAND [ARGS...]
func lireArgsCage(args []string) (argsCage, string) {
	var c argsCage
	if len(args) < 2 {
		return c, "usage: NAME TOOLS [--rw DIR]... -- COMMAND"
	}
	c.nom, c.outils = args[0], args[1]
	reste := args[2:]
	for len(reste) > 0 && reste[0] != "--" {
		switch reste[0] {
		case "--rw", "--ro":
			if len(reste) < 2 {
				return c, "usage: NAME TOOLS [--rw DIR]... -- COMMAND"
			}
			if reste[0] == "--rw" {
				c.ecriture = append(c.ecriture, reste[1])
			} else {
				c.lecture = append(c.lecture, reste[1])
			}
			reste = reste[2:]
		case "--env":
			if len(reste) < 2 || !reVariableCage.MatchString(reste[1]) {
				return c, "invalid variable"
			}
			v := ""
			if len(reste) > 2 {
				v = reste[2]
			}
			c.variables = append(c.variables, [2]string{reste[1], v})
			reste = reste[min(3, len(reste)):]
		default:
			return c, "unknown option " + reste[0]
		}
	}
	if len(reste) > 0 {
		reste = reste[1:]
	}
	if len(reste) == 0 {
		return c, "no command"
	}
	c.commande = reste
	return c, ""
}

// chaineSBPL: a path as a Seatbelt string.
func chaineSBPL(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// profilSeatbelt: the cage's rules. In a Seatbelt profile the last rule that matches wins: everything is
// allowed, then writing is denied everywhere, reading denied in the home and in Athanor's folder, then the
// cage's own folders are given back.
func profilSeatbelt(maison, outils string, ecriture, lecture []string, existe func(string) bool) string {
	kit := filepath.Dir(outils)
	var b strings.Builder
	sous := func(p string) string { return "(subpath " + chaineSBPL(p) + ")" }
	b.WriteString("(version 1)\n(allow default)\n")
	b.WriteString("(deny file-write*)\n")
	b.WriteString("(allow file-write* " + sous("/private/tmp") + " " + sous("/private/var/folders") + " " + sous("/dev") + ")\n")
	b.WriteString("(deny file-read* file-write*")
	if maison != "" && maison != "/" {
		b.WriteString(" " + sous(maison))
	}
	// Athanor's folder: its settings, the keys of the MCP servers, the pilots' private folders
	b.WriteString(" (literal " + chaineSBPL(filepath.Join(kit, ".env")) + ") " + sous(filepath.Join(kit, "extensions")) + " " + sous(filepath.Join(kit, "logs")) +
		" (regex " + chaineSBPL("^"+regexp.QuoteMeta(kit)+"/athanor-config.*\\.json$") + "))\n")
	if maison != "" && maison != "/" {
		// the toolchains installed in the home stay usable (read-only): npx, uvx, node from nvm…
		var l []string
		for _, d := range []string{".nvm", ".volta", ".bun", ".deno", ".cargo", ".npm-global", ".local/bin", ".local/share/uv", ".local/share/pnpm", ".local/pipx", ".pyenv"} {
			if existe(maison + "/" + d) {
				l = append(l, sous(maison+"/"+d))
			}
		}
		// the home itself, as a folder (a program resolving ~ reads it), never what is in it
		l = append(l, "(literal "+chaineSBPL(maison)+")")
		b.WriteString("(allow file-read* " + strings.Join(l, " ") + ")\n")
	}
	var lus []string
	for _, d := range lecture {
		if existe(d) {
			lus = append(lus, sous(d))
		}
	}
	if len(lus) > 0 {
		b.WriteString("(allow file-read* " + strings.Join(lus, " ") + ")\n")
	}
	var ecrits []string
	for _, d := range append(append([]string{}, ecriture...), outils) {
		if existe(d) {
			ecrits = append(ecrits, sous(d))
		}
	}
	b.WriteString("(allow file-read* file-write* " + strings.Join(ecrits, " ") + ")\n")
	return b.String()
}

// environnementCage: the server's environment: the cage's own variables over the server's, the tokens out.
func environnementCage(base []string, variables [][2]string, maison, outils string) []string {
	retire := map[string]bool{"CLAUDE_CODE_OAUTH_TOKEN": true, "ANTHROPIC_API_KEY": true}
	poses := map[string]string{"HOME": maison, "XDG_CACHE_HOME": outils + "/cache", "npm_config_cache": outils + "/cache/npm",
		"UV_CACHE_DIR": outils + "/cache/uv", "PLAYWRIGHT_BROWSERS_PATH": outils + "/cache/ms-playwright"}
	ordre := []string{"HOME", "XDG_CACHE_HOME", "npm_config_cache", "UV_CACHE_DIR", "PLAYWRIGHT_BROWSERS_PATH"}
	vus := map[string]bool{}
	var out []string
	for _, v := range variables {
		vus[v[0]] = true
	}
	for _, e := range base {
		k, _, _ := strings.Cut(e, "=")
		if retire[k] || vus[k] || poses[k] != "" {
			continue
		}
		out = append(out, e)
	}
	for _, v := range variables {
		if !retire[v[0]] && poses[v[0]] == "" {
			out = append(out, v[0]+"="+v[1])
		}
	}
	for _, k := range ordre {
		out = append(out, k+"="+poses[k])
	}
	return out
}

func estDossierCage(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
