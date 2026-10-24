//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// athanor detecter on a fake Mac: the applications in their usual places, the lines Claude reads.
func TestDetecterMac(t *testing.T) {
	r := t.TempDir()
	maison := filepath.Join(r, "Users", "moi")
	kit := filepath.Join(maison, "Athanor")
	for _, d := range []string{
		"Users/Shared/Epic Games/UE_5.3/Engine/Binaries/Mac/UnrealEditor.app", "Users/Shared/Epic Games/UE_5.10/Engine/Binaries/Mac/UnrealEditor.app",
		"Applications/Blender.app/Contents/MacOS", "Applications/RobloxStudio.app", "Applications/Xcode.app",
		"Applications/Android Studio.app/Contents/jbr/Contents/Home/bin", "Users/moi/Library/Android/sdk/platform-tools", "Users/moi/Athanor/system",
	} {
		_ = os.MkdirAll(filepath.Join(r, d), 0o755)
	}
	for _, f := range []string{"Applications/Blender.app/Contents/MacOS/Blender", "Applications/Android Studio.app/Contents/jbr/Contents/Home/bin/java"} {
		_ = os.WriteFile(filepath.Join(r, f), []byte("x"), 0o755)
	}
	t.Setenv("ATHANOR_TEST_MAC", "1")
	t.Setenv("ATHANOR_RACINE_MAC", r)
	t.Setenv("HOME", maison)
	t.Setenv("PROJECT", filepath.Join(r, "projet"))
	for _, v := range []string{"UE_DIR", "BLENDER_EXE", "ANDROID_SDK_DIR", "ANDROID_JDK_DIR"} {
		t.Setenv(v, "")
	}
	if err := detecter(kit); err != nil {
		t.Fatal(err)
	}
	env, _ := os.ReadFile(filepath.Join(kit, "system", "pc_tools.env"))
	md, _ := os.ReadFile(filepath.Join(r, "projet", ".athanor", "PC_TOOLS.md"))
	for _, a := range []string{"UE_DIR='" + r + "/Users/Shared/Epic Games/UE_5.10'", "BLENDER_EXE='" + r + "/Applications/Blender.app/Contents/MacOS/Blender'",
		"ANDROID_SDK_DIR='" + maison + "/Library/Android/sdk'", "ANDROID_JDK_DIR='" + r + "/Applications/Android Studio.app/Contents/jbr/Contents/Home'", "WIN_PROFIL=''"} {
		if !strings.Contains(string(env), a) {
			t.Errorf("pc_tools.env: missing %s in\n%s", a, env)
		}
	}
	for _, a := range []string{"- **Unreal Engine**: UE_5.10", "UnrealEditor.app/Contents/MacOS/UnrealEditor", "open -a RobloxStudio", "open -a Blender",
		"- **Xcode**", "- **Android SDK**", "- **macOS**: you run on the Mac itself"} {
		if !strings.Contains(string(md), a) {
			t.Errorf("PC_TOOLS.md: missing %q in\n%s", a, md)
		}
	}
	for _, non := range []string{"winget", "unreal-editor", "roblox-studio [--wait]", "$TOOLS/windows"} {
		if strings.Contains(string(md), non) {
			t.Errorf("PC_TOOLS.md: %q on a Mac:\n%s", non, md)
		}
	}
}
