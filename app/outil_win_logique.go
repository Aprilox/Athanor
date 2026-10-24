package main

// The Windows side's installation steps (outil_win_*_windows.go), their parts without any Windows call:
// reading wsl.exe's answers, editing .wslconfig, checking the requests of the administrator task. Here so
// that they are tested on every system. The former PowerShell scripts' rules, word for word.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf16"
)

// distroWSL: a line of "wsl --list --verbose".
type distroWSL struct {
	Nom     string
	Defaut  bool
	Version int
}

var reLigneDistro = regexp.MustCompile(`^\s*(\*?)\s*(\S+)\s+(\S+)\s+(\d)\s*$`)

// lireDistrosWSL: the distributions of "wsl --list --verbose" (header skipped), Docker Desktop's left out.
func lireDistrosWSL(texte string) []distroWSL {
	texte = strings.ReplaceAll(texte, "\x00", "")
	var lignes []string
	for _, l := range regexp.MustCompile(`\r?\n`).Split(texte, -1) {
		if strings.TrimSpace(l) != "" {
			lignes = append(lignes, l)
		}
	}
	if len(lignes) > 0 {
		lignes = lignes[1:]
	}
	var d []distroWSL
	for _, l := range lignes {
		m := reLigneDistro.FindStringSubmatch(l)
		if m == nil || strings.HasPrefix(m[2], "docker-desktop") {
			continue
		}
		d = append(d, distroWSL{Nom: m[2], Defaut: m[1] == "*", Version: int(m[4][0] - '0')})
	}
	return d
}

// choisirDistroWSL: the first Ubuntu, else the default one, else the first one.
func choisirDistroWSL(d []distroWSL) int {
	for i, x := range d {
		if strings.HasPrefix(x.Nom, "Ubuntu") {
			return i
		}
	}
	for i, x := range d {
		if x.Defaut {
			return i
		}
	}
	return 0
}

var (
	reMirroredActif  = regexp.MustCompile(`(?im)^\s*networkingMode\s*=\s*mirrored`)
	reNetworkingMode = regexp.MustCompile(`^\s*networkingMode\s*=`)
	reSectionWSL2    = regexp.MustCompile(`(?im)^(\s*\[wsl2\][^\r\n]*\r?\n)`)
	reSectionWSL2L   = regexp.MustCompile(`(?i)^\s*\[wsl2\]`)
)

// lignesFichier: ReadAllLines (the last line end does not make an empty line).
func lignesFichier(texte string) []string {
	if texte == "" {
		return nil
	}
	texte = strings.TrimPrefix(texte, "\ufeff")
	l := regexp.MustCompile(`\r\n|\n|\r`).Split(texte, -1)
	if len(l) > 0 && l[len(l)-1] == "" {
		l = l[:len(l)-1]
	}
	return l
}

// sansNetworkingMode: .wslconfig without its networkingMode lines (install's way: CRLF, one at the end).
func sansNetworkingMode(texte string) string {
	var garde []string
	for _, l := range lignesFichier(texte) {
		if !reNetworkingMode.MatchString(l) {
			garde = append(garde, l)
		}
	}
	return strings.Join(garde, "\r\n") + "\r\n"
}

// avecMirrored: .wslconfig with networkingMode=mirrored right after the [wsl2] line, or a [wsl2] section
// added at the end (install's way).
func avecMirrored(texte string) string {
	if reSectionWSL2.MatchString(texte) {
		fait := false
		texte = reSectionWSL2.ReplaceAllStringFunc(texte, func(s string) string {
			if fait {
				return s
			}
			fait = true
			return s + "networkingMode=mirrored\r\n"
		})
	} else {
		texte = strings.TrimRight(texte, " \t\r\n") + "\r\n[wsl2]\r\nnetworkingMode=mirrored\r\n"
	}
	return strings.TrimLeft(texte, " \t\r\n")
}

