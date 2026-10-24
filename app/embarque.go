package main

// The single executable (since 2.26): a release is one file per system. What a kit folder needs besides
// the executable travels inside it, as a tar.gz built by "athanor construire" (app/embarque/kit.tar.gz):
// the texts for Claude (template/), the settings model (env.example), the licence, and the Go sources of
// app/, written into the folder only when a repair needs them ("Rebuild Athanor", Claude's repairs). The Windows executable also carries the Linux one of the
// WSL mode (embarque/athanor-linux.gz). A plain "go build" carries none of it and behaves as before
// (the tests, a copy of the source code used in place).
//
// Started from anywhere but a kit folder, the executable installs or updates Athanor's folder (the user's
// data folder, dossier_athanor.go: created only once the user accepted) and runs the copy kept there.
// Started from a kit folder (that copy, or an older installation where the executable was replaced),
// it brings the folder's files up to its own version when the application opens.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// kitEmbarque: this executable carries the kit's files (a build of "athanor construire" or a release).
func kitEmbarque() bool { return len(archiveKitGz) > 0 }

// exeDuKit: the executable of Athanor's folder d (Athanor.exe in Windows, athanor elsewhere).
func exeDuKit(d string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(d, "Athanor.exe")
	}
	return filepath.Join(d, "athanor")
}

// ecrireAtomique: the file written aside then renamed over dest. In Windows a running executable cannot be
// replaced but can be renamed: it is moved out of the way first (dest.ancien, removed at a later update).
func ecrireAtomique(dest string, contenu []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	t, err := os.CreateTemp(filepath.Dir(dest), ".nouveau-*")
	if err != nil {
		return err
	}
	_, err = t.Write(contenu)
	if e := t.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = os.Chmod(t.Name(), mode)
	}
	if err == nil {
		if err = os.Rename(t.Name(), dest); err != nil && runtime.GOOS == "windows" {
			ancien := dest + ".ancien"
			_ = os.Remove(ancien)
			if os.Rename(dest, ancien) == nil {
				err = os.Rename(t.Name(), dest)
			}
		}
	}
	if err != nil {
		os.Remove(t.Name())
	}
	return err
}

// fichiersArchive: the files of a tar.gz ("/"-separated relative names → content and mode), refusing
// any name that would leave the folder.
func fichiersArchive(gz []byte) (map[string][]byte, map[string]os.FileMode, error) {
	z, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, nil, err
	}
	tr := tar.NewReader(z)
	out, modes := map[string][]byte{}, map[string]os.FileMode{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		nom := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if nom == "." || strings.HasPrefix(nom, "../") || strings.HasPrefix(nom, "/") || strings.Contains(nom, "\\") {
			return nil, nil, fmt.Errorf("invalid name in the archive: %q", h.Name)
		}
		b, err := io.ReadAll(io.LimitReader(tr, 64<<20))
		if err != nil {
			return nil, nil, err
		}
		out[nom], modes[nom] = b, os.FileMode(h.Mode)&0o755|0o644
	}
	return out, modes, nil
}

