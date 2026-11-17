package main

// A project's folder sent to the recycle bin: what may go, and the recycle bin the tests replace.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var corbeilleProjet = mettreALaCorbeille // a test sees what goes, without the real recycle bin

// controlerDossierSupprime: "" when d may go to the recycle bin, else why not.
func controlerDossierSupprime(d, kit string) string {
	if !filepath.IsAbs(d) {
		return "The project's folder path is not a full path"
	}
	if cle := dossierInterdit(d, kit); cle != "" {
		return cle
	}
	fi, err := os.Lstat(d)
	if errors.Is(err, os.ErrNotExist) {
		return "" // already gone: the project is still removed from Studio
	}
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return "The project's folder is a link or not a folder: it is never deleted"
	}
	reel := cheminReel(d)
	if runtime.GOOS == "windows" && !strings.EqualFold(reel, filepath.Clean(d)) || runtime.GOOS != "windows" && reel != filepath.Clean(d) {
		return "The project's folder leads elsewhere (a link or a junction): it is never deleted"
	}
	return ""
}
