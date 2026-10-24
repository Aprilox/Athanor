package main

// athanor installer [KIT]: the installation of Linux, WSL and macOS, once (commands/install.sh hands over to
// it): Claude Code, the system sandbox, RTK and a few basic tools. It asks for the password once (sudo on
// Linux; Homebrew's own installer on a Mac without it). Native Windows and the WSL side of Windows keep
// their PowerShell installers (commands/install-natif.ps1, install.ps1, which then runs this one in WSL).

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const versionRTK = "v0.50.0"

type installation struct {
	kit, systeme string
	sortie       io.Writer
	couleurs     bool
}

func (in *installation) printf(f string, a ...any) { fmt.Fprintf(in.sortie, f, a...) }
func (in *installation) couleur(code, t string) string {
	if !in.couleurs {
		return t
	}
	return "\033[" + code + "m" + t + "\033[0m"
}
func (in *installation) etape(t string) { in.printf("\n%s\n", in.couleur("1", "== "+t)) }
func (in *installation) ok(t string)    { in.printf("%s\n", in.couleur("32", "✓ "+t)) }
func (in *installation) alerte(t string) {
	in.printf("%s\n", in.couleur("33", "! "+t))
}

// lancer: a command with the terminal (sudo asks for the password there); true when it succeeded.
func (in *installation) lancer(silencieux bool, nom string, args ...string) bool {
	c := exec.Command(nom, args...)
	c.Dir = in.kit
	c.Stdin = os.Stdin
	if silencieux {
		c.Stdout, c.Stderr = io.Discard, io.Discard
	} else {
		c.Stdout, c.Stderr = in.sortie, in.sortie
	}
	return c.Run() == nil
}

// shell: a line of sh (pipes of the official installers), with the terminal.
func (in *installation) shell(silencieux bool, ligne string) bool {
	return in.lancer(silencieux, "sh", "-c", ligne)
}

func aCommande(nom string) bool { _, err := exec.LookPath(nom); return err == nil }

func premiereLigne(nom string, args ...string) string {
	out, _ := exec.Command(nom, args...).CombinedOutput()
	l := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	return strings.TrimSpace(l[0])
}

func lancerInstaller(args []string) int {
	kit := ""
	if len(args) > 0 {
		kit = args[0]
	} else if exe, err := os.Executable(); err == nil {
		kit = filepath.Dir(exe)
	}
	kit, _ = filepath.Abs(kit)
	if !fichierRegulierPilote(filepath.Join(kit, "system", "pilot.sh")) {
		fmt.Fprintln(os.Stderr, "athanor installer: not a tool folder: "+kit)
		return 2
	}
	_ = os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
	nomLog := "install-wsl.log"
	if runtime.GOOS == "darwin" {
		nomLog = "install-mac.log"
	}
	var sortie io.Writer = os.Stdout
	if f, err := os.OpenFile(filepath.Join(kit, "logs", nomLog), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer f.Close()
		sortie = io.MultiWriter(os.Stdout, f)
	}
	st, _ := os.Stdout.Stat()
	in := &installation{kit: kit, systeme: filepath.Join(kit, "system"), sortie: sortie, couleurs: st != nil && st.Mode()&os.ModeCharDevice != 0}
	in.printf("===== %s athanor installer (%s)\n", time.Now().Format("2006-01-02 15:04:05"), premiereLigne("uname", "-r"))
	switch runtime.GOOS {
	case "linux":
		in.linux()
	case "darwin":
		in.mac()
	default:
		in.alerte("Windows: run commands\\install.cmd (WSL) or Setup & health → Install everything (native Windows).")
		return 1
	}
	return 0
}

