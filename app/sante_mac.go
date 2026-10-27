package main

// Setup & health on macOS: the pilot runs on the Mac itself, Claude in Claude Code's own sandbox (Seatbelt),
// the packages come from Homebrew.

import "runtime"

// verificationsSysteme: the checks of this system's mode (native Windows has its own list, natif.go).
func verificationsSysteme() []*Verif {
	switch runtime.GOOS {
	case "darwin":
		return VerificationsMac()
	case "linux":
		return VerificationsLinux() // the Linux app: the pilot runs here, never in WSL
	}
	return Verifications() // Windows: the WSL mode (native Windows has its own list, natif.go)
}

func VerificationsMac() []*Verif {
	v := func(id, titre, explication string, facultatif bool, rep Reparation) *Verif {
		return nouvelleVerif("verif-", id, titre, explication, facultatif, rep)
	}
	installer := Reparation{Genre: "fenetre", Titre: "Package installation", Args: []string{"installer"}, Explication: "Opens a Terminal window: Homebrew installs the packages and Claude Code (your password may be asked once)."}
	return []*Verif{
		v("macos", "macOS", "The pilot runs on this Mac, Claude in its sandbox (Seatbelt).", false, Reparation{Genre: "aucune"}),
		v("internet", "Internet", "Claude must reach claude.ai.", false, Reparation{Genre: "aucune"}),
		v("paquets", "Basic packages (Homebrew)", "git, jq, curl, node, npm, ripgrep, ImageMagick, ffmpeg and the GitHub CLI (GitHub push), installed with Homebrew.", false, installer),
		v("barriere", "System sandbox", "Seatbelt (sandbox-exec), which keeps Claude and the MCP servers inside the project. Without it, the pilot refuses to start.", false, Reparation{Genre: "aucune"}),
		v("claude", "Claude Code", "The claude program (~/.local/bin).", false, reparationKit("claude")),
		v("jeton", "Token and model access", "The token of your subscription (CLAUDE_CODE_OAUTH_TOKEN in .env) is accepted: a tiny Haiku session answers.", false, reparationJeton),
		v("ponytail", "Ponytail, the simplest code", "Built in: Claude builds each milestone with the smallest complete change, never less than the request (Settings: Simplest code).", true, reparationKit("ponytail")),
		v("outils_pc", "PC software detected", "Unreal, Blender, Roblox Studio, Xcode, the Android SDK… found on this Mac and made available to Claude.", true, Reparation{Genre: "aucune"}),
		v("outils_kit", "Athanor's files", "Athanor's executable passes its self-test and its texts for Claude (template/) are in place.", false, reparationKit("outils_kit")),
	}
}