// remplacerDossier: dest replaced by the files given (relative to dest), written aside first so that a
// failure leaves the previous folder as it was.
func remplacerDossier(dest string, fichiers map[string][]byte, modes map[string]os.FileMode) error {
	tmp := dest + ".nouveau"
	_ = os.RemoveAll(tmp)
	for rel, b := range fichiers {
		c := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(c), 0o755); err != nil {
			os.RemoveAll(tmp)
			return err
		}
		m := modes[rel]
		if m == 0 {
			m = 0o644
		}
		if err := os.WriteFile(c, b, m); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	ancien := dest + ".ancien"
	_ = os.RemoveAll(ancien)
	if _, err := os.Lstat(dest); err == nil {
		if err := os.Rename(dest, ancien); err != nil {
			os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Rename(ancien, dest)
		os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(ancien)
	return nil
}

// sourceDuCode: d is a copy of the source code (a git repository): its files are the reference, never
// replaced by the ones built in.
func sourceDuCode(d string) bool {
	st, err := os.Stat(filepath.Join(d, ".git"))
	return err == nil && (st.IsDir() || st.Mode().IsRegular())
}

// pilotesEnCours: the projects whose pilot runs from kit d (its logs/pilotes/*/pilot.pid).
func pilotesEnCours(d string) []string {
	l, _ := filepath.Glob(filepath.Join(d, "logs", "pilotes", "*", "pilot.pid"))
	var noms []string
	for _, f := range l {
		noms = append(noms, filepath.Base(filepath.Dir(f)))
	}
	return noms
}

// ResultatDeploiement: what deployerKit did.
type ResultatDeploiement struct {
	Fichiers bool // template/, app/, .env.example, LICENSE written
	Exe      bool // the kit's executable replaced by this one
	Linux    bool // Windows: the Linux executable of the WSL mode written
	Reporte  bool // Windows: the Linux executable waits for the running pilots to stop
}

// deployerKit: Athanor's folder d brought to this executable's version: its files when the version changes
// (never in a copy of the source code), its executable when this one is newer than the folder's (a
// "Rebuild Athanor" of the same version is kept), and in Windows the Linux executable, unless a pilot of
// this kit runs (system/athanor-linux.attente: it is written at the next opening, or before a pilot
// starts). The version is recorded once everything is in place, so that what failed is tried again.
func deployerKit(d string) (ResultatDeploiement, error) {
	var r ResultatDeploiement
	if !kitEmbarque() {
		return r, errors.New("this executable carries no kit files (built without athanor construire)")
	}
	for _, s := range []string{"system", "logs"} {
		if err := os.MkdirAll(filepath.Join(d, s), 0o755); err != nil {
			return r, err
		}
	}
	avant := versionKit(d)
	nouvelle := avant != versionAthanor
	attente := filepath.Join(d, "system", "athanor-linux.attente") // the Linux executable left for later
	_, errAttente := os.Stat(attente)
	linuxVoulu := runtime.GOOS == "windows" && len(athanorLinuxGz) > 0
	if !nouvelle && estDossierKit(d) && fichierRegulierPilote(exeDuKit(d)) && (!linuxVoulu || (errAttente != nil && fichierRegulierPilote(filepath.Join(d, "athanor")))) {
		return r, nil
	}
	var premiere error
	noter := func(err error) {
		if err != nil && premiere == nil {
			premiere = err
		}
	}
	if !sourceDuCode(d) && (nouvelle || !estDossierKit(d)) {
		fichiers, modes, err := fichiersArchive(archiveKitGz)
		if err != nil {
			return r, err
		}
		parDossier := map[string]map[string][]byte{}
		for nom, b := range fichiers {
			haut, reste, ok := strings.Cut(nom, "/")
			switch {
			case ok && (haut == "template" || haut == "app"):
				if parDossier[haut] == nil {
					parDossier[haut] = map[string][]byte{}
				}
				parDossier[haut][reste] = b
			case !ok && nom == "env.example":
				noter(ecrireAtomique(filepath.Join(d, ".env.example"), b, 0o644))
			case !ok && nom == "LICENSE":
				noter(ecrireAtomique(filepath.Join(d, "LICENSE"), b, 0o644))
			}
		}
		// app/ (the sources) only where it already is: written when a repair needs it (extraireSources)
		if _, err := os.Stat(filepath.Join(d, "app", "go.mod")); err != nil {
			delete(parDossier, "app")
		}
		for haut, f := range parDossier {
			m := map[string]os.FileMode{}
			for rel := range f {
				m[rel] = modes[haut+"/"+rel]
			}
			noter(remplacerDossier(filepath.Join(d, haut), f, m))
		}
		r.Fichiers = premiere == nil
	}
	// its own executable, when the folder's is missing or older
	if exe, err := os.Executable(); err == nil {
		cible := exeDuKit(d)
		if !memeFichier(exe, cible) && (!fichierRegulierPilote(cible) || versionPlusRecente(versionAthanor, avant)) {
			b, err := os.ReadFile(exe)
			if err == nil {
				err = ecrireAtomique(cible, b, 0o755)
			}
			noter(err)
			r.Exe = err == nil
		}
	}
	// Windows: the Linux executable of the WSL mode (a WSL pilot runs it: never replaced under it)
	if linuxVoulu {
		l := filepath.Join(d, "athanor")
		if !fichierRegulierPilote(l) || nouvelle || errAttente == nil {
			if fichierRegulierPilote(l) && len(pilotesEnCours(d)) > 0 {
				r.Reporte = true
				noter(ecrireAtomique(attente, []byte(versionAthanor+"\n"), 0o644))
			} else {
				z, err := gzip.NewReader(bytes.NewReader(athanorLinuxGz))
				var b []byte
				if err == nil {
					b, err = io.ReadAll(z)
				}
				if err == nil {
					err = ecrireAtomique(l, b, 0o755)
				}
				noter(err)
				r.Linux = err == nil
				if r.Linux {
					_ = os.Remove(attente)
				}
			}
		}
	}
	for _, f := range []string{exeDuKit(d) + ".ancien", filepath.Join(d, "athanor") + ".ancien"} {
		_ = os.Remove(f) // left by an earlier update, when nothing runs it any more
	}
	if premiere == nil && nouvelle {
		premiere = ecrireAtomique(filepath.Join(d, "system", "version.txt"), []byte(versionAthanor+"\n"), 0o644)
	}
	return r, premiere
}

// extraireSources: <kit>/app, the Go sources this executable was built from, for a repair (athanor
// reparer, a repair by Claude). Nothing when they are there already, in a copy of the source code, or
// when this executable carries none.
func extraireSources(d string) error {
	if !kitEmbarque() || sourceDuCode(d) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(d, "app", "go.mod")); err == nil {
		return nil
	}
	fichiers, modes, err := fichiersArchive(archiveKitGz)
	if err != nil {
		return err
	}
	f, m := map[string][]byte{}, map[string]os.FileMode{}
	for nom, b := range fichiers {
		if rel, ok := strings.CutPrefix(nom, "app/"); ok {
			f[rel], m[rel] = b, modes[nom]
		}
	}
	return remplacerDossier(filepath.Join(d, "app"), f, m)
}

// memeFichier: a and b are the same file (links resolved).
func memeFichier(a, b string) bool {
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// executerSurPlace: the sub-commands a downloaded executable runs where it is, without Athanor's folder.
func executerSurPlace(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "aide", "help", "--help", "-h", "version", "--version", "construire", "mise-a-jour":
		return true
	}
	return false
}

// lancerDepuisTelechargement: started outside a kit folder (the downloaded file, wherever it is), the
// executable installs or updates Athanor's folder, then runs the copy kept there with the same arguments.
// False: started from a kit folder, or a build that carries no kit (everything goes on as before).
func lancerDepuisTelechargement(args []string) (int, bool) {
	if !kitEmbarque() || executerSurPlace(args) {
		return 0, false
	}
	ici := dossierExecutable()
	if ici == "" || estDossierKit(ici) || installationAncienne(ici) {
		return 0, false
	}
	accepte := false
	reste := []string{}
	for _, a := range args {
		if a == "--oui" || a == "--yes" {
			accepte = true
			continue
		}
		reste = append(reste, a)
	}
	d := dossierAthanorDefaut()
	// the version kept there is newer: it runs, this older file changes nothing
	if versionPlusRecente(versionKit(d), versionAthanor) && fichierRegulierPilote(exeDuKit(d)) {
		return relancerKit(exeDuKit(d), reste), true
	}
	// an update while Athanor is open: it has to be closed first (its window is shown)
	if len(reste) == 0 && versionKit(d) != versionAthanor && versionKit(d) != "" && instanceExistante("ping") {
		titre, texte := texteFermerAvantMiseAJour(versionKit(d))
		informer(titre, texte)
		instanceExistante("montrer")
		return 0, true
	}
	if !premierLancement(d, accepte) {
		return 1, true // refused: nothing was written
	}
	r, err := deployerKit(d)
	if err != nil && !(estDossierKit(d) && fichierRegulierPilote(exeDuKit(d))) {
		titre, texte := texteEchecInstallation(d, err)
		informer(titre, texte)
		return 1, true
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Athanor: update incomplete:", err)
	}
	if r.Reporte {
		titre, texte := texteMiseAJourReportee(pilotesEnCours(d))
		informer(titre, texte)
	}
	return relancerKit(exeDuKit(d), reste), true
}

// relancerKit: the kit's executable with these arguments, in place of this one (Unix), or started and
// waited for (Windows: the application is left running on its own, so the downloaded file stays free).
func relancerKit(exe string, args []string) int {
	if runtime.GOOS != "windows" {
		err := execRemplacer(exe, append([]string{exe}, args...), os.Environ())
		fmt.Fprintln(os.Stderr, "Athanor:", err)
		return 1
	}
	c := exec.Command(exe, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		titre, texte := texteEchecInstallation(filepath.Dir(exe), err)
		informer(titre, texte)
		return 1
	}
	if len(args) == 0 {
		return 0
	}
	if err := c.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return 1
	}
	return 0
}

