package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The window guard's decisions: which processes are ours, which windows it hides, and never the ones
// Athanor opens on purpose nor the user's own.
func TestGardeFenetres(t *testing.T) {
	const s = int64(10_000_000) // one second, in Windows' 100 ns units
	procs := []processusGarde{
		{pid: 4, ppid: 0, exe: "System", cree: 1},
		{pid: 100, ppid: 4, exe: `C:\kit\Athanor.exe`, cree: 100 * s},                 // the pilot (racine)
		{pid: 110, ppid: 100, exe: `C:\kit\Athanor.exe`, cree: 110 * s},               // --isole
		{pid: 120, ppid: 110, exe: `C:\Users\u\.local\bin\claude.exe`, cree: 120 * s}, // Claude Code
		{pid: 130, ppid: 120, exe: `C:\WINDOWS\system32\cmd.exe`, cree: 945 * s},      // an MCP server's cmd
		{pid: 131, ppid: 130, exe: `C:\Program Files\nodejs\node.exe`, cree: 946 * s}, // and its node
		{pid: 132, ppid: 131, exe: `C:\WINDOWS\system32\conhost.exe`, cree: 946 * s},  // node's console host
		{pid: 140, ppid: 120, exe: `C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`, cree: 950 * s},
		{pid: 150, ppid: 100, exe: `C:\Program Files\Git\cmd\git.exe`, cree: 960 * s}, // the pilot's own git
		{pid: 151, ppid: 150, exe: `C:\WINDOWS\system32\conhost.exe`, cree: 960 * s},
		{pid: 200, ppid: 4, exe: `C:\WINDOWS\explorer.exe`, cree: 2 * s},         // the user's
		{pid: 210, ppid: 200, exe: `C:\WINDOWS\system32\cmd.exe`, cree: 990 * s}, // the user's own cmd
		{pid: 300, ppid: 999, exe: `C:\x\orphan.exe`, cree: 50 * s},              // a dead parent
		{pid: 310, ppid: 100, exe: `C:\x\older.exe`, cree: 10 * s},               // pid 100 given again: no child of the pilot
		{pid: 311, ppid: 310, exe: `C:\x\child-of-older.exe`, cree: 20 * s},
	}
	nous := descendantsGarde(procs, 100)
	for _, pid := range []uint32{110, 120, 130, 131, 132, 140, 150, 151} {
		if _, ok := nous[pid]; !ok {
			t.Errorf("%d not counted as ours", pid)
		}
	}
	for _, pid := range []uint32{100, 4, 200, 210, 300, 310, 311} {
		if _, ok := nous[pid]; ok {
			t.Errorf("%d counted as ours", pid)
		}
	}
	if len(descendantsGarde(procs, 12345)) != 0 || len(descendantsGarde(nil, 100)) != 0 {
		t.Error("descendants of nothing")
	}
	// a loop of parents (numbers given again) never hangs
	if n := descendantsGarde([]processusGarde{{pid: 1, ppid: 2}, {pid: 2, ppid: 1}}, 1); len(n) != 1 {
		t.Errorf("loop: %v", n)
	}

	maintenant := 960 * s
	cas := []struct {
		nom    string
		classe string
		pid    uint32
		titre  string
		cache  bool
		exe    string
	}{
		{"console of node (Windows names the program)", classeConsoleGarde, 131, "", true, "node.exe"},
		{"console of node (Windows names its host)", classeConsoleGarde, 132, "", true, "node.exe"},
		{"PowerShell's console", classeConsoleGarde, 140, `C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`, true, "powershell.exe"},
		{"the pilot's own git: started by the pilot itself (on purpose)", classeConsoleGarde, 150, "", false, ""},
		{"its host", classeConsoleGarde, 151, "", false, ""},
		{"Athanor's own console (installation, sign-in)", classeConsoleGarde, 110, "Installation", false, ""},
		{"the user's cmd", classeConsoleGarde, 210, "", false, ""},
		{"an unknown process", classeConsoleGarde, 777, "", false, ""},
		{"a Windows Terminal titled with PowerShell's path", classeTerminalGarde, 5000, `C:\WINDOWS\System32\WindowsPowerShell\v1.0\powershell.exe`, true, "powershell.exe"},
		{"with an Administrator prefix", classeTerminalGarde, 5000, `Administrator: C:\WINDOWS\System32\WindowsPowerShell\v1.0\POWERSHELL.EXE`, true, "powershell.exe"},
		{"cmd running npx", classeTerminalGarde, 5000, `C:\WINDOWS\system32\cmd.exe - npx -y @playwright/mcp@latest`, true, "cmd.exe"},
		{"the user's own terminal", classeTerminalGarde, 5000, "Windows PowerShell", false, ""},
		{"a program of ours started long ago", classeTerminalGarde, 5000, `C:\Users\u\.local\bin\claude.exe`, false, ""},
		{"a program that is not ours", classeTerminalGarde, 5000, `C:\WINDOWS\explorer.exe`, false, ""},
		{"Athanor's own title", classeTerminalGarde, 5000, `C:\kit\Athanor.exe`, false, ""},
		{"another class", "Chrome_WidgetWin_1", 131, "", false, ""},
	}
	for _, c := range cas {
		p, ok := fenetreAMasquerGarde(100, c.classe, c.pid, c.titre, nous, maintenant, 20*s)
		if ok != c.cache || (ok && nomExeGarde(p.exe) != c.exe) {
			t.Errorf("%s: %v %q", c.nom, ok, p.exe)
		}
	}
	p, _ := fenetreAMasquerGarde(100, classeConsoleGarde, 131, "", nous, maintenant, 20*s)
	if l := texteFenetreGarde(p, nous); l != "Windows: a window of node.exe (started by cmd.exe) opened on your screen: hidden, it goes on without it." {
		t.Error(l)
	}

	// the application's own hidden windows: one line each, the last 500 kept
	kit := t.TempDir()
	for i := 0; i < 510; i++ {
		noterFenetreKit(kit, "ligne")
	}
	b, _ := os.ReadFile(filepath.Join(kit, "logs", "fenetres-masquees.log"))
	if n := strings.Count(string(b), "\n"); n != 500 || !strings.HasSuffix(string(b), " ligne\n") {
		t.Errorf("%d lines", n)
	}
	noterFenetreKit("", "rien") // no kit: nothing, no panic
}