func (in *installation) linux() {
	wsl := false
	if b, err := os.ReadFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft") {
		wsl = true
	}
	in.etape("System")
	if wsl {
		if !fichierExiste("/proc/sys/fs/binfmt_misc/WSLInterop") && os.Getenv("WSL_INTEROP") == "" {
			in.alerte("Windows interoperability disabled: Unreal and Blender will not be launchable.")
		}
		v, _ := os.ReadFile("/proc/version")
		if !strings.Contains(strings.ToLower(premiereLigne("uname", "-r")), "wsl2") && !strings.Contains(string(v), "WSL2") {
			in.alerte("Check that this distribution runs on WSL2 (PowerShell: wsl -l -v). The sandbox does not work on WSL1.")
		}
		in.ok("WSL detected")
		if strings.HasPrefix(in.kit, "/mnt/") {
			in.ok("Kit on the Windows drive: Unreal and Blender will be able to open the project")
		} else {
			in.alerte("The kit is not on a Windows drive (/mnt/c...): Unreal and Blender will have to go through \\\\wsl$.")
		}
	} else {
		in.ok("Native Linux")
	}

	in.etape("Packages (sudo)")
	apt, dnf := aCommande("apt-get"), aCommande("dnf")
	switch {
	case apt:
		// a previously interrupted installation blocks apt: finish it first
		in.lancer(true, "sudo", "dpkg", "--configure", "-a")
		in.lancer(true, "sudo", "apt-get", "-f", "install", "-y", "-qq")
		if in.lancer(false, "sudo", "apt-get", "update", "-qq") && in.lancer(false, "sudo", append([]string{"apt-get", "install", "-y", "-qq"},
			"git", "curl", "ca-certificates", "jq", "ripgrep", "procps", "python3", "python3-venv", "python3-pip", "nodejs", "npm", "imagemagick", "ffmpeg",
			"zip", "unzip", "bubblewrap", "socat", "xvfb", "openssh-client")...) {
			in.ok("Packages installed")
		} else {
			in.alerte("Package installation incomplete: read the messages above.")
		}
	case dnf:
		if in.lancer(false, "sudo", append([]string{"dnf", "install", "-y"}, "git", "curl", "jq", "ripgrep", "procps-ng", "python3", "nodejs", "npm",
			"ImageMagick", "ffmpeg", "zip", "unzip", "bubblewrap", "socat", "xorg-x11-server-Xvfb", "openssh-clients")...) {
			in.ok("Packages installed")
		} else {
			in.alerte("Package installation incomplete.")
		}
	default:
		in.alerte("Unknown package manager: install by hand git curl jq nodejs npm imagemagick bubblewrap socat Xvfb.")
	}

	in.etape("GitHub CLI (to link projects to GitHub)")
	aide, _ := exec.Command("gh", "auth", "login", "--help").CombinedOutput()
	switch {
	case strings.Contains(string(aide), "--skip-ssh-key"):
		in.ok("GitHub CLI already installed (" + premiereLigne("gh", "--version") + ")")
	case apt:
		// GitHub's own package repository: the distribution's gh is often too old
		if in.shell(false, `sudo mkdir -p -m 755 /etc/apt/keyrings && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | sudo tee /etc/apt/keyrings/githubcli-archive-keyring.gpg > /dev/null && sudo chmod go+r /etc/apt/keyrings/githubcli-archive-keyring.gpg && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null && sudo apt-get update -qq && sudo apt-get install -y -qq gh`) {
			in.ok("GitHub CLI installed")
		} else {
			in.alerte("GitHub CLI not installed: linking a project to GitHub will not be possible.")
		}
	case dnf:
		if in.lancer(false, "sudo", "dnf", "install", "-y", "gh") {
			in.ok("GitHub CLI installed")
		} else {
			in.alerte("GitHub CLI not installed: linking a project to GitHub will not be possible.")
		}
	default:
		in.alerte("Install the GitHub CLI by hand (https://cli.github.com) to link projects to GitHub.")
	}

	in.etape("System sandbox")
	if premiereLigne("sysctl", "-n", "kernel.apparmor_restrict_unprivileged_userns") == "1" {
		profil := "abi <abi/4.0>,\ninclude <tunables/global>\n\nprofile bwrap /usr/bin/bwrap flags=(unconfined) {\n  userns,\n  include if exists <local/bwrap>\n}\n"
		c := exec.Command("sudo", "tee", "/etc/apparmor.d/bwrap")
		c.Stdin, c.Stdout = strings.NewReader(profil), io.Discard
		_ = c.Run()
		if !in.lancer(true, "sudo", "systemctl", "reload", "apparmor") {
			in.lancer(false, "sudo", "apparmor_parser", "-r", "/etc/apparmor.d/bwrap")
		}
		in.ok("AppArmor profile for bubblewrap added")
	}
	if in.lancer(true, "bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "true") {
		in.ok("bubblewrap works")
	} else {
		in.alerte("bubblewrap does not start: the sandbox will be unavailable and Claude will refuse to start.")
	}
	// seccomp filter: blocks Unix sockets, hence launching cmd.exe or powershell.exe from inside the sandbox
	if in.lancer(true, "sudo", "npm", "install", "-g", "--silent", "@anthropic-ai/sandbox-runtime") {
		in.ok("seccomp filter installed")
	} else {
		in.alerte("seccomp filter not installed (npm): run this installation again later.")
	}

	in.claudeCode(filepath.Join(maisonInstallation(), ".bashrc"))
	in.rtk()

	in.etape("Docker (optional, for \"tool IMAGE COMMAND\")")
	if aCommande("docker") && in.lancer(true, "docker", "info") {
		in.ok("Docker reachable")
	} else {
		in.alerte("Docker unreachable. Windows: Docker Desktop > Settings > Resources > WSL integration > tick this distribution.")
	}
	in.outilsPC()
	in.etape("Done")
	in.printf("Next step: configure (token, model...). Windows: commands\\configure.cmd · Linux: commands/configure.sh\n")
	in.printf("Then, to test the sandbox: commands/test-sandbox.sh (Windows: commands\\test-sandbox.cmd)\n")
}

