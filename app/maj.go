package main

// Updates from the application: the latest release of Athanor on GitHub, compared with this version (at
// opening, unless Options says not to, and when the user clicks the version). "Update now" downloads the
// file of this system, checks it against the release's SHA256SUMS.txt, then hands over to it: the new
// executable waits for this one to close, brings the tool folder to its version (embarque.go: settings,
// token, projects, logs/ and tools/ kept) and opens Athanor again. Running pilots go on.
//
// Only for an executable that carries the kit's files (a release, or athanor construire), never for a copy
// of the source code (git pull there).

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	enregistrerCommande("mise-a-jour", "mise-a-jour KIT PID: run by the new executable an update downloaded: waits for the application PID to close, brings the tool folder KIT to its version and opens Athanor again", lancerMiseAJour)
}

// apiDerniereVersion: GitHub's latest release of Athanor (ATHANOR_MAJ_API: the test harness's stand-in,
// never set by the application).
func apiDerniereVersion() string {
	if u := os.Getenv("ATHANOR_MAJ_API"); u != "" {
		return u
	}
	return "https://api.github.com/repos/Aprilox/Athanor/releases/latest"
}

// nomFichierMaj: the release's file for this system and processor (the one a user downloads; on the Mac,
// the zip of Athanor.app, whose executable serves both processors).
func nomFichierMaj() string {
	switch systemeMaj {
	case "windows/amd64":
		return "Athanor-Windows-x64.exe"
	case "windows/arm64":
		return "Athanor-Windows-ARM64.exe"
	case "linux/amd64":
		return "Athanor-Linux-x64"
	case "linux/arm64":
		return "Athanor-Linux-ARM64"
	case "darwin/arm64", "darwin/amd64":
		return "Athanor-macOS.zip"
	}
	return ""
}

// systemeMaj: this system and processor (a test stands in for another one).
var systemeMaj = runtime.GOOS + "/" + runtime.GOARCH

// executableZipMac: where Athanor-macOS.zip keeps the executable.
const executableZipMac = "Athanor.app/Contents/MacOS/athanor"

// extraireZipMac: the executable of Athanor.app, from the zip zf into dest.
func extraireZipMac(zf, dest string) error {
	z, err := zip.OpenReader(zf)
	if err != nil {
		return errors.New("The downloaded archive cannot be read: update refused.")
	}
	defer z.Close()
	for _, e := range z.File {
		if e.Name != executableZipMac {
			continue
		}
		r, err := e.Open()
		if err != nil {
			return err
		}
		defer r.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(r, 400<<20)); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("The downloaded archive holds no Athanor: update refused.")
}

// EtatMaj: what the application knows of the updates (the header's button, Options).
type EtatMaj struct {
	Possible   bool   `json:"possible"`             // this executable can update itself
	Raison     string `json:"raison,omitempty"`     // why not ("source": a copy of the source code)
	Derniere   string `json:"derniere,omitempty"`   // the latest version published
	Disponible bool   `json:"disponible"`           // newer than this one
	Page       string `json:"page,omitempty"`       // the release page
	Notes      string `json:"notes,omitempty"`      // what is new (the release text, without the downloads)
	Verifie    int64  `json:"verifie,omitempty"`    // when it was checked (Unix seconds)
	EnCours    string `json:"en_cours,omitempty"`   // verification, telechargement, redemarrage
	Erreur     string `json:"erreur,omitempty"`     // the last failure
	Avancement int    `json:"avancement,omitempty"` // download, percent
	urlFichier string
	urlSommes  string
}

var (
	majMu   sync.Mutex
	majEtat EtatMaj
)

// majPossible: whether this executable can update the kit k (and why not).
func majPossible(k Kit) (bool, string) {
	if !kitEmbarque() {
		return false, "source"
	}
	if sourceDuCode(k.Racine) {
		return false, "source"
	}
	if nomFichierMaj() == "" {
		return false, "systeme"
	}
	return true, ""
}

func etatMajCourant(k Kit) EtatMaj {
	majMu.Lock()
	defer majMu.Unlock()
	e := majEtat
	e.Possible, e.Raison = majPossible(k)
	if !e.Possible {
		e.Disponible = false
	}
	return e
}

