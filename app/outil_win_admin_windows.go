//go:build windows

package main

// Athanor.exe autoriser-admin [--retirer]: allows (or removes) Claude's Windows installations with
// administrator rights (was system\windows\allow-admin.ps1 and commands\allow-admin.cmd). Windows asks for
// the user's consent once: Athanor.exe is copied to C:\ProgramData\Athanor (editable only by an
// administrator) and the scheduled task "Athanor - installations" runs that copy, elevated, in the user's
// session; the user can run the task, not change it.
//
// Athanor.exe admin-tache --dossier KIT\tools\windows\admin-requests: the task's worker (was
// system\windows\admin-installer.ps1). It allows in the Windows firewall (local network only) Unreal and the
// project's programs, so that Windows never shows the "Allow access" window, then reads the requests
// dropped by Claude (one per file), validates them, runs them and writes the result. It executes nothing else.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func init() {
	enregistrerCommande("autoriser-admin", "autoriser-admin [--retirer]: allows (or removes) Claude's Windows installations with administrator rights (the task \"Athanor - installations\"; Windows asks once)", lancerAutoriserAdmin)
	enregistrerCommande("admin-tache", "admin-tache --dossier KIT\\tools\\windows\\admin-requests: the administrator task's worker (firewall rules, winget installations, elevated runs Claude asked for)", lancerAdminTache)
}

const groupePareFeu = "Athanor"

func dossierAdminWindows() string { return filepath.Join(os.Getenv("ProgramData"), "Athanor") }

func lancerAutoriserAdmin(args []string) int {
	e, args := sortieJournal(args)
	kit, args := kitWindows(args)
	retirer, eleve, utilisateur := false, false, ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--retirer", "remove":
			retirer = true
		case "--eleve":
			eleve = true
		case "--utilisateur":
			if i+1 < len(args) {
				utilisateur = args[i+1]
				i++
			}
		}
	}
	if utilisateur == "" {
		utilisateur = utilisateurWindows()
	}
	if !estAdministrateur() {
		if eleve {
			e.alerte("Administrator rights are needed.")
			return 1
		}
		// the elevated run, without a window: what it says comes back here
		a := []string{"autoriser-admin", "--eleve", "--kit", kit, "--utilisateur", utilisateur}
		if retirer {
			a = append(a, "--retirer")
		}
		code, err := relancerEleveJournal(e, a)
		if err != nil {
			e.alerte("Authorisation refused: " + err.Error())
			return 1
		}
		return code
	}
	return autoriserAdmin(e, kit, utilisateur, retirer)
}