func (in *installation) mac() {
	in.etape("System")
	in.ok("macOS " + premiereLigne("sw_vers", "-productVersion") + " (" + runtime.GOARCH + ")")
	// Homebrew's own place: what it installs is found right away, even before the shell is restarted
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if !strings.Contains(":"+os.Getenv("PATH")+":", ":"+d+":") {
			os.Setenv("PATH", d+":"+os.Getenv("PATH"))
		}
	}
	in.etape("Homebrew (the packages of the Mac)")
	if !aCommande("brew") {
		in.printf("Homebrew is missing: its official installer runs now (it asks for your password and for Enter).\n")
		in.shell(false, `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`)
	}
	if aCommande("brew") {
		in.ok("Homebrew (" + premiereLigne("brew", "--version") + ")")
		if in.lancer(false, "brew", "install", "git", "jq", "ripgrep", "node", "imagemagick", "ffmpeg", "gh") {
			in.ok("Packages installed")
		} else {
			in.alerte("Package installation incomplete: read the messages above.")
		}
	} else {
		in.alerte("Homebrew not installed: install it from https://brew.sh, then run this installation again.")
	}
	in.etape("System sandbox")
	d, _ := os.MkdirTemp("", "athanor-cage")
	_ = exec.Command("/usr/bin/sandbox-exec", "-p", `(version 1)(allow default)(deny file-write* (subpath "`+d+`"))`, "/usr/bin/touch", filepath.Join(d, "x")).Run()
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err == nil && !fichierExiste(filepath.Join(d, "x")) {
		in.ok("Seatbelt (sandbox-exec) works")
	} else {
		in.alerte("sandbox-exec does not block writing: Claude's sandbox will be unavailable.")
	}
	_ = os.RemoveAll(d)
	in.claudeCode(filepath.Join(maisonInstallation(), ".zshrc"))
	in.rtk()
	in.outilsPC()
	in.etape("Done")
	in.printf("Next step, in the Athanor app: Setup & health → Check everything (and the token: Repair on the Token line).\n")
	in.printf("Without the app: commands/configure.sh, then commands/start.sh ~/path/to/project\n")
}