var reVersionMaj = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)$`)

// notesMaj: the release text, without its downloads table (the application has its own button).
func notesMaj(corps string) string {
	if i := strings.Index(corps, "\n## Download"); i >= 0 {
		corps = corps[:i]
	} else if strings.HasPrefix(corps, "## Download") {
		corps = ""
	}
	corps = strings.TrimSpace(corps)
	if len(corps) > 6000 {
		corps = corps[:6000] + "…"
	}
	return corps
}

// verifierMaj: GitHub asked for the latest release (15 s at most).
func verifierMaj(k Kit) EtatMaj {
	if ok, _ := majPossible(k); !ok {
		return etatMajCourant(k)
	}
	majMu.Lock()
	if majEtat.EnCours == "telechargement" || majEtat.EnCours == "redemarrage" {
		majMu.Unlock()
		return etatMajCourant(k)
	}
	majEtat.EnCours, majEtat.Erreur = "verification", ""
	majMu.Unlock()
	derniere, page, notes, fichier, sommes, err := lireDerniereVersion()
	majMu.Lock()
	majEtat.EnCours = ""
	majEtat.Verifie = time.Now().Unix()
	if err != nil {
		majEtat.Erreur = err.Error()
	} else {
		majEtat.Derniere, majEtat.Page, majEtat.Notes = derniere, page, notes
		majEtat.urlFichier, majEtat.urlSommes = fichier, sommes
		majEtat.Disponible = versionPlusRecente(derniere, versionAthanor) && fichier != "" && sommes != ""
		if versionPlusRecente(derniere, versionAthanor) && (fichier == "" || sommes == "") {
			majEtat.Erreur = "The latest release has no file for this system yet."
		}
	}
	majMu.Unlock()
	return etatMajCourant(k)
}

func lireDerniereVersion() (version, page, notes, fichier, sommes string, err error) {
	ctx, fin := context.WithTimeout(context.Background(), 15*time.Second)
	defer fin()
	req, _ := http.NewRequestWithContext(ctx, "GET", apiDerniereVersion(), nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Athanor/"+versionAthanor)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", "", "", errors.New("GitHub cannot be reached: check the connection.")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", "", "", "", "", fmt.Errorf("GitHub answered %d.", r.StatusCode)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Corps  string `json:"body"`
		Brouil bool   `json:"draft"`
		Pre    bool   `json:"prerelease"`
		Assets []struct {
			Nom string `json:"name"`
			URL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&rel); err != nil {
		return "", "", "", "", "", errors.New("Unreadable answer from GitHub.")
	}
	m := reVersionMaj.FindStringSubmatch(strings.TrimSpace(rel.Tag))
	if m == nil || rel.Brouil || rel.Pre {
		return "", "", "", "", "", errors.New("Unreadable answer from GitHub.")
	}
	for _, a := range rel.Assets {
		switch a.Nom {
		case nomFichierMaj():
			fichier = a.URL
		case "SHA256SUMS.txt":
			sommes = a.URL
		}
	}
	return m[1], rel.Page, notesMaj(rel.Corps), fichier, sommes, nil
}

// telechargerMaj: url into f (size at most max), calling avance with the percent done.
func telechargerMaj(url, f string, max int64, avance func(int)) error {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "Athanor/"+versionAthanor)
	c := &http.Client{Timeout: 15 * time.Minute}
	r, err := c.Do(req)
	if err != nil {
		return errors.New("Download failed: check the connection.")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("Download failed (%d).", r.StatusCode)
	}
	if r.ContentLength > max {
		return errors.New("Download refused: the file is too large.")
	}
	out, err := os.Create(f)
	if err != nil {
		return err
	}
	var lu int64
	buf := make([]byte, 256<<10)
	for {
		n, e := r.Body.Read(buf)
		if n > 0 {
			lu += int64(n)
			if lu > max {
				out.Close()
				return errors.New("Download refused: the file is too large.")
			}
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				return err
			}
			if avance != nil && r.ContentLength > 0 {
				avance(int(lu * 100 / r.ContentLength))
			}
		}
		if e == io.EOF {
			break
		}
		if e != nil {
			out.Close()
			return errors.New("Download interrupted: try again.")
		}
	}
	return out.Close()
}

// sommeAttendue: the SHA-256 of nom in a SHA256SUMS.txt ("HASH  name" lines).
func sommeAttendue(sommes []byte, nom string) string {
	sc := bufio.NewScanner(strings.NewReader(string(sommes)))
	for sc.Scan() {
		champs := strings.Fields(sc.Text())
		if len(champs) == 2 && strings.TrimPrefix(champs[1], "*") == nom && len(champs[0]) == 64 {
			return strings.ToLower(champs[0])
		}
	}
	return ""
}

func sha256Fichier(f string) (string, error) {
	o, err := os.Open(f)
	if err != nil {
		return "", err
	}
	defer o.Close()
	h := sha256.New()
	if _, err := io.Copy(h, o); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dossierMaj: where an update is downloaded (the kit's logs/maj).
func dossierMaj(k Kit) string { return filepath.Join(k.Journaux(), "maj") }

// preparerMaj: the new executable downloaded and checked; its path.
func preparerMaj(k Kit) (string, error) {
	majMu.Lock()
	fichier, sommes, derniere := majEtat.urlFichier, majEtat.urlSommes, majEtat.Derniere
	majMu.Unlock()
	if fichier == "" || sommes == "" || !versionPlusRecente(derniere, versionAthanor) {
		return "", errors.New("No update to install: check again.")
	}
	d := dossierMaj(k)
	_ = os.RemoveAll(d)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	fs := filepath.Join(d, "SHA256SUMS.txt")
	if err := telechargerMaj(sommes, fs, 1<<20, nil); err != nil {
		return "", err
	}
	b, _ := os.ReadFile(fs)
	attendue := sommeAttendue(b, nomFichierMaj())
	if attendue == "" {
		return "", errors.New("The release gives no checksum for this file: update refused.")
	}
	nom := "athanor-" + derniere
	if runtime.GOOS == "windows" {
		nom = "Athanor-" + derniere + ".exe"
	}
	f := filepath.Join(d, nom)
	if err := telechargerMaj(fichier, f+".part", 300<<20, func(p int) {
		majMu.Lock()
		majEtat.Avancement = p
		majMu.Unlock()
	}); err != nil {
		return "", err
	}
	if s, err := sha256Fichier(f + ".part"); err != nil || s != attendue {
		os.Remove(f + ".part")
		return "", errors.New("The downloaded file does not match the release's checksum: update refused.")
	}
	if strings.HasSuffix(nomFichierMaj(), ".zip") { // the Mac: the executable of the Athanor.app it holds
		err := extraireZipMac(f+".part", f)
		os.Remove(f + ".part")
		if err != nil {
			os.Remove(f)
			return "", err
		}
	} else if err := os.Rename(f+".part", f); err != nil {
		return "", err
	}
	if err := os.Chmod(f, 0o755); err != nil {
		return "", err
	}
	// it must really be Athanor of that version
	out, err := commande(f, "version").Output()
	if err != nil || strings.TrimSpace(string(out)) != derniere {
		return "", errors.New("The downloaded file does not start: update refused.")
	}
	return f, nil
}

// installerMaj: download, check, hand over to the new executable; then fin (the application closes).
func installerMaj(k Kit, fin func()) error {
	majMu.Lock()
	if majEtat.EnCours == "telechargement" || majEtat.EnCours == "redemarrage" {
		majMu.Unlock()
		return errors.New("An update is already under way.")
	}
	majEtat.EnCours, majEtat.Erreur, majEtat.Avancement = "telechargement", "", 0
	majMu.Unlock()
	go func() {
		f, err := preparerMaj(k)
		if err == nil {
			err = lancerDetacheExe(f, "mise-a-jour", k.Racine, strconv.Itoa(os.Getpid()))
		}
		majMu.Lock()
		if err != nil {
			majEtat.EnCours, majEtat.Erreur = "", err.Error()
			majMu.Unlock()
			return
		}
		majEtat.EnCours = "redemarrage"
		majMu.Unlock()
		time.Sleep(1500 * time.Millisecond) // the interface shows "Athanor restarts"
		fin()
	}()
	return nil
}

// lancerMiseAJour: athanor mise-a-jour KIT PID, by the new executable.
func lancerMiseAJour(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor mise-a-jour KIT PID")
		return 2
	}
	kit := args[0]
	pid, _ := strconv.Atoi(args[1])
	if !estDossierKit(kit) {
		fmt.Fprintln(os.Stderr, "athanor mise-a-jour: not a tool folder:", kit)
		return 2
	}
	tx := textesProgresMaj(langueDuKit(kit))
	fenetre := ouvrirProgresMaj(fmt.Sprintf(tx[0], versionAthanor))
	defer fenetre.fermer()
	fenetre.etapeMaj(tx[1], 5)
	for i := 0; pid > 0 && i < 240 && processusVivantPilote(pid); i++ {
		time.Sleep(250 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	progresDeploiement = func(etape string, pct int) {
		switch etape {
		case "fichiers":
			fenetre.etapeMaj(tx[2], pct)
		case "exe":
			fenetre.etapeMaj(tx[3], pct)
		case "linux":
			fenetre.etapeMaj(tx[4], pct)
		}
	}
	_, err := deployerKit(kit)
	progresDeploiement = nil
	if err != nil {
		fenetre.fermer()
		titre, texte := texteEchecInstallation(kit, err)
		informer(titre, texte)
	}
	// Athanor opens again (the previous executable when the new one could not be put in place)
	fenetre.etapeMaj(tx[5], 95)
	if e := rouvrirAthanor(kit); e != nil {
		fenetre.fermer()
		titre, texte := texteEchecInstallation(kit, e)
		informer(titre, texte)
		return 1
	}
	// the small window stays until the new Athanor answers (its window is opening)
	for i := 0; i < 60 && !instanceExistante("ping"); i++ {
		time.Sleep(500 * time.Millisecond)
	}
	fenetre.etapeMaj(tx[5], 100)
	time.Sleep(400 * time.Millisecond)
	if err != nil {
		return 1
	}
	return 0
}

// rouvrirAthanor: the application again after the update; opened from Athanor.app, through the bundle
// (its Dock icon comes back with it), else the kit's executable.
func rouvrirAthanor(kit string) error {
	if b := bundleDurableMac(); b != "" && runtime.GOOS == "darwin" {
		if commande("open", b).Run() == nil {
			return nil
		}
	}
	return lancerDetacheExe(exeDuKit(kit))
}

// langueDuKit: the application's language, from the kit's settings ("" when unknown).
func langueDuKit(kit string) string {
	b, err := os.ReadFile(filepath.Join(kit, nomConfig()))
	if err != nil {
		return ""
	}
	var c struct {
		Langue string `json:"langue"`
	}
	_ = json.Unmarshal(b, &c)
	return c.Langue
}

// textesProgresMaj: the title (with the version) and the steps of the update's window.
func textesProgresMaj(langue string) [6]string {
	if langue == "" {
		langue = langueSysteme()
	}
	switch langue {
	case "fr":
		return [6]string{"Mise à jour d'Athanor %s", "Fermeture d'Athanor…", "Installation des fichiers…", "Installation de l'application…", "Installation de la partie Linux…", "Ouverture d'Athanor…"}
	case "es":
		return [6]string{"Actualización de Athanor %s", "Cerrando Athanor…", "Instalando los archivos…", "Instalando la aplicación…", "Instalando la parte Linux…", "Abriendo Athanor…"}
	case "de":
		return [6]string{"Update von Athanor %s", "Athanor wird geschlossen…", "Dateien werden installiert…", "Anwendung wird installiert…", "Linux-Teil wird installiert…", "Athanor wird geöffnet…"}
	case "it":
		return [6]string{"Aggiornamento di Athanor %s", "Chiusura di Athanor…", "Installazione dei file…", "Installazione dell'applicazione…", "Installazione della parte Linux…", "Apertura di Athanor…"}
	case "pt":
		return [6]string{"Atualização do Athanor %s", "A fechar o Athanor…", "A instalar os ficheiros…", "A instalar a aplicação…", "A instalar a parte Linux…", "A abrir o Athanor…"}
	}
	return [6]string{"Updating Athanor %s", "Closing Athanor…", "Installing the files…", "Installing the application…", "Installing the Linux part…", "Opening Athanor…"}
}

// nettoyerMaj: what an update left in logs/maj, once the new version runs.
func nettoyerMaj(k Kit) {
	if versionKit(k.Racine) == versionAthanor {
		_ = os.RemoveAll(dossierMaj(k))
	}
}

// apiMaj: what is known of the updates; ?verifier=1 asks GitHub now.
func (s *Serveur) apiMaj(w http.ResponseWriter, r *http.Request) {
	k := s.kitCourant()
	if r.URL.Query().Get("verifier") == "1" {
		s.ecrireJSON(w, verifierMaj(k))
		return
	}
	s.ecrireJSON(w, etatMajCourant(k))
}

// apiInstallerMaj: "Update now": the download starts; the application closes once the new executable
// has taken over.
func (s *Serveur) apiInstallerMaj(w http.ResponseWriter, r *http.Request) {
	k := s.kitCourant()
	if ok, _ := majPossible(k); !ok {
		s.erreur(w, http.StatusBadRequest, "This copy of Athanor is updated with git pull, not from here.")
		return
	}
	if err := installerMaj(k, quitterPourMaj); err != nil {
		s.erreur(w, http.StatusConflict, err.Error())
		return
	}
	s.ecrireJSON(w, etatMajCourant(k))
}

// verifierMajFond: at opening (unless Options says not to), then twice a day.
func (s *Serveur) verifierMajFond() {
	nettoyerMaj(s.kitCourant())
	time.Sleep(8 * time.Second)
	for {
		s.mu.Lock()
		non := s.config.PasVerifierMaj
		s.mu.Unlock()
		if !non {
			verifierMaj(s.kitCourant())
		}
		time.Sleep(12 * time.Hour)
	}
}
