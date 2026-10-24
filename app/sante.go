package main

// Setup and health: checks, automatic repairs, and Claude repair sessions.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Reparation struct {
	Genre       string // wsl, fenetre, powershell, aucune
	Script      string
	Titre       string
	Explication string
}

type Verif struct {
	ID          string `json:"id"`
	Titre       string `json:"titre"`
	Explication string `json:"explication"`
	Script      string `json:"-"`
	Facultatif  bool   `json:"facultatif"`
	Reparation  Reparation

	tache      *Tache
	reparation *Tache
	claude     *Tache
	reverifier bool
	mu         sync.Mutex
}

type VerifVue struct {
	ID             string `json:"id"`
	Titre          string `json:"titre"`
	Explication    string `json:"explication"`
	Facultatif     bool   `json:"facultatif"`
	Statut         string `json:"statut"`
	Resume         string `json:"resume"`
	ReparationAuto bool   `json:"reparation_auto"`
	ReparationInfo string `json:"reparation_info"`
	EnReparation   bool   `json:"en_reparation"`
	TacheID        string `json:"tache_id"`
	ReparationID   string `json:"reparation_id"`
	ClaudeID       string `json:"claude_id"`
}

const chargerEnv = `export PATH="$HOME/.local/bin:$PWD/system/bin:$PATH"; if [ -f .env ]; then while IFS= read -r l || [ -n "$l" ]; do l=${l%$'\r'}; [[ "$l" =~ ^([A-Za-z_][A-Za-z0-9_]*)=(.*)$ ]] && export "${BASH_REMATCH[1]}=${BASH_REMATCH[2]}"; done < .env; fi; `

// the token check, the same in both modes (WSL, native Windows)
const scriptJeton = `grep -q '^CLAUDE_CODE_OAUTH_TOKEN=.\{20,\}' .env || { echo 'Token ABSENT from .env (Settings)'; exit 3; }
export PATH="$HOME/.local/bin:$PATH"
sortie=$(ENABLE_CLAUDEAI_MCP_SERVERS=false timeout 120 claude -p 'Answer exactly: TOKEN_OK' --model haiku --max-turns 1 < /dev/null 2>&1); code=$?
if printf '%s' "$sortie" | grep -q TOKEN_OK; then echo 'Connection OK'; else echo "Test session FAILED (code $code): $(printf '%s\n' "$sortie" | grep -v '^[[:space:]]*$' | tail -n 1 | cut -c1-200)"; echo '--- full output ---'; printf '%s\n' "$sortie" | tail -n 12; exit 1; fi`

var reparationJeton = Reparation{Genre: "fenetre", Titre: "Get a token", Script: `export PATH="$HOME/.local/bin:$PATH"; claude setup-token`, Explication: "Opens a window: sign in in the browser, then paste the token shown into Settings → CLAUDE_CODE_OAUTH_TOKEN."}