// lignesReseauUnreal: the lines of .wslconfig without networkingMode and without a broken "[" line
// (reseau-unreal's way; a blank line stays).
func lignesReseauUnreal(texte string) []string {
	var l []string
	reVideOuCrochet := regexp.MustCompile(`^\s*\[?\s*$`)
	reVide := regexp.MustCompile(`^\s*$`)
	reCrochet := regexp.MustCompile(`^\s*\[\s*$`)
	for _, x := range lignesFichier(texte) {
		if (!reNetworkingMode.MatchString(x) && !reVideOuCrochet.MatchString(x)) || reVide.MatchString(x) {
			if !reCrochet.MatchString(x) {
				l = append(l, x)
			}
		}
	}
	return l
}

// avecMirroredLignes: networkingMode=mirrored after the first [wsl2] line, or a [wsl2] section at the end.
func avecMirroredLignes(l []string) []string {
	var n []string
	trouve := false
	for _, x := range l {
		n = append(n, x)
		if !trouve && reSectionWSL2L.MatchString(x) {
			n = append(n, "networkingMode=mirrored")
			trouve = true
		}
	}
	if !trouve {
		n = append(n, "[wsl2]", "networkingMode=mirrored")
	}
	return n
}

// texteLignesWSL: the lines joined with CRLF, trimmed, one CRLF at the end.
func texteLignesWSL(l []string) string {
	return strings.TrimSpace(strings.Join(l, "\r\n")) + "\r\n"
}

// ---------------------------------------------------------------------------- the administrator task

var (
	reIDWinget      = regexp.MustCompile(`^[A-Za-z0-9._+-]{2,100}$`)
	reDemandeRun    = regexp.MustCompile(`^run:\s*(.+)$`)
	reRunGuillemets = regexp.MustCompile(`^"([^"]+)"\s*(.*)$`)
	reLigneBarre    = regexp.MustCompile(`^[\s\-\\|/█▒]+$`)
	reScopeWinget   = regexp.MustCompile(`scope|portée`)
	reDejaWinget    = regexp.MustCompile(`already installed|déjà installé|No newer package|Aucune mise à niveau`)
)

// demandeAdmin: a request file of Claude's, read: a winget id, or "run: PROGRAM ARGUMENTS".
type demandeAdmin struct {
	Run       bool
	ID        string
	Programme string
	Arguments string
}

func lireDemandeAdmin(texte string) demandeAdmin {
	texte = strings.TrimSpace(strings.TrimPrefix(texte, "\ufeff"))
	if m := reDemandeRun.FindStringSubmatch(texte); m != nil {
		ligne := strings.TrimSpace(m[1])
		d := demandeAdmin{Run: true}
		if g := reRunGuillemets.FindStringSubmatch(ligne); g != nil {
			d.Programme, d.Arguments = g[1], g[2]
		} else {
			parts := regexp.MustCompile(`\s+`).Split(ligne, 2)
			d.Programme = parts[0]
			if len(parts) > 1 {
				d.Arguments = parts[1]
			}
		}
		d.Arguments = strings.TrimSpace(d.Arguments)
		return d
	}
	return demandeAdmin{ID: texte}
}

