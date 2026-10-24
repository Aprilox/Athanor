package main

import (
	"strings"
	"testing"
)

func TestLireDistrosWSL(t *testing.T) {
	texte := "  NAME                   STATE           VERSION\r\n* docker-desktop         Running         2\r\n  Ubuntu-22.04           Stopped         2\r\n  Debian                 Stopped         1\r\n\r\n"
	d := lireDistrosWSL(strings.Join(strings.Split(texte, ""), "\x00"))
	if len(d) != 2 || d[0].Nom != "Ubuntu-22.04" || d[0].Version != 2 || d[1].Nom != "Debian" || d[1].Version != 1 || d[0].Defaut {
		t.Fatalf("%+v", d)
	}
	if choisirDistroWSL(d) != 0 {
		t.Fatal("Ubuntu first")
	}
	d2 := lireDistrosWSL("NAME STATE VERSION\n  Arch Stopped 2\n* Fedora Running 2\n")
	if choisirDistroWSL(d2) != 1 || !d2[1].Defaut {
		t.Fatalf("the default one: %+v", d2)
	}
	if len(lireDistrosWSL("Windows Subsystem for Linux has no installed distributions.\r\n")) != 0 {
		t.Fatal("no distribution")
	}
}

func TestWslconfigInstall(t *testing.T) {
	if g := avecMirrored(""); g != "[wsl2]\r\nnetworkingMode=mirrored\r\n" {
		t.Fatalf("%q", g)
	}
	if g := avecMirrored("[wsl2]\r\nmemory=8GB\r\n[wsl2]\r\n"); g != "[wsl2]\r\nnetworkingMode=mirrored\r\nmemory=8GB\r\n[wsl2]\r\n" {
		t.Fatalf("after the first [wsl2] only: %q", g)
	}
	if g := avecMirrored("[experimental]\nx=1\n\n"); g != "[experimental]\nx=1\r\n[wsl2]\r\nnetworkingMode=mirrored\r\n" {
		t.Fatalf("%q", g)
	}
	if !reMirroredActif.MatchString("[wsl2]\r\n  NetworkingMode = Mirrored\r\n") {
		t.Fatal("mirrored seen")
	}
	if g := sansNetworkingMode("[wsl2]\r\nnetworkingMode=mirrored\r\nmemory=8GB"); g != "[wsl2]\r\nmemory=8GB\r\n" {
		t.Fatalf("%q", g)
	}
}

func TestWslconfigUnreal(t *testing.T) {
	l := lignesReseauUnreal("[wsl2]\r\nnetworkingMode=nat\r\n[\r\n\r\nmemory=4GB\r\n")
	if strings.Join(l, "|") != "[wsl2]||memory=4GB" {
		t.Fatalf("%q", l)
	}
	if g := texteLignesWSL(avecMirroredLignes(l)); g != "[wsl2]\r\nnetworkingMode=mirrored\r\n\r\nmemory=4GB\r\n" {
		t.Fatalf("%q", g)
	}
	if g := texteLignesWSL(avecMirroredLignes(nil)); g != "[wsl2]\r\nnetworkingMode=mirrored\r\n" {
		t.Fatalf("%q", g)
	}
}

func TestDemandesAdmin(t *testing.T) {
	d := lireDemandeAdmin("\ufeffrun: \"C:\\Dev\\jeu\\setup tool.exe\" /S /D=x \r\n")
	if !d.Run || d.Programme != `C:\Dev\jeu\setup tool.exe` || d.Arguments != "/S /D=x" {
		t.Fatalf("%+v", d)
	}
	d = lireDemandeAdmin("run:C:\\k\\tools\\windows\\a.msi")
	if !d.Run || d.Programme != `C:\k\tools\windows\a.msi` || d.Arguments != "" {
		t.Fatalf("%+v", d)
	}
	d = lireDemandeAdmin("Git.Git\n")
	if d.Run || d.ID != "Git.Git" || !reIDWinget.MatchString(d.ID) || reIDWinget.MatchString("a b") || reIDWinget.MatchString("x;calc") {
		t.Fatalf("%+v", d)
	}
	racines := []string{`C:\Kit\tools\windows`, `D:\Dev\jeu`}
	for chemin, veut := range map[string]bool{
		`D:\Dev\jeu\bin\a.exe`:       true,
		`d:/dev/JEU/a.exe`:           true,
		`D:\Dev\jeu2\a.exe`:          false,
		`D:\Dev\jeu\..\autre\a.exe`:  false,
		`C:\Kit\tools\windows\x.ps1`: true,
		`a.exe`:                      false,
	} {
		if cheminAutoriseAdmin(chemin, racines) != veut {
			t.Errorf("%s: want %v", chemin, veut)
		}
	}
	if p, l := ligneCommandeAdmin(`C:\a b\x.msi`, "/l*v log.txt", ".msi"); p != "msiexec.exe" || l != `msiexec.exe /i "C:\a b\x.msi" /qn /norestart /l*v log.txt` {
		t.Fatalf("%s %s", p, l)
	}
	if _, l := ligneCommandeAdmin(`C:\x.cmd`, "", ".cmd"); l != `cmd.exe /c ""C:\x.cmd" "` {
		t.Fatalf("%s", l)
	}
	if _, l := ligneCommandeAdmin(`C:\x.exe`, "", ".exe"); l != `"C:\x.exe"` {
		t.Fatalf("%s", l)
	}
	if g := finSortieWinget("  ██████▒▒  \r\nFound Git [Git.Git]\r\n - \r\n\r\nSuccessfully installed\r\n"); g != "Found Git [Git.Git]\nSuccessfully installed" {
		t.Fatalf("%q", g)
	}
	if !reDejaWinget.MatchString("Found an existing package already installed.") || !reScopeWinget.MatchString("No applicable installer found for the scope") {
		t.Fatal("winget messages")
	}
}

func TestTacheAdminXML(t *testing.T) {
	x := xmlTacheAdmin(`PC\Moi & co`, "S-1-5-21-1", `C:\ProgramData\Athanor\Athanor.exe`, `admin-tache --dossier "C:\K\tools\windows\admin-requests"`)
	for _, s := range []string{`<UserId>PC\Moi &amp; co</UserId>`, "(A;;GRGX;;;S-1-5-21-1)", "<RunLevel>HighestAvailable</RunLevel>",
		"<Arguments>admin-tache --dossier &quot;C:\\K\\tools\\windows\\admin-requests&quot;</Arguments>", "<ExecutionTimeLimit>PT2H</ExecutionTimeLimit>"} {
		if !strings.Contains(x, s) {
			t.Errorf("missing %s", s)
		}
	}
	if !tacheAdminAJourXML(x) {
		t.Fatal("up to date")
	}
	ancien := `<Exec><Command>powershell.exe</Command><Arguments>-NoProfile -File "C:\ProgramData\Athanor\admin-installer.ps1" -Dossier "x"</Arguments></Exec>`
	if tacheAdminAJourXML(ancien) {
		t.Fatal("the former script's task must be allowed again")
	}
	u := []byte{0xff, 0xfe}
	for _, r := range "<Command>Athanor.exe</Command> admin-tache" {
		u = append(u, byte(r), 0)
	}
	if !tacheAdminAJourXML(texteUTF16(u)) {
		t.Fatal("UTF-16 output")
	}
}