func Verifications() []*Verif {
	v := func(id, titre, explication, script string, facultatif bool, rep Reparation) *Verif {
		return &Verif{ID: id, Titre: titre, Explication: explication, Script: script, Facultatif: facultatif, Reparation: rep, tache: NouvelleTache("verif-"+id, titre)}
	}
	installer := Reparation{Genre: "powershell", Script: `commands\install.ps1`, Explication: "Opens the full installer in a window (WSL2, network, packages, Claude Code). May require a PC restart."}
	paquets := Reparation{Genre: "fenetre", Titre: "Package installation", Script: "bash commands/install.sh", Explication: "Opens a window: Linux asks for your password (sudo) to install the packages."}
	return []*Verif{
		v("wsl", "WSL and the Linux distribution", "The pilot runs in WSL2, the Linux built into Windows.",
			`echo "WSL_OK $(uname -r) $(grep PRETTY /etc/os-release | cut -d= -f2)"`, false, installer),
		v("internet", "Internet from WSL", "Claude must reach claude.ai from Linux.",
			`getent hosts claude.ai >/dev/null && curl -s -o /dev/null -m 10 -w 'claude.ai reachable (code %{http_code})\n' https://claude.ai/`, false, installer),
		v("paquets", "Basic Linux packages", "git, jq, curl, node, npm, bubblewrap, socat, ripgrep, ImageMagick, ffmpeg, zip, Xvfb (Claude's screen), ssh and the GitHub CLI (GitHub push).",
			`m=0; for p in git jq curl node npm bwrap socat rg convert ffmpeg zip Xvfb ssh gh; do if command -v $p >/dev/null 2>&1; then echo "ok  $p"; else echo "MISSING $p"; m=1; fi; done; exit $m`, false, paquets),
		v("barriere", "System sandbox", "The sandbox (bubblewrap + seccomp filter) that keeps Claude inside the project. Without it, the pilot refuses to start.",
			`bwrap --ro-bind / / --dev /dev --proc /proc true && echo 'bubblewrap OK' && { npm ls -g @anthropic-ai/sandbox-runtime >/dev/null 2>&1 && echo 'seccomp filter OK' || { echo 'seccomp filter MISSING'; exit 2; }; }`, false,
			Reparation{Genre: "fenetre", Titre: "System sandbox", Script: "bash commands/install.sh", Explication: "Reinstalls bubblewrap, the AppArmor profile and the seccomp filter (sudo password asked)."}),
		v("claude", "Claude Code", "The claude program in WSL (~/.local/bin).",
			`export PATH="$HOME/.local/bin:$PATH"; claude --version`, false,
			Reparation{Genre: "wsl", Script: `curl -fsSL https://claude.ai/install.sh | bash && { grep -q '.local/bin' ~/.bashrc || echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc; }`}),
		v("jeton", "Token and model access", "The token of your subscription (CLAUDE_CODE_OAUTH_TOKEN in .env) is accepted: a tiny Haiku session answers.",
			scriptJeton, false, reparationJeton),
		v("rtk", "RTK, output compression", "Optional: reduces what Claude reads for git, ls, grep…",
			`test -x system/bin/rtk && system/bin/rtk --version`, true,
			Reparation{Genre: "wsl", Script: `cd system/bin && curl -fsSL https://github.com/rtk-ai/rtk/releases/download/v0.50.0/rtk-x86_64-unknown-linux-musl.tar.gz | tar -xz rtk && chmod +x rtk && ./rtk --version`}),
		v("outils_pc", "PC software detected", "Unreal, Blender, Android SDK, Docker… found on this PC and made available to Claude (read-only).",
			`export PROJECT="$PWD/logs/detection"; mkdir -p "$PROJECT" tools; `+outilKit("detecter")+`"${X[@]}" >/dev/null 2>&1; sed -n '3,25p' "$PROJECT/.athanor/PC_TOOLS.md"`, true, Reparation{Genre: "aucune"}),
		v("docker", "Docker", "Optional: disposable images for Claude (tool IMAGE COMMAND). Docker Desktop → Settings → Resources → WSL integration.",
			`docker info >/dev/null 2>&1 && echo 'Docker reachable' || { echo 'Docker UNREACHABLE (optional)'; exit 1; }`, true, Reparation{Genre: "aucune"}),
		v("admin", "Administrator rights for installations", "Scheduled task that lets Claude install winget software and set the firewall without bothering you.",
			`S=/mnt/c/Windows/System32/schtasks.exe; [ -x $S ] || { echo 'schtasks UNREACHABLE'; exit 1; }; $S /query /tn 'Athanor - installations' >/dev/null 2>&1 && echo 'Task present' || { echo 'Task ABSENT: Claude can only install portable software'; exit 1; }`, true,
			Reparation{Genre: "powershell", Script: `commands\allow-admin.cmd`, Explication: "Windows will ask for your consent once (UAC) to create the installation task. Afterwards Claude installs and runs anything it needs with administrator rights, without ever prompting you."}),
		v("outils_kit", "Tool scripts", "The gateways (pc-exec, unreal-cmd, blender, tool…) and the pilot scripts are executable and consistent.",
			`chmod +x commands/*.sh system/*.sh system/bin/* 2>/dev/null
for f in system/pilot.sh commands/*.sh; do bash -n "$f" || { echo "Syntax ERROR: $f"; exit 1; }; done
./athanor autotest "$PWD" >/dev/null 2>&1 || { echo 'athanor ERROR: its autotest fails (Rebuild Athanor)'; exit 1; }
if grep -lq $'\r' system/pilot.sh commands/*.sh 2>/dev/null; then echo 'ERROR: Windows line endings (CRLF) in a script'; exit 1; fi
echo 'scripts OK'`, false,
			Reparation{Genre: "wsl", Script: `chmod +x commands/*.sh system/*.sh system/bin/* ; echo 'permissions restored'`}),
	}
}

