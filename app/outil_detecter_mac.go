package main

// athanor detecter on macOS: the applications Claude can use (Unreal, Blender, Roblox Studio, Xcode, Godot)
// and the Android SDK, in their usual places. Written without a build constraint: tested everywhere on a
// fake root (ATHANOR_TEST_MAC, ATHANOR_RACINE_MAC).

import (
	"os"
	"path"
	"path/filepath"
)

type logicielsDuMac struct {
	ue, blender, sdk, jdk, roblox, xcode, godot string
}

// racineMac: "/" on a Mac; a fake root in the tests.
func racineMac() string {
	if r := os.Getenv("ATHANOR_RACINE_MAC"); r != "" {
		return r
	}
	return "/"
}

func detecterMac(racine, maison string) logicielsDuMac {
	var r logicielsDuMac
	app := func(p string) string { return filepath.Join(racine, p) }
	var ues []string
	for _, d := range []string{"Users/Shared/Epic Games", "Applications/Epic Games"} {
		for _, u := range globPrefixe(app(d), "UE_") {
			if estD(u + "/Engine/Binaries/Mac/UnrealEditor.app") {
				ues = append(ues, u)
			}
		}
	}
	r.ue = plusRecent(ues)
	for _, b := range []string{"Applications/Blender.app/Contents/MacOS/Blender"} {
		if estF(app(b)) {
			r.blender = app(b)
		}
	}
	if estD(app("Applications/RobloxStudio.app")) {
		r.roblox = app("Applications/RobloxStudio.app")
	}
	if estD(app("Applications/Xcode.app")) {
		r.xcode = app("Applications/Xcode.app")
	}
	if estD(app("Applications/Godot.app")) {
		r.godot = app("Applications/Godot.app/Contents/MacOS/Godot")
	}
	if maison != "" {
		for _, d := range []string{filepath.Join(maison, "Library/Android/sdk")} {
			if estD(d+"/platform-tools") || estD(d+"/cmdline-tools") {
				r.sdk = d
				break
			}
		}
	}
	for _, j := range []string{"Applications/Android Studio.app/Contents/jbr/Contents/Home"} {
		if estF(app(j) + "/bin/java") {
			r.jdk = app(j)
		}
	}
	return r
}

// lignesMac: the PC_TOOLS.md lines of a Mac: the programs run directly (no gateway: they are the Mac's own).
func lignesMac(l func(string), ue, blender, sdk, jdk, roblox, xcode, godot string) {
	if ue != "" {
		l("- **Unreal Engine**: " + path.Base(ue))
		l("  - `\"" + ue + "/Engine/Binaries/Mac/UnrealEditor.app/Contents/MacOS/UnrealEditor\" Game.uproject`: the editor; add `-ExecCmds=\"...\" -unattended -nullrhi` for commandlets and tests without a window")
		l("  - `\"" + ue + "/Engine/Build/BatchFiles/RunUAT.sh\" BuildCookRun -project=$PWD/Game.uproject -platform=Mac -clientconfig=Shipping -build -cook -stage -pak -archive -archivedirectory=deliverable`: Mac game")
		if os.Getenv("UNREAL_MCP") == "1" {
			l("  - **Unreal MCP active**: `mcp__unreal__*` tools when the editor is open (the pilot opens it before each cycle if a .uproject exists)")
		}
	} else {
		l("- Unreal Engine: not detected")
	}
	if roblox != "" {
		l("- **Roblox Studio**: `open -a RobloxStudio PLACE.rbxl` opens it on a place of the project")
		if os.Getenv("ROBLOX_MCP") == "1" {
			l("  - **Roblox Studio MCP active**: `mcp__roblox-studio__*` tools on the open Studio (the pilot opens it before each cycle; Rojo project: `.athanor/studio/SYNC.luau` updates its scripts live)")
		}
	}
	if blender != "" {
		l("- **Blender**: `\"" + blender + "\" -b -P script.py` (headless), `open -a Blender FILE.blend` (its window, for the Blender MCP)")
	} else {
		l("- Blender: not detected")
	}
	if godot != "" {
		l("- **Godot**: `\"" + godot + "\" --headless --path . ...`")
	}
	if xcode != "" {
		l("- **Xcode**: `xcodebuild`, `xcrun simctl` (iOS simulators)")
	} else {
		l("- Xcode: not installed (iOS builds impossible)")
	}
	if sdk != "" {
		l("- **Android SDK**: `" + sdk + "`; `\"" + sdk + "/platform-tools/adb\" devices`, `adb install -r game.apk` (phone plugged in over USB)")
	} else {
		l("- Android SDK: missing (install it as a portable copy in `$TOOLS/android-sdk`, see PC_TOOLS_GUIDE.md)")
	}
	if jdk != "" {
		l("- **Java for Android**: `" + jdk + "`")
	}
	l("- **macOS**: you run on the Mac itself, in a sandbox (Seatbelt): the applications above run directly from your shell; portable programs go in `$TOOLS`")
}
