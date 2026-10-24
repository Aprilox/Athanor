package main

// Setup & health on macOS: the pilot runs on the Mac itself, Claude in Claude Code's own sandbox (Seatbelt),
// the packages come from Homebrew.

import "runtime"

// verificationsSysteme: the checks of this system's mode (native Windows has its own list, natif.go).
func verificationsSysteme() []*Verif {
	if runtime.GOOS == "darwin" {
		return VerificationsMac()
	}
	return Verifications()
}

func VerificationsMac() []*Verif {
	v := func(id, titre, explication, script string, facultatif bool, rep Reparation) *Verif {
		return &Verif{ID: id, Titre: titre, Explication: explication, Script: script, Facultatif: facultatif, Reparation: rep, tache: NouvelleTache("verif-"+id, titre)}
	}
	installer := Reparation{Genre: "fenetre", Titre: "Package installation", Script: "bash commands/install.sh", Explication: "Opens a Terminal window: Homebrew installs the packages and Claude Code (your password may be asked once)."}
	return []*Verif{
		v("macos", "macOS", "The pilot runs on this Mac, Claude in its sandbox (Seatbelt).",
			`echo "macOS $(sw_vers -productVersion) ($(uname -m))"`, false, Reparation{Genre: "aucune"}),
		v("internet", "Internet", "Claude must reach claude.ai.",
			`curl -s -o /dev/null -m 10 -w 'claude.ai reachable (code %{http_code})\n' https://claude.ai/`, false, Reparation{Genre: "aucune"}),
		v("paquets", "Basic packages (Homebrew)", "git, jq, curl, node, npm, ripgrep, ImageMagick, ffmpeg and the GitHub CLI (GitHub push), installed with Homebrew.",
			`export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"; m=0; for p in brew git jq curl node npm rg magick ffmpeg gh; do if command -v $p >/dev/null 2>&1; then echo "ok  $p"; else echo "MISSING $p"; m=1; fi; done; exit $m`, false, installer),
		v("barriere", "System sandbox", "Seatbelt (sandbox-exec), which keeps Claude and the MCP servers inside the project. Without it, the pilot refuses to start.",
			`d=$(mktemp -d) && /usr/bin/sandbox-exec -p "(version 1)(allow default)(deny file-write* (subpath \"$d\"))" /usr/bin/touch "$d/x" 2>/dev/null; if [ -e "$d/x" ]; then echo 'sandbox-exec does NOT block writing'; rm -rf "$d"; exit 1; fi; rm -rf "$d"; echo 'Seatbelt OK'`, false, Reparation{Genre: "aucune"}),
		v("claude", "Claude Code", "The claude program (~/.local/bin).",
			`export PATH="$HOME/.local/bin:$PATH"; claude --version`, false,
			Reparation{Genre: "wsl", Script: `curl -fsSL https://claude.ai/install.sh | bash`}),
		v("jeton", "Token and model access", "The token of your subscription (CLAUDE_CODE_OAUTH_TOKEN in .env) is accepted: a tiny Haiku session answers.",
			scriptJeton, false, reparationJeton),
		v("outils_pc", "PC software detected", "Unreal, Blender, Roblox Studio, Xcode, the Android SDK… found on this Mac and made available to Claude.",
			`export PROJECT="$PWD/logs/detection"; mkdir -p "$PROJECT" tools; `+outilKit("detecter")+`"${X[@]}" >/dev/null 2>&1; sed -n '3,25p' "$PROJECT/.athanor/PC_TOOLS.md"`, true, Reparation{Genre: "aucune"}),
		v("outils_kit", "Tool scripts", "The pilot scripts are consistent and keep Linux line ends (Git Bash fails on CRLF).",
			`for f in system/pilot.sh commands/*.sh; do bash -n "$f" || { echo "Syntax ERROR: $f"; exit 1; }; done
./athanor autotest "$PWD" >/dev/null 2>&1 || { echo 'athanor ERROR: its autotest fails (Rebuild Athanor)'; exit 1; }
echo 'scripts OK'`, false, Reparation{Genre: "aucune"}),
	}
}