func (v *Verif) Vue() VerifVue {
	t := v.tache.Vue(0)
	resume := ""
	if t.Statut == "echec" {
		for _, l := range t.Sortie {
			u := strings.ToUpper(l)
			if strings.Contains(u, "MISSING") || strings.Contains(u, "FAIL") || strings.Contains(u, "ERROR") || strings.Contains(u, "NOT FOUND") || strings.Contains(u, "ABSENT") || strings.Contains(u, "IMPOSSIBLE") || strings.Contains(u, "UNREACHABLE") || strings.Contains(u, "INVALID") {
				resume = l
				break
			}
		}
	}
	if resume == "" {
		for i := len(t.Sortie) - 1; i >= 0; i-- {
			if strings.TrimSpace(t.Sortie[i]) != "" {
				resume = t.Sortie[i]
				break
			}
		}
	}
	v.mu.Lock()
	enRep := (v.reparation != nil && v.reparation.EnCours()) || (v.claude != nil && v.claude.EnCours())
	repID, clID := "", ""
	if v.reparation != nil {
		repID = v.reparation.ID
	}
	if v.claude != nil {
		clID = v.claude.ID
	}
	v.mu.Unlock()
	return VerifVue{v.ID, v.Titre, v.Explication, v.Facultatif, t.Statut, resume, v.Reparation.Genre != "aucune", v.Reparation.Explication, enRep, v.tache.ID, repID, clID}
}

// Verifier starts the check in the tool folder (WSL), with the .env loaded.
func (v *Verif) Verifier(k Kit) {
	v.tache.Lancer(CommandeWSL(k, chargerEnv+v.Script), nil)
}

func (v *Verif) Reparer(k Kit) (string, error) {
	r := v.Reparation
	switch r.Genre {
	case "wsl":
		t := NouvelleTache("rep-"+v.ID, "Repair: "+v.Titre)
		v.mu.Lock()
		v.reparation = t
		v.mu.Unlock()
		t.Lancer(CommandeWSL(k, chargerEnv+r.Script), func(bool) { v.Verifier(k) })
		return "Repair started; the check will run again when it is done.", nil
	case "fenetre":
		if err := FenetreWSL(k, r.Titre, r.Script); err != nil {
			return "", err
		}
		return r.Explication + " Run the check again when it is done.", nil
	case "powershell":
		if err := FenetrePowerShell(k, r.Script); err != nil {
			return "", err
		}
		return r.Explication + " Run the check again when it is done.", nil
	}
	return "", fmt.Errorf("No automatic repair: ask Claude")
}

