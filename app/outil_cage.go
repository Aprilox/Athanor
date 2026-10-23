//go:build linux

package main

// The wrappers the pilot and Claude Code start, outside Claude's sandbox (WSL and Linux):
//   athanor mcp-cage NAME TOOLS [--rw DIR]... [--ro DIR]... [--env NAME VALUE]... -- COMMAND [ARGS...]
//       a local MCP server in its cage (bubblewrap): system/mcp-jail.sh
//   athanor ecran lancer PROGRAM ARGS... | capture PROJECT
//       Claude's screen on the Linux side: system/ecran.sh
// The scripts stay the entry points (the MCP configurations and the pilot name them) and hand over to
// these when the kit's executable runs here.

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

func executerP(prog string, args []string, env []string) int {
	chemin, err := exec.LookPath(prog)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
		return 127
	}
	err = syscall.Exec(chemin, append([]string{prog}, args...), env)
	fmt.Fprintf(os.Stderr, "%s: %v\n", prog, err)
	return 126
}

var reNomCage = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

func lancerMCPCage(args []string) int {
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin") // bwrap is found here, never elsewhere
	for _, v := range []string{"LD_PRELOAD", "LD_LIBRARY_PATH", "LD_AUDIT", "GCONV_PATH"} {
		os.Unsetenv(v)
	}
	erreur := func(code int, m string) int { fmt.Fprintln(os.Stderr, "mcp-jail: "+m); return code }
	if len(args) < 2 {
		return erreur(2, "usage: NAME TOOLS [--rw DIR]... -- COMMAND")
	}
	nom, outils, reste := args[0], args[1], args[2:]
	var ecriture, lecture, variables []string
	for len(reste) > 0 && reste[0] != "--" {
		switch reste[0] {
		case "--rw", "--ro":
			if len(reste) < 2 {
				return erreur(2, "usage: NAME TOOLS [--rw DIR]... -- COMMAND")
			}
			if reste[0] == "--rw" {
				ecriture = append(ecriture, reste[1])
			} else {
				lecture = append(lecture, reste[1])
			}
			reste = reste[2:]
		case "--env":
			if len(reste) < 2 || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`).MatchString(reste[1]) {
				return erreur(2, "invalid variable")
			}
			v := ""
			if len(reste) > 2 {
				v = reste[2]
			}
			variables = append(variables, "--setenv", reste[1], v)
			reste = reste[min(3, len(reste)):]
		default:
			return erreur(2, "unknown option "+reste[0])
		}
	}
	if len(reste) > 0 {
		reste = reste[1:]
	}
	if len(reste) == 0 {
		return erreur(2, "no command")
	}
	if !reNomCage.MatchString(nom) {
		return erreur(2, "invalid name")
	}
	if !dossierP(outils) {
		return erreur(2, "tool folder not found")
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		return erreur(1, "bubblewrap is missing (run install)")
	}
	maison := outils + "/mcp/" + nom
	for _, d := range []string{maison, outils + "/cache/npm", outils + "/cache/uv"} {
		if err := os.MkdirAll(d, 0o777); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			return 1
		}
	}
	a := []string{"--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/tmp", "--tmpfs", "/mnt"}
	if dossierP("/run/WSL") {
		a = append(a, "--tmpfs", "/run/WSL") // no launching Windows programs through WSL interop
	}
	// name resolution: WSL links /etc/resolv.conf (and sometimes hosts) into /mnt/wsl, hidden above; the
	// files themselves come back, read-only
	for _, f := range []string{"/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf"} {
		if r, err := filepath.EvalSymlinks(f); err == nil && strings.HasPrefix(r, "/mnt/") && fichierP(r) {
			a = append(a, "--ro-bind", r, r)
		}
	}
	if h := os.Getenv("HOME"); h != "" && dossierP(h) && h != "/" {
		a = append(a, "--tmpfs", h)
		// the toolchains installed in the home stay usable (read-only): npx, uvx, node from nvm…
		for _, d := range []string{".nvm", ".volta", ".bun", ".deno", ".cargo", ".npm-global", ".local/bin", ".local/share/uv", ".local/share/pnpm", ".local/pipx", ".pyenv"} {
			if dossierP(h + "/" + d) {
				a = append(a, "--ro-bind", h+"/"+d, h+"/"+d)
			}
		}
	}
	for _, d := range lecture {
		if dossierP(d) {
			a = append(a, "--ro-bind", d, d)
		}
	}
	for _, d := range ecriture {
		if dossierP(d) {
			a = append(a, "--bind", d, d)
		}
	}
	a = append(a, "--bind", outils, outils)
	// Claude's screen: a server's windows (Godot, a browser) open there, never on the user's screen
	if x := os.Getenv("ATHANOR_X11_DIR"); x != "" && dossierP(x) {
		a = append(a, "--bind", x, "/tmp/.X11-unix")
	}
	a = append(a, variables...) // the server's own variables, before the cage's (HOME, caches…) which win
	repertoire := maison
	if len(ecriture) > 0 {
		repertoire = ecriture[0]
	}
	a = append(a, "--setenv", "HOME", maison, "--setenv", "XDG_CACHE_HOME", outils+"/cache", "--setenv", "npm_config_cache", outils+"/cache/npm",
		"--setenv", "UV_CACHE_DIR", outils+"/cache/uv", "--setenv", "PLAYWRIGHT_BROWSERS_PATH", outils+"/cache/ms-playwright",
		"--unsetenv", "CLAUDE_CODE_OAUTH_TOKEN", "--unsetenv", "ANTHROPIC_API_KEY", "--unsetenv", "WSL_INTEROP",
		"--die-with-parent", "--chdir", repertoire, "--")
	return executerP("bwrap", append(a, reste...), os.Environ())
}

// priveDe: safety.sh's prive_de (the pilot's private folder of a project).
func priveDe(kit, projet string) string {
	h := sha1.Sum([]byte(projet))
	return kit + "/logs/pilotes/" + hex.EncodeToString(h[:])[:16]
}

func lancerEcran(args []string) int {
	switch argGH(args, 0) {
	case "lancer":
		reste := args[1:]
		if len(reste) == 0 {
			return 2
		}
		if x := os.Getenv("ATHANOR_X11_DIR"); x != "" && dossierP(x) {
			if _, err := exec.LookPath("bwrap"); err == nil {
				a := []string{"--dev-bind", "/", "/", "--bind", x, "/tmp/.X11-unix"}
				// the user's session is out of reach too: its Wayland sockets, and its D-Bus bus
				if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
					l, _ := filepath.Glob(r + "/wayland-*")
					for _, s := range append(l, r+"/bus") {
						if st, err := os.Stat(s); err == nil && st.Mode()&os.ModeSocket != 0 {
							a = append(a, "--ro-bind", "/dev/null", s)
						}
					}
				}
				return executerP("bwrap", append(append(a, "--"), reste...), os.Environ())
			}
		}
		return executerP(reste[0], reste[1:], os.Environ())
	case "capture":
		kit := os.Getenv("KIT")
		projet, _ := cheminProjetGH(firstNonEmpty(argGH(args, 1), "/nulle-part"))
		if !dossierP(projet) {
			projet = ""
		}
		b, err := os.ReadFile(priveDe(kit, projet) + "/ecran")
		absent := func() int { fmt.Fprintln(os.Stderr, "Claude's screen is not running for this project."); return 1 }
		if err != nil {
			return absent()
		}
		premiere, _, complete := strings.Cut(string(b), "\n") // read -r n dir: a whole first line
		if !complete {
			return absent()
		}
		l := strings.TrimLeft(premiere, " \t")
		n, dir := l, ""
		if i := strings.IndexAny(l, " \t"); i >= 0 {
			n, dir = l[:i], strings.Trim(l[i:], " \t")
		}
		if st, err := os.Stat(dir + "/X" + n); !regexp.MustCompile(`^[0-9]{1,4}$`).MatchString(n) || err != nil || st.Mode()&os.ModeSocket == 0 {
			return absent()
		}
		if _, err := exec.LookPath("import"); err != nil {
			fmt.Fprintln(os.Stderr, "ImageMagick is missing (run install).")
			return 1
		}
		nul, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		_ = syscall.Dup3(int(nul.Fd()), 2, 0)
		return executerP("bwrap", []string{"--dev-bind", "/", "/", "--bind", dir, "/tmp/.X11-unix", "--", "import", "-display", ":" + n, "-window", "root", "png:-"}, os.Environ())
	}
	fmt.Fprintln(os.Stderr, "Usage: ecran.sh lancer PROGRAM ARGS... | ecran.sh capture PROJECT")
	return 2
}
