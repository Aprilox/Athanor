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
	Genre       string   // kit (athanor ARGS on the pilot's side, then the check again), fenetre, windows, aucune
	Args        []string // the athanor sub-command and its arguments
	Titre       string
	Explication string
}

type Verif struct {
	ID          string   `json:"id"`
	Titre       string   `json:"titre"`
	Explication string   `json:"explication"`
	Args        []string `json:"-"` // athanor sante ID: the check, run on the pilot's side
	Facultatif  bool     `json:"facultatif"`
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


// reparationJeton: Claude Code's sign-in in a window, which shows the subscription's token.
var reparationJeton = Reparation{Genre: "fenetre", Titre: "Get a token", Args: []string{"jeton-claude"}, Explication: "Opens a window: sign in in the browser, then paste the token shown into Settings → CLAUDE_CODE_OAUTH_TOKEN."}

// nouvelleVerif: a check whose work is athanor sante ID (outil_sante.go) on the pilot's side.
func nouvelleVerif(prefixe, id, titre, explication string, facultatif bool, rep Reparation) *Verif {
	return &Verif{ID: id, Titre: titre, Explication: explication, Args: []string{"sante", id}, Facultatif: facultatif, Reparation: rep, tache: NouvelleTache(prefixe+id, titre)}
}

// reparationKit: the automatic repair athanor sante --reparer ID, then the check again.
func reparationKit(id string) Reparation {
	return Reparation{Genre: "kit", Args: []string{"sante", "--reparer", id}}
}

func Verifications() []*Verif {
	v := func(id, titre, explication string, facultatif bool, rep Reparation) *Verif {
		return nouvelleVerif("verif-", id, titre, explication, facultatif, rep)
	}
	installer := Reparation{Genre: "windows", Titre: "Installation", Args: []string{"installer-windows"}, Explication: "Opens the full installer in a window (WSL2, network, packages, Claude Code). May require a PC restart."}
	paquets := Reparation{Genre: "fenetre", Titre: "Package installation", Args: []string{"installer"}, Explication: "Opens a window: Linux asks for your password (sudo) to install the packages."}
	return []*Verif{
		v("wsl", "WSL and the Linux distribution", "The pilot runs in WSL2, the Linux built into Windows.", false, installer),
		v("internet", "Internet from WSL", "Claude must reach claude.ai from Linux.", false, installer),
		v("paquets", "Basic Linux packages", "git, jq, curl, node, npm, bubblewrap, socat, ripgrep, ImageMagick, ffmpeg, zip, Xvfb (Claude's screen), ssh and the GitHub CLI (GitHub push).", false, paquets),
		v("barriere", "System sandbox", "The sandbox (bubblewrap + seccomp filter) that keeps Claude inside the project. Without it, the pilot refuses to start.", false,
			Reparation{Genre: "fenetre", Titre: "System sandbox", Args: []string{"installer"}, Explication: "Reinstalls bubblewrap, the AppArmor profile and the seccomp filter (sudo password asked)."}),
		v("claude", "Claude Code", "The claude program in WSL (~/.local/bin).", false, reparationKit("claude")),
		v("jeton", "Token and model access", "The token of your subscription (CLAUDE_CODE_OAUTH_TOKEN in .env) is accepted: a tiny Haiku session answers.", false, reparationJeton),
		v("rtk", "RTK, output compression", "Optional: reduces what Claude reads for git, ls, grep…", true, reparationKit("rtk")),
		v("outils_pc", "PC software detected", "Unreal, Blender, Android SDK, Docker… found on this PC and made available to Claude (read-only).", true, Reparation{Genre: "aucune"}),
		v("docker", "Docker", "Optional: disposable images for Claude (tool IMAGE COMMAND). Docker Desktop → Settings → Resources → WSL integration.", true, Reparation{Genre: "aucune"}),
		v("admin", "Administrator rights for installations", "Scheduled task that lets Claude install winget software and set the firewall without bothering you.", true,
			Reparation{Genre: "windows", Titre: "Administrator rights", Args: []string{"autoriser-admin"}, Explication: "Windows will ask for your consent once (UAC) to create the installation task. Afterwards Claude installs and runs anything it needs with administrator rights, without ever prompting you."}),
		v("outils_kit", "Tool scripts", "The gateways (pc-exec, unreal-cmd, blender, tool…) and the pilot scripts are executable and consistent.", false, reparationKit("outils_kit")),
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

// Verifier starts the check in the tool folder, on the pilot's side (WSL, native Windows, Linux, macOS).
func (v *Verif) Verifier(k Kit) {
	v.tache.Lancer(commandeKit(k, v.Args...), nil)
}

func (v *Verif) Reparer(k Kit) (string, error) {
	r := v.Reparation
	switch r.Genre {
	case "kit":
		t := NouvelleTache("rep-"+v.ID, "Repair: "+v.Titre)
		v.mu.Lock()
		v.reparation = t
		v.mu.Unlock()
		t.Lancer(commandeKit(k, r.Args...), func(bool) { v.Verifier(k) })
		return "Repair started; the check will run again when it is done.", nil
	case "fenetre":
		if err := fenetreKit(k, r.Titre, r.Args...); err != nil {
			return "", err
		}
		return r.Explication + " Run the check again when it is done.", nil
	case "windows":
		if err := fenetreWindows(k, r.Titre, r.Args...); err != nil {
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

## Command run (in this folder; Athanor's executable, app/outil_sante.go)
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
`, v.Titre, v.Explication, ligneCommandeKit(v.Args), sortie, precision, langueDe(k))
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
	t.Lancer(commandeKit(k, "reparation", "logs/"+fichier), fin)
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
	t.Lancer(commandeKit(k, "tester-cage"), nil)
	return t
}

// ligneCommandeKit: how the command reads in the kit folder (Claude's repair instructions).
func ligneCommandeKit(args []string) string {
	exe := "./athanor"
	if modeNatif.Load() {
		exe = "./Athanor.exe"
	}
	return exe + " " + strings.Join(args, " ")
}