func (v *Verif) DemanderAClaude(k Kit, complement string) (*Tache, error) {
	sortie := v.tache.Texte()
	if len(sortie) > 6000 {
		sortie = sortie[len(sortie)-6000:]
	}
	precision := ""
	if strings.TrimSpace(complement) != "" {
		precision = "\n## Details from the user\n" + strings.TrimSpace(complement) + "\n"
	}
	consigne := fmt.Sprintf(`# Repair requested from the Athanor app

You are in the Athanor kit folder (WSL). A health check failed; diagnose and repair it, without asking questions.

## Check: %s
%s

## Command run (bash, in this folder)
`+"```"+`
%s
`+"```"+`

## Output
`+"```"+`
%s
`+"```"+`
%s
## Rules
- You may read and edit the kit files (system/, commands/, template/, .env except the token), install what is missing in ~/.local, $PWD/tools or with npm/pip/curl, and run the command again to verify.
- The app and the athanor sub-commands are Go (app/). If you change app/, rebuild with `+"`./athanor reparer --recompiler`"+` (WSL and Linux; `+"`./Athanor.exe reparer --recompiler`"+` in native Windows): it shows your change, checks it, builds, runs the autotest and only then replaces the executable (the previous one is kept, `+"`reparer --annuler`"+` puts it back). Never replace Athanor.exe or athanor by hand.
- sudo is not available in this session. If a step requires sudo or a Windows action (installer, UAC, restart), write the exact commands in logs/TODO_FOR_YOU.md and explain in two lines.
- Never read the value of the token and never print it.
- End with a summary of 5 lines at most: what was broken, what you did, what remains to be done. Answer in the user's language (code: %s).
`, v.Titre, v.Explication, v.Script, sortie, precision, langueDe(k))
	return lancerClaude(k, "claude-"+v.ID, "Claude repairs: "+v.Titre, "repair-"+v.ID+".md", consigne, func(bool) { v.Verifier(k) }, func(t *Tache) { v.mu.Lock(); v.claude = t; v.mu.Unlock() })
}

func lancerClaude(k Kit, id, nom, fichier, consigne string, fin func(bool), enregistrer func(*Tache)) (*Tache, error) {
	if err := os.MkdirAll(k.Journaux(), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(k.Journaux(), fichier), []byte(consigne), 0o644); err != nil {
		return nil, err
	}
	t := NouvelleTache(id, nom)
	if enregistrer != nil {
		enregistrer(t)
	}
	t.Lancer(CommandeWSL(k, "bash commands/repair.sh logs/"+fichier), fin)
	return t, nil
}

// Free-form request ("install Godot 4", "why does the pilot stop?").
func DemandeLibre(k Kit, texte string) (*Tache, error) {
	consigne := fmt.Sprintf(`# Request from the Athanor app

You are in the Athanor kit folder (WSL), outside the sandbox. The user asks:

%s

Rules: act without asking questions; after a change of the Go sources in app/, rebuild with `+"`./athanor reparer --recompiler`"+` (`+"`./Athanor.exe reparer --recompiler`"+` in native Windows), never by hand; install what is missing in ~/.local, $PWD/tools (portable) or with npm/pip/curl; for Windows software, use system/bin/install-windows ID (winget) when available, otherwise write the steps in logs/TODO_FOR_YOU.md; sudo is not available (write the commands to run in that same file); never read or print the token; end with a summary of 5 lines at most, in the user's language (code: %s).
`, strings.TrimSpace(texte), langueDe(k))
	id := fmt.Sprintf("libre-%d", time.Now().UnixNano())
	return lancerClaude(k, id, "Claude: "+court(texte, 60), id+".md", consigne, nil, nil)
}

// langueDe reads LANGUAGE from the tool's .env (written by the app); "en" by default.
func langueDe(k Kit) string {
	for _, l := range LireEnv(k.Env()) {
		if l.Cle == "LANGUAGE" && languesConnues[l.Valeur] {
			return l.Valeur
		}
	}
	return "en"
}

func TestBarriere(k Kit) *Tache {
	t := NouvelleTache("barriere-test", "Real sandbox test")
	t.Lancer(CommandeWSL(k, "bash commands/test-sandbox.sh"), nil)
	return t
}