// cheminAutoriseAdmin: the program lies inside one of the allowed folders (Windows paths, any case).
func cheminAutoriseAdmin(chemin string, racines []string) bool {
	plein := strings.ToLower(nettoyerCheminWin(chemin))
	if plein == "" {
		return false
	}
	for _, r := range racines {
		rf := strings.ToLower(strings.TrimRight(nettoyerCheminWin(r), `\`)) + `\`
		if rf != `\` && strings.HasPrefix(plein, rf) {
			return true
		}
	}
	return false
}

// nettoyerCheminWin: a Windows path with its ".." and "." resolved (GetFullPath for an absolute path).
func nettoyerCheminWin(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	if len(p) < 3 || p[1] != ':' || p[2] != '\\' {
		return ""
	}
	var pile []string
	for _, x := range strings.Split(p[3:], `\`) {
		switch x {
		case "", ".":
		case "..":
			if len(pile) > 0 {
				pile = pile[:len(pile)-1]
			}
		default:
			pile = append(pile, x)
		}
	}
	return p[:3] + strings.Join(pile, `\`)
}

// finSortieWinget: the last 15 lines of winget's output that say something (its progress bars left out).
func finSortieWinget(sortie string) string {
	var l []string
	for _, x := range regexp.MustCompile(`\r?\n`).Split(sortie, -1) {
		if strings.TrimSpace(x) != "" && !reLigneBarre.MatchString(x) {
			l = append(l, x)
		}
	}
	if len(l) > 15 {
		l = l[len(l)-15:]
	}
	return strings.Join(l, "\n")
}

// dernieresLignes: the last n lines of a text (Get-Content | Select-Object -Last n).
func dernieresLignes(texte string, n int) string {
	l := lignesFichier(strings.ReplaceAll(texte, "\r\n", "\n"))
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// ligneCommandeAdmin: the program and the raw command line of an elevated run, by its extension (an .msi
// through msiexec, a .ps1 through PowerShell, a .bat or .cmd through cmd).
func ligneCommandeAdmin(prog, arguments, ext string) (string, string) {
	switch ext {
	case ".msi":
		return "msiexec.exe", strings.TrimSpace(`msiexec.exe /i "` + prog + `" /qn /norestart ` + arguments)
	case ".ps1":
		return "powershell.exe", strings.TrimSpace(`powershell.exe -NoProfile -ExecutionPolicy Bypass -File "` + prog + `" ` + arguments)
	case ".bat", ".cmd":
		return "cmd.exe", `cmd.exe /c ""` + prog + `" ` + arguments + `"`
	}
	return prog, strings.TrimSpace(`"` + prog + `" ` + arguments)
}

const nomTacheAdmin = "Athanor - installations"

// xmlTacheAdmin: the scheduled task "Athanor - installations": run by the user's session with its highest
// rights, its program in the administrators' folder; the user can run it, not change it (the security
// descriptor: full access for Administrators and SYSTEM, read and run for the user).
func xmlTacheAdmin(utilisateur, sid, programme, arguments string) string {
	x := func(s string) string {
		return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
	}
	return `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>Athanor: Claude's Windows installations with administrator rights (winget, firewall).</Description>
    <SecurityDescriptor>D:(A;;FA;;;BA)(A;;FA;;;SY)(A;;GRGX;;;` + x(sid) + `)</SecurityDescriptor>
  </RegistrationInfo>
  <Principals>
    <Principal id="Author">
      <UserId>` + x(utilisateur) + `</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>Queue</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <AllowStartOnDemand>true</AllowStartOnDemand>
    <Enabled>true</Enabled>
    <ExecutionTimeLimit>PT2H</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>` + x(programme) + `</Command>
      <Arguments>` + x(arguments) + `</Arguments>
    </Exec>
  </Actions>
</Task>
`
}

// texteUTF16: the text of a Windows tool's output, UTF-16 (with or without its BOM) or 8-bit.
func texteUTF16(b []byte) string {
	if len(b) >= 2 && (bytes.HasPrefix(b, []byte{0xff, 0xfe}) || (len(b) >= 4 && b[1] == 0 && b[3] == 0)) {
		b = bytes.TrimPrefix(b, []byte{0xff, 0xfe})
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

// tacheAdminAJourXML: the task's definition runs Athanor's executable (admin-tache), not the former
// PowerShell script (system\windows\admin-installer.ps1, removed): a task made by an older version must be
// allowed again.
func tacheAdminAJourXML(xml string) bool {
	l := strings.ToLower(xml)
	return strings.Contains(l, "athanor.exe") && strings.Contains(l, "admin-tache")
}

// tacheAdminEtat: the administrator task exists, and runs this version's worker. From Windows (schtasks.exe)
// or from WSL (through interop, from /mnt/c).
func tacheAdminEtat() (presente, aJour bool) {
	prog, dossier := "schtasks.exe", ""
	if runtime.GOOS != "windows" {
		prog = "/mnt/c/Windows/System32/schtasks.exe"
		if _, err := os.Stat(prog); err != nil {
			return false, false
		}
		dossier = "/mnt/c"
	}
	c := commande(prog, "/query", "/tn", nomTacheAdmin, "/xml")
	c.Dir = dossier
	cacherFenetre(c)
	out, err := c.Output()
	if err != nil {
		return false, false
	}
	return true, tacheAdminAJourXML(texteUTF16(out))
}

// racineKitAdmin: the tool folder from the requests folder (KIT\tools\windows\admin-requests).
func racineKitAdmin(dossier string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(dossier)))
}