// mettreAJourSurPlace: the application opens from a kit folder (the copy of Athanor's folder, or an older
// installation whose executable was replaced by a newer one): the folder's files follow its version.
func mettreAJourSurPlace() {
	if !kitEmbarque() {
		return
	}
	d := dossierExecutable()
	if d == "" || !(estDossierKit(d) || installationAncienne(d)) {
		return
	}
	r, err := deployerKit(d)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Athanor: update of the tool folder incomplete:", err)
	}
	if r.Reporte {
		fmt.Fprintln(os.Stderr, "Athanor: the Linux executable is updated once the running pilots have stopped")
	}
}

// finirMiseAJour: before a pilot starts, what an update left for later (the Linux executable of the WSL
// mode, while pilots ran), when no pilot of the kit runs any more.
func finirMiseAJour(kit string) {
	if !kitEmbarque() || len(pilotesEnCours(kit)) > 0 || !memeFichier(dossierExecutable(), kit) {
		return
	}
	if _, err := os.Stat(filepath.Join(kit, "system", "athanor-linux.attente")); err != nil && versionKit(kit) == versionAthanor {
		return
	}
	if _, err := deployerKit(kit); err != nil {
		fmt.Fprintln(os.Stderr, "Athanor: update of the tool folder incomplete:", err)
	}
}