func maisonInstallation() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// claudeCode: the official installer; its folder in the PATH of the shell's start file (rc).
func (in *installation) claudeCode(rc string) {
	in.etape("Claude Code")
	bin := filepath.Join(maisonInstallation(), ".local", "bin")
	os.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	if aCommande("claude") {
		in.ok("Claude Code already installed (" + premiereLigne("claude", "--version") + ")")
	} else if in.shell(false, "curl -fsSL https://claude.ai/install.sh | bash") {
		in.ok("Claude Code installed")
	} else {
		in.alerte("Claude Code installation failed.")
	}
	if b, _ := os.ReadFile(rc); !strings.Contains(string(b), ".local/bin") {
		if f, err := os.OpenFile(rc, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_, _ = f.WriteString(`export PATH="$HOME/.local/bin:$PATH"` + "\n")
			f.Close()
		}
	}
}

// cibleRTK: the name of RTK's archive for this machine (empty: none).
func cibleRTK(goos, goarch string) string {
	switch goos + "/" + goarch {
	case "linux/amd64":
		return "x86_64-unknown-linux-musl"
	case "linux/arm64":
		return "aarch64-unknown-linux-gnu"
	case "darwin/arm64":
		return "aarch64-apple-darwin"
	case "darwin/amd64":
		return "x86_64-apple-darwin"
	}
	return ""
}

func (in *installation) rtk() {
	in.etape("RTK (command output compression)")
	dest := filepath.Join(in.systeme, "bin", "rtk")
	if st, err := os.Stat(dest); err == nil && st.Mode()&0o111 != 0 {
		in.ok("RTK already present")
		return
	}
	cible := cibleRTK(runtime.GOOS, runtime.GOARCH)
	if cible != "" && telechargerRTK("https://github.com/rtk-ai/rtk/releases/download/"+versionRTK+"/rtk-"+cible+".tar.gz", dest) == nil {
		in.ok("RTK " + versionRTK + " installed")
	} else {
		in.alerte("RTK not installed: Claude will work without it.")
	}
}

// telechargerRTK: the archive's rtk, written aside then renamed (never half a program in place).
func telechargerRTK(url, dest string) error {
	cl := &http.Client{Timeout: 2 * time.Minute}
	r, err := cl.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return extraireRTK(r.Body, dest)
}

func extraireRTK(archive io.Reader, dest string) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if err != nil {
			return fmt.Errorf("no rtk in the archive: %v", err)
		}
		if h.Typeflag != tar.TypeReg || filepath.Base(h.Name) != "rtk" {
			continue
		}
		_ = os.MkdirAll(filepath.Dir(dest), 0o755)
		tmp := dest + ".nouveau"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, io.LimitReader(t, 200<<20)); err != nil {
			f.Close()
			os.Remove(tmp)
			return err
		}
		f.Close()
		return os.Rename(tmp, dest)
	}
}

// outilsPC: the scripts executable, then the PC tools detected (what Claude will be told).
func (in *installation) outilsPC() {
	in.etape("PC tools")
	for _, motif := range []string{"commands/*.sh", "system/*.sh", "system/bin/*"} {
		l, _ := filepath.Glob(filepath.Join(in.kit, motif))
		for _, f := range l {
			if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() {
				_ = os.Chmod(f, st.Mode()|0o111)
			}
		}
	}
	projet := filepath.Join(in.kit, "logs", "sandbox-test")
	os.Setenv("PROJECT", projet)
	if err := detecter(in.kit); err != nil {
		in.alerte("PC tools not detected: " + err.Error())
		return
	}
	b, _ := os.ReadFile(filepath.Join(projet, ".athanor", "PC_TOOLS.md"))
	lignes := strings.Split(string(b), "\n")
	for i := 2; i < len(lignes) && i < 20; i++ { // sed -n '3,20p'
		in.printf("%s\n", lignes[i])
	}
}
