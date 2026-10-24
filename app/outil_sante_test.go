package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Every check of each system's list has its work in athanor sante, and every automatic repair names a
// sub-command that exists (fork A's Windows ones excepted when they are not built yet).
func TestSanteListesCompletes(t *testing.T) {
	for goos, liste := range map[string][]*Verif{"linux": Verifications(), "windows": VerificationsNatives(), "darwin": VerificationsMac()} {
		for _, v := range liste {
			if len(v.Args) != 2 || v.Args[0] != "sante" || v.Args[1] != v.ID {
				t.Fatalf("%s %s: args %q", goos, v.ID, v.Args)
			}
			if verifSante(goos, v.ID) == nil {
				t.Fatalf("%s: no athanor sante %s", goos, v.ID)
			}
			r := v.Reparation
			switch r.Genre {
			case "aucune":
			case "kit", "fenetre", "windows":
				if len(r.Args) == 0 {
					t.Fatalf("%s %s: repair without a command", goos, v.ID)
				}
				if r.Genre == "kit" && r.Args[0] == "sante" && (len(r.Args) != 3 || r.Args[1] != "--reparer") {
					t.Fatalf("%s %s: %q", goos, v.ID, r.Args)
				}
				if _, ok := sousCommandes[r.Args[0]]; !ok && r.Genre != "windows" {
					t.Fatalf("%s %s: unknown sub-command %q", goos, v.ID, r.Args[0])
				}
			default:
				t.Fatalf("%s %s: genre %q", goos, v.ID, r.Genre)
			}
		}
	}
	if verifSante("linux", "git_bash") != nil || verifSante("windows", "barriere") != nil {
		t.Fatal("a check of another system")
	}
}

func TestChargerEnvSante(t *testing.T) {
	kit := t.TempDir()
	os.WriteFile(filepath.Join(kit, ".env"), []byte("A_SANTE=1\r\n# x=y\nB_SANTE=$(rm -rf /)\nmauvais ligne\nC_SANTE=a=b\n"), 0o600)
	t.Setenv("A_SANTE", "avant")
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/maison")
	chargerEnvSante(kit)
	if os.Getenv("A_SANTE") != "1" || os.Getenv("B_SANTE") != "$(rm -rf /)" || os.Getenv("C_SANTE") != "a=b" {
		t.Fatalf("%q %q %q", os.Getenv("A_SANTE"), os.Getenv("B_SANTE"), os.Getenv("C_SANTE"))
	}
	sep := string(os.PathListSeparator)
	if runtime.GOOS != "windows" && os.Getenv("PATH") != "/maison/.local/bin"+sep+filepath.Join(kit, "system", "bin")+sep+"/usr/bin" {
		t.Fatalf("PATH %q", os.Getenv("PATH"))
	}
}

func TestImprimerLignes(t *testing.T) {
	var b bytes.Buffer
	ancien := sortieSante
	sortieSante = &b
	defer func() { sortieSante = ancien }()
	imprimerLignes("1\n2\n3\n4\n5\n", 2, 4)
	if b.String() != "2\n3\n4\n" {
		t.Fatalf("%q", b.String())
	}
}

func TestAjouterPathBashrc(t *testing.T) {
	f := filepath.Join(t.TempDir(), ".bashrc")
	os.WriteFile(f, []byte("alias x=y"), 0o644)
	ajouterPathBashrc(f)
	ajouterPathBashrc(f)
	b, _ := os.ReadFile(f)
	if string(b) != "alias x=y\nexport PATH=\"$HOME/.local/bin:$PATH\"\n" {
		t.Fatalf("%q", b)
	}
}

func TestSanteJetonAbsent(t *testing.T) {
	kit := t.TempDir()
	var b bytes.Buffer
	ancien := sortieSante
	sortieSante = &b
	defer func() { sortieSante = ancien }()
	os.WriteFile(filepath.Join(kit, ".env"), []byte("CLAUDE_CODE_OAUTH_TOKEN=court\n"), 0o600)
	if c := santeJeton(kit); c != 3 || b.String() != "Token ABSENT from .env (Settings)\n" {
		t.Fatalf("%d %q", c, b.String())
	}
}

// The Claude repair session: the instruction, the arguments of repair.sh, the log, the note for the user.
func TestReparationClaude(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script as the fake claude")
	}
	kit, maison := t.TempDir(), t.TempDir()
	bin := filepath.Join(maison, ".local", "bin")
	os.MkdirAll(bin, 0o755)
	trace := filepath.Join(kit, "trace")
	os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n[ \"$1\" = --help ] && { echo '--strict-mcp-config'; exit 0; }\n"+
		"printf '%s\\n' \"$@\" > "+trace+"\necho \"$CLAUDE_CODE_DISABLE_BACKGROUND_TASKS $ENABLE_CLAUDEAI_MCP_SERVERS\" >> "+trace+
		"\necho '{\"type\":\"result\"}'\nmkdir -p logs; echo x > logs/TODO_FOR_YOU.md\nexit 4\n"), 0o755)
	os.MkdirAll(filepath.Join(kit, "logs"), 0o755)
	os.WriteFile(filepath.Join(kit, "logs", "r.md"), []byte("Fix it"), 0o644)
	os.WriteFile(filepath.Join(kit, ".env"), []byte("MODEL=m1\nFALLBACK_MODEL=m2\n"), 0o600)
	t.Setenv("HOME", maison)
	t.Setenv("REPAIR_MODEL", "")
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir(kit)
	if c := lancerReparation([]string{"logs/r.md"}); c != 4 {
		t.Fatalf("code %d", c)
	}
	b, _ := os.ReadFile(trace)
	attendu := "-p\nFix it\n--permission-mode\nbypassPermissions\n--permission-prompts\nnone\n--output-format\nstream-json\n--verbose\n--max-turns\n80\n--model\nm1\n--strict-mcp-config\n--fallback-model\nm2\n1 false\n"
	if string(b) != attendu {
		t.Fatalf("args:\n%s", b)
	}
	if j, _ := os.ReadFile(filepath.Join(kit, "logs", "reparations.jsonl")); string(j) != "{\"type\":\"result\"}\n" {
		t.Fatalf("log %q", j)
	}
	if c := lancerReparation([]string{"logs/absent.md"}); c != 1 {
		t.Fatalf("missing file: %d", c)
	}
}

func TestReparerOutilsKit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute bits")
	}
	kit := t.TempDir()
	os.WriteFile(filepath.Join(kit, "athanor"), []byte("x"), 0o644)
	var b bytes.Buffer
	ancien := sortieSante
	sortieSante = &b
	defer func() { sortieSante = ancien }()
	if reparerOutilsKit(kit) != 0 || !strings.Contains(b.String(), "permissions restored") {
		t.Fatal(b.String())
	}
	if st, _ := os.Stat(filepath.Join(kit, "athanor")); st.Mode()&0o111 != 0o111 {
		t.Fatalf("mode %v", st.Mode())
	}
}