// the texts of the dialogs (English or French, like the first launch's)
func texteFermerAvantMiseAJour(ancienne string) (string, string) {
	if langueSysteme() == "fr" {
		return "Athanor — mise à jour", fmt.Sprintf("Athanor %s est ouvert. Fermez-le (et son icône près de l'horloge : Quitter), puis ouvrez à nouveau ce fichier pour passer à la version %s.\n\nLes pilotes qui tournent continuent.", ancienne, versionAthanor)
	}
	return "Athanor — update", fmt.Sprintf("Athanor %s is open. Close it (and its icon near the clock: Quit), then open this file again to update to version %s.\n\nThe running pilots go on.", ancienne, versionAthanor)
}

func texteEchecInstallation(d string, err error) (string, string) {
	if langueSysteme() == "fr" {
		return "Athanor", fmt.Sprintf("Impossible de préparer le dossier d'Athanor :\n\n    %s\n\n%v", d, err)
	}
	return "Athanor", fmt.Sprintf("Athanor's folder could not be prepared:\n\n    %s\n\n%v", d, err)
}

func texteMiseAJourReportee(projets []string) (string, string) {
	liste := strings.Join(projets, ", ")
	if langueSysteme() == "fr" {
		return "Athanor — mise à jour", fmt.Sprintf("Athanor %s est installé. Des pilotes tournent encore (%s) : leur partie Linux passe à la nouvelle version quand ils se sont tous arrêtés, au prochain démarrage d'un pilote.", versionAthanor, liste)
	}
	return "Athanor — update", fmt.Sprintf("Athanor %s is installed. Pilots are still running (%s): their Linux side moves to the new version once they have all stopped, when a pilot starts next.", versionAthanor, liste)
}
