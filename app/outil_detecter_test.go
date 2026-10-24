//go:build linux

package main

// athanor detecter against detect-tools.sh, on a fake Windows seen from WSL (drives in /mnt, cmd.exe,
// wslpath, powershell.exe, the tools): the same pc_tools.env and PC_TOOLS.md. /mnt is replaced in a
// mount namespace of the test only (unshare); the test is skipped where that is not allowed.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilevercmp(t *testing.T) {
	if _, err := exec.LookPath("sort"); err != nil {
		t.Skip("no sort")
	}
	l := []string{"UE_5.10", "UE_5.9", "UE_5.3", "UE_5.3.1", "UE_5", "UE_4.27", "Blender 4.2", "Blender 3.6", "Blender 10.0", "a.tar.gz", "a-1.2.tar.gz",
		"a-1.10.tar.gz", "x~1", "x", "x1", ".cache", ".", "..", "", "B1", "b1", "v1.2~rc1", "v1.2", "v1.2a", "0001", "1", "z.9", "z.10", "a.b.c", "A/UE_5.2/x", "A/UE_5.12/x"}
	out, err := exec.Command("bash", "-c", `printf '%s\n' "$@" | LC_ALL=C sort -V`, "_").Output()
	_ = out
	if err != nil {
		t.Skip(err)
	}
	c := exec.Command("bash", "-c", `LC_ALL=C sort -V`)
	c.Stdin = strings.NewReader(strings.Join(l, "\n") + "\n")
	attendu, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	g := append([]string(nil), l...)
	trierVersions(g)
	if strings.Join(g, "\n")+"\n" != string(attendu) {
		t.Errorf("sort -V:\n go %q\n sort %q", g, strings.Split(string(attendu), "\n"))
	}
}

