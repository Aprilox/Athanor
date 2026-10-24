package main

// Athanor.app, built anywhere (athanor construire --all): the bundle around one executable for both Mac
// processors (Apple Silicon and Intel). The release signs it on a Mac (an ad-hoc signature: macOS then
// asks once to allow it, see the README).

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const idBundleMac = "ai.athanor.app"

// fatMachO: one executable made of the given Mac executables (a "universal" file: a header, then each one
// at an offset aligned on 16 KB).
func fatMachO(sortie string, tranches ...string) error {
	type tranche struct {
		cpu, sous uint32
		donnees   []byte
	}
	var l []tranche
	for _, f := range tranches {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if len(b) < 12 || binary.LittleEndian.Uint32(b) != 0xfeedfacf { // MH_MAGIC_64
			return fmt.Errorf("%s: not a 64-bit Mac executable", filepath.Base(f))
		}
		l = append(l, tranche{binary.LittleEndian.Uint32(b[4:]), binary.LittleEndian.Uint32(b[8:]), b})
	}
	if len(l) == 0 {
		return errors.New("no executable")
	}
	const aligne = 14
	entete := make([]byte, 8+20*len(l))
	binary.BigEndian.PutUint32(entete, 0xcafebabe) // FAT_MAGIC
	binary.BigEndian.PutUint32(entete[4:], uint32(len(l)))
	position := uint32(len(entete))
	var corps []byte
	for i, t := range l {
		pas := uint32(1) << aligne
		debut := (position + pas - 1) / pas * pas
		corps = append(corps, make([]byte, debut-position)...)
		e := entete[8+20*i:]
		binary.BigEndian.PutUint32(e, t.cpu)
		binary.BigEndian.PutUint32(e[4:], t.sous)
		binary.BigEndian.PutUint32(e[8:], debut)
		binary.BigEndian.PutUint32(e[12:], uint32(len(t.donnees)))
		binary.BigEndian.PutUint32(e[16:], aligne)
		corps = append(corps, t.donnees...)
		position = debut + uint32(len(t.donnees))
	}
	return os.WriteFile(sortie, append(entete, corps...), 0o755)
}

// infoPlistMac: the bundle's description (name, version, icon; a normal application, in the Dock).
func infoPlistMac(version string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>Athanor</string>
	<key>CFBundleDisplayName</key><string>Athanor</string>
	<key>CFBundleIdentifier</key><string>` + idBundleMac + `</string>
	<key>CFBundleExecutable</key><string>athanor</string>
	<key>CFBundleIconFile</key><string>athanor</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>` + version + `</string>
	<key>CFBundleVersion</key><string>` + version + `</string>
	<key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
	<key>LSMinimumSystemVersion</key><string>11.0</string>
	<key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
	<key>NSHighResolutionCapable</key><true/>
	<key>NSHumanReadableCopyright</key><string>MIT License, Aprilox</string>
</dict>
</plist>
`
}

// construireAppMac: dist/Athanor.app from the two Mac executables (and the icon of winres/).
func construireAppMac(src, dist, version string) error {
	app := filepath.Join(dist, "Athanor.app")
	_ = os.RemoveAll(app)
	contenu := filepath.Join(app, "Contents")
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(contenu, d), 0o755); err != nil {
			return err
		}
	}
	if err := fatMachO(filepath.Join(contenu, "MacOS", "athanor"), filepath.Join(dist, "athanor-darwin-arm64"), filepath.Join(dist, "athanor-darwin-amd64")); err != nil {
		return err
	}
	icone, err := os.ReadFile(filepath.Join(src, "winres", "athanor.icns"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contenu, "Resources", "athanor.icns"), icone, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(contenu, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(contenu, "Info.plist"), []byte(infoPlistMac(version)), 0o644)
}