func autoriserAdmin(e *etapeWin, kit, utilisateur string, retirer bool) int {
	dest := dossierAdminWindows()
	// Previous name of the tool (Claude Autopilote): its task, folder and firewall rules are removed in both modes.
	_ = sansFenetreWin(commande("schtasks.exe", "/Delete", "/F", "/TN", "Claude Autopilote - installations")).Run()
	_ = os.RemoveAll(filepath.Join(os.Getenv("ProgramData"), "ClaudeAutopilote"))
	_, _ = powershellCache("Remove-NetFirewallRule -Group 'Claude Autopilote' -ErrorAction SilentlyContinue")
	if retirer {
		_, _ = powershellCache("Remove-NetFirewallRule -Group $env:ATHANOR_GROUPE -ErrorAction SilentlyContinue", "ATHANOR_GROUPE="+groupePareFeu)
		_ = sansFenetreWin(commande("schtasks.exe", "/Delete", "/F", "/TN", nomTacheAdmin)).Run()
		_ = os.RemoveAll(dest)
		e.ligne("Administrator installations removed.")
		return 0
	}
	demandes := filepath.Join(kit, "tools", "windows", "admin-requests")
	for _, d := range []string{dest, demandes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			e.alerte("Cannot create " + d + ": " + err.Error())
			return 1
		}
	}
	// the task's program: a copy of this Athanor.exe in the administrators' folder (the kit's own copy can be
	// changed by the user, so by Claude: it must never run elevated)
	exe, err := os.Executable()
	if err != nil {
		e.alerte(err.Error())
		return 1
	}
	programme := filepath.Join(dest, "Athanor.exe")
	if err := copierFichierWin(exe, programme); err != nil {
		e.alerte("Cannot copy Athanor.exe into " + dest + ": " + err.Error())
		return 1
	}
	_ = os.Remove(filepath.Join(dest, "admin-installer.ps1")) // the former script
	// Folder editable only by administrators and SYSTEM; users can only read.
	_ = sansFenetreWin(commande("icacls.exe", dest, "/inheritance:r", "/grant:r", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-545:(OI)(CI)RX")).Run()
	sid, _, _, err := windows.LookupSID("", utilisateur)
	if err != nil {
		e.alerte("Unknown user " + utilisateur + ": " + err.Error())
		return 1
	}
	xml := xmlTacheAdmin(utilisateur, sid.String(), programme, `admin-tache --dossier "`+demandes+`"`)
	fx, err := os.CreateTemp("", "athanor-tache-*.xml")
	if err != nil {
		e.alerte(err.Error())
		return 1
	}
	u := utf16.Encode([]rune(xml))
	b := []byte{0xff, 0xfe}
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	_, err = fx.Write(b)
	fx.Close()
	defer os.Remove(fx.Name())
	if err != nil {
		e.alerte(err.Error())
		return 1
	}
	if out, err := sansFenetreWin(commande("schtasks.exe", "/Create", "/F", "/TN", nomTacheAdmin, "/XML", fx.Name())).CombinedOutput(); err != nil {
		e.alerte("The task could not be created: " + strings.TrimSpace(texteUTF16(out)))
		return 1
	}
	// First firewall update right away (Unreal and project programs, local network only).
	adminTache(demandes)
	e.ligne("Task '" + nomTacheAdmin + "' created: Claude can install winget software with administrator rights,")
	e.ligne("and Windows will no longer ask for network access for Unreal or the game (local network only).")
	return 0
}

func copierFichierWin(src, dst string) error {
	anciens, _ := filepath.Glob(dst + ".ancien-*")
	for _, a := range anciens {
		_ = os.Remove(a)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".nouveau"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	if os.Rename(tmp, dst) == nil {
		return nil
	}
	// the task's program is running (Windows refuses to replace it): moved aside first, removed next time
	ancien := fmt.Sprintf("%s.ancien-%d", dst, time.Now().Unix())
	if err := os.Rename(dst, ancien); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func lancerAdminTache(args []string) int {
	dossier := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--dossier" && i+1 < len(args) {
			dossier = args[i+1]
			i++
		}
	}
	if dossier == "" {
		fmt.Fprintln(os.Stderr, "Usage: admin-tache --dossier KIT\\tools\\windows\\admin-requests")
		return 2
	}
	adminTache(dossier)
	return 0
}

// adminTache: the firewall rules, then each request of the folder (nothing when it does not exist). What it
// does goes into C:\ProgramData\Athanor\journal.txt.
func adminTache(dossier string) {
	journal := filepath.Join(dossierAdminWindows(), "journal.txt")
	noter := func(t string) {
		if f, err := os.OpenFile(journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "%s  %s\r\n", time.Now().Format("2006-01-02 15:04:05"), t)
			f.Close()
		}
	}
	if !dossierExistePilote(dossier) {
		return
	}
	kit := racineKitAdmin(dossier)
	pareFeuAdmin(kit, noter)

	// ---- Requests dropped by Claude (one file each):
	//   WINGET_ID                       install a winget package (machine scope, silent)
	//   run: C:\path\program.exe args   run a program elevated, WITHOUT any UAC prompt. Only programs inside a
	//                                   known project folder or tools\windows (.exe .msi .bat .cmd .ps1).
	projets := projetsAdmin(kit)
	racines := append([]string{filepath.Join(kit, "tools", "windows")}, projets...)
	fichiers, _ := filepath.Glob(filepath.Join(dossier, "demande-*.txt"))
	for _, f := range fichiers {
		if !fichierRegulierPilote(f) {
			continue
		}
		res := filepath.Join(filepath.Dir(f), strings.Replace(filepath.Base(f), "demande-", "resultat-", 1))
		b, _ := os.ReadFile(f)
		_ = os.Remove(f)
		d := lireDemandeAdmin(string(b))
		ecrireRes := func(code int, texte string) {
			_ = os.WriteFile(res, []byte(fmt.Sprintf("code=%d\n%s\r\n", code, texte)), 0o644)
		}
		if d.Run {
			if !fichierRegulierPilote(d.Programme) || !cheminAutoriseAdmin(d.Programme, racines) {
				noter("REFUSED run outside the allowed folders: " + d.Programme)
				ecrireRes(2, `refused: only programs inside a project or tools\windows can run elevated`)
				continue
			}
			ext := strings.ToLower(filepath.Ext(d.Programme))
			switch ext {
			case ".exe", ".msi", ".bat", ".cmd", ".ps1":
			default:
				noter("REFUSED extension " + ext + " : " + d.Programme)
				ecrireRes(2, "refused: only .exe .msi .bat .cmd .ps1")
				continue
			}
			noter("elevated run: " + d.Programme + " " + d.Arguments)
			prog, ligne := ligneCommandeAdmin(d.Programme, d.Arguments, ext)
			if p, err := exec.LookPath(prog); err == nil {
				prog = p
			}
			c := sansFenetreWin(commande(prog))
			c.SysProcAttr.CmdLine = ligne
			var sortie strings.Builder
			c.Stdout = &sortie
			code := 0
			if err := c.Run(); err != nil {
				if c.ProcessState != nil {
					code = c.ProcessState.ExitCode()
				} else {
					code = 125
					sortie.WriteString(err.Error())
				}
			}
			noter(fmt.Sprintf("  -> code %d", code))
			ecrireRes(code, dernieresLignes(sortie.String(), 30))
			continue
		}
		id := d.ID
		if !reIDWinget.MatchString(id) {
			noter("REFUSED invalid identifier: " + id)
			ecrireRes(2, "invalid identifier")
			continue
		}
		noter("installing " + id)
		winget := func(scope bool) (string, int) {
			a := []string{"install", "--id", id, "--exact", "--source", "winget", "--silent"}
			if scope {
				a = append(a, "--scope", "machine")
			}
			a = append(a, "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
			c := sansFenetreWin(commande("winget.exe", a...))
			out, err := c.CombinedOutput()
			code := 0
			if err != nil {
				code = 1
				if c.ProcessState != nil {
					code = c.ProcessState.ExitCode()
				}
			}
			return string(out), code
		}
		sortie, code := winget(true)
		if code != 0 && reScopeWinget.MatchString(sortie) {
			s2, c2 := winget(false)
			sortie, code = sortie+s2, c2
		}
		if reDejaWinget.MatchString(sortie) {
			code = 0
		}
		noter(fmt.Sprintf("  -> code %d", code))
		ecrireRes(code, finSortieWinget(sortie))
	}
}

// projetsAdmin: the projects known to the app (logs\projects.txt, one path per line, written by Athanor.exe).
func projetsAdmin(kit string) []string {
	b, err := os.ReadFile(filepath.Join(kit, "logs", "projects.txt"))
	if err != nil {
		return nil
	}
	var p []string
	for _, l := range lignesFichier(string(b)) {
		l = strings.TrimSpace(l)
		if l != "" && dossierExistePilote(l) {
			p = append(p, l)
		}
	}
	return p
}

// pareFeuAdmin: inbound connections allowed only from the local network (LocalSubnet) for all the
// engine's programs (editor, compiler: UnrealBuildTool, its dotnet, UBA; tools), the kit's Windows tools
// and the projects' deliverables. A program already allowed (a rule of the group naming it) is skipped.
func pareFeuAdmin(kit string, noter func(string)) {
	var exes []string
	exesDe := func(racine string, exclure func(string) bool) {
		_ = filepath.WalkDir(racine, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".exe") && (exclure == nil || !exclure(p)) {
				exes = append(exes, p)
			}
			return nil
		})
	}
	moteurs, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramFiles"), "Epic Games", "UE_*"))
	for _, m := range moteurs {
		if !dossierExistePilote(m) {
			continue
		}
		bin := filepath.Join(m, "Engine", "Binaries")
		if dossierExistePilote(bin) {
			exesDe(bin, func(p string) bool {
				l := strings.ToLower(p)
				for _, s := range []string{`\linux\`, `\mac\`, `\ios\`, `\android\`} {
					if strings.Contains(l, s) {
						return true
					}
				}
				return false
			})
		}
	}
	for _, d := range append([]string{filepath.Join(kit, "tools", "windows")}, func() []string {
		var l []string
		for _, p := range projetsAdmin(kit) {
			l = append(l, filepath.Join(p, "deliverable"))
		}
		return l
	}()...) {
		if dossierExistePilote(d) {
			exesDe(d, nil)
		}
	}
	if len(exes) == 0 {
		return
	}
	// the programs of the group's rules, then a rule TCP and UDP for each one missing (one PowerShell call:
	// the firewall's rule groups are only reachable there)
	out, err := powershellCacheEntree(`$ErrorActionPreference = 'SilentlyContinue'
$g = $env:ATHANOR_GROUPE
$deja = @{}
Get-NetFirewallRule -Group $g | Get-NetFirewallApplicationFilter | ForEach-Object { $deja[$_.Program.ToLower()] = 1 }
foreach ($p in ([Console]::In.ReadToEnd() -split "`+"`r?`n"+`")) {
  if (-not $p -or $deja.ContainsKey($p.ToLower())) { continue }
  $n = [IO.Path]::GetFileName($p)
  foreach ($proto in @('TCP', 'UDP')) {
    New-NetFirewallRule -DisplayName "Athanor - $n ($proto)" -Group $g -Direction Inbound -Action Allow -Program $p -Protocol $proto -Profile Any -RemoteAddress LocalSubnet | Out-Null
  }
  $deja[$p.ToLower()] = 1
  Write-Output "ALLOWED $p"
}`, strings.Join(exes, "\n")+"\n", "ATHANOR_GROUPE="+groupePareFeu)
	if err != nil && out == "" {
		noter("firewall: " + err.Error())
	}
	for _, l := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(l), "ALLOWED "); ok {
			noter("firewall: " + p + " allowed (local network only)")
		}
	}
}