func TestPariteDetecter(t *testing.T) {
	src, _ := filepath.Abs(filepath.Join(dossierOracles(), "detect-tools.sh")) // the former script, as a test oracle
	if _, err := os.Stat(src); err != nil {
		t.Skip("no detect-tools.sh")
	}
	if exec.Command("unshare", "-m", "--propagation", "private", "true").Run() != nil {
		t.Skip("no mount namespace here")
	}
	bin := binaireAthanor(t)
	d, _ := filepath.EvalSymlinks(t.TempDir())
	scenario := `set -u
D="$1"; SRC="$2"; BIN="$3"; CAS="$4"
mount -t tmpfs none /mnt || exit 99
f() { mkdir -p "$(dirname "$1")"; printf 'x' > "$1"; }
f "/mnt/c/Program Files/Epic Games/UE_5.3/Engine/Binaries/Win64/UnrealEditor.exe"
f "/mnt/c/Program Files/Epic Games/UE_5.10/Engine/Binaries/Win64/UnrealEditor.exe"
mkdir -p "/mnt/c/Program Files/Epic Games/UE_5.12"          # no editor: not a candidate
f "/mnt/d/Epic Games/UE_5.9/Engine/Binaries/Win64/UnrealEditor.exe"
f "/mnt/c/Program Files/Blender Foundation/Blender 3.6/blender.exe"
f "/mnt/c/Program Files/Blender Foundation/Blender 4.2/blender.exe"
f "/mnt/d/Program Files/Blender Foundation/Blender 10.1/blender.exe"
mkdir -p /mnt/e/Windows /mnt/f/rien
f "/mnt/c/Users/Moi/AppData/Local/Android/Sdk/platform-tools/adb.exe"
f "/mnt/c/Program Files/Android/Android Studio/jbr/bin/java.exe"
for v in version-a version-b version-c; do f "/mnt/c/Users/Moi/AppData/Local/Roblox/Versions/$v/RobloxStudioBeta.exe"; done
touch -d '2026-01-01' /mnt/c/Users/Moi/AppData/Local/Roblox/Versions/version-a/RobloxStudioBeta.exe
touch -d '2026-03-01' /mnt/c/Users/Moi/AppData/Local/Roblox/Versions/version-b/RobloxStudioBeta.exe
touch -d '2026-02-01' /mnt/c/Users/Moi/AppData/Local/Roblox/Versions/version-c/RobloxStudioBeta.exe
F="$D/faux"; mkdir -p "$F"
s() { printf '#!/bin/bash\n%s\n' "$2" > "$F/$1"; chmod +x "$F/$1"; }
case "$CAS" in sans-windows) ;; *)
s cmd.exe 'printf "C:\\\\Users\\\\Moi\\r\\n"'
s wslpath '[ "$1" = -u ] && [ "$2" = "C:\\Users\\Moi" ] && echo /mnt/c/Users/Moi'
s powershell.exe 'exit 0' ;; esac
case "$CAS" in sans-docker) s docker 'exit 1' ;; *) s docker '[ "$1" = info ] && exit 0' ;; esac
s node 'echo v20.11.1'
s npm 'printf "\033[1m10.2.4\033[0m\r\n"'
s python3 'case "$*" in "-m pip --version") echo "pip 24.0 from /usr/lib/python3/dist-packages/pip (python 3.12)" ;; *) echo "Python 3.12.3" ;; esac'
s php 'echo PHP'
s java 'echo "Picked up JAVA_TOOL_OPTIONS: -Dx=1.2" >&2; echo "openjdk version \"21.0.2\" 2024-01-16" >&2'
s go 'exit 1'
s rustc 'echo "rustc 1.79.0 (129f3b996 2024-06-10) and a very long line that goes on and on, éèà, beyond the ninety bytes kept by cut"'
s dotnet 'printf "\n\n8.0.100\n"'
s psql 'echo "psql (PostgreSQL) 16.3"'
s sqlite3 'exit 0'
s convert 'echo "Version: ImageMagick 6.9.12-98 Q16 x86_64"'
s ffmpeg 'echo "ffmpeg version 6.1.1-3ubuntu5 Copyright (c) 2000-2023"'
s git 'echo "git version 2.43.0"'
for m in sh go; do
  K="$D/$m"; mkdir -p "$K/system" "$K/tools/node22" "$K/tools/cache" "$K/tools/bin" "$K/tools/zzz" "$K/tools/windows/jdk-17/bin" "$K/logs"
  cp "$SRC" "$K/system/detect-tools.sh"
  printf 'x' > "$K/tools/bin/outil"; ln -sf outil "$K/tools/bin/lien"; mkdir -p "$K/tools/bin/dossier"; ln -sfn /tmp "$K/tools/lien-dossier"
  case "$CAS" in bureau) : > "$K/logs/bureau-isole" ;; esac
  E=(PATH="$F:/usr/bin:/bin" PROJECT="$K/projet" UNREAL_MCP=1 ROBLOX_MCP=1 LC_ALL=C)
  case "$CAS" in force) E+=(UE_DIR=/x/UE_9 BLENDER_EXE=/x/b.exe ANDROID_SDK_DIR=/x/sdk ANDROID_JDK_DIR=/x/jdk) ;; esac
  if [ $m = sh ]; then env -i "${E[@]}" bash "$K/system/detect-tools.sh"; else env -i "${E[@]}" "$BIN" detecter "$K"; fi
  echo "exit $m=$?"
done
`
	_ = os.WriteFile(filepath.Join(d, "scenario.sh"), []byte(scenario), 0o755)
	for _, cas := range []string{"wsl", "sans-windows", "sans-docker", "bureau", "force"} {
		dc := filepath.Join(d, cas)
		_ = os.MkdirAll(dc, 0o755)
		out, err := exec.Command("unshare", "-m", "--propagation", "private", "bash", filepath.Join(d, "scenario.sh"), dc, src, bin, cas).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "exit sh=0") || !strings.Contains(string(out), "exit go=0") {
			t.Fatalf("%s: %v %s", cas, err, out)
		}
		for _, f := range []string{"system/pc_tools.env", "projet/.athanor/PC_TOOLS.md"} {
			a, _ := os.ReadFile(filepath.Join(dc, "sh", f))
			b, _ := os.ReadFile(filepath.Join(dc, "go", f))
			na := strings.ReplaceAll(string(a), filepath.Join(dc, "sh"), "K")
			nb := strings.ReplaceAll(string(b), filepath.Join(dc, "go"), "K")
			if na != nb || len(na) < 20 {
				t.Errorf("%s %s:\n go %s\n sh %s", cas, f, nb, na)
			}
		}
	}
}
