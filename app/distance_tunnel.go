package main

// The Cloudflare tunnel of the remote access (distance.go): cloudflared, Cloudflare's own program (Apache
// 2.0), downloaded from its GitHub releases the first time (its SHA-256 checked against the release's
// list), then started on its own: a "quick tunnel", no account, carries the local server of the remote
// access to an https address under trycloudflare.com. It survives a restart of Athanor (an update): the
// address, and the devices' access, stay the same until the computer restarts.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type etatTunnel struct {
	etat    string // installation, demarrage, pret, erreur
	message string
	adresse string // https://….trycloudflare.com
	pid     int
	gen     int // each start of the tunnel: an older watcher stops
}

// tunnelSauve: the running tunnel, for the next start of Athanor (logs/distance/tunnel.json).
type tunnelSauve struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Adresse string `json:"adresse"`
}

var reAdresseTunnel = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func (s *Serveur) dossierDistance() string {
	return filepath.Join(s.kitCourant().Journaux(), "distance")
}

func (s *Serveur) cheminCloudflared() string {
	nom := "cloudflared"
	if runtime.GOOS == "windows" {
		nom += ".exe"
	}
	return filepath.Join(s.kitCourant().Racine, "system", "cloudflared", nom)
}

// hoteTunnel: the tunnel's host name, the only one the remote server answers ("" without a tunnel).
func (s *Serveur) hoteTunnel() string {
	d := s.dist()
	d.mu.Lock()
	a := d.tunnel.adresse
	d.mu.Unlock()
	if u, err := url.Parse(a); err == nil {
		return u.Host
	}
	return ""
}

func (s *Serveur) etatTunnel(gen int, etat, message, adresse string, pid int) bool {
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	if gen != d.tunnel.gen {
		return false // a newer start (or a stop) took over
	}
	d.tunnel.etat, d.tunnel.message, d.tunnel.adresse, d.tunnel.pid = etat, message, adresse, pid
	return true
}

// nomAssetCloudflared: the file of cloudflared's releases for this computer (Windows on ARM runs the x64 one).
func nomAssetCloudflared() string {
	switch runtime.GOOS {
	case "windows":
		return "cloudflared-windows-amd64.exe"
	case "darwin":
		return "cloudflared-darwin-" + runtime.GOARCH + ".tgz"
	}
	return "cloudflared-linux-" + runtime.GOARCH
}

// sommeDansNotes: the SHA-256 the release's notes give for a file ("cloudflared-linux-amd64: 9d71…").
func sommeDansNotes(notes, nom string) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(nom) + `\s*:\s*([0-9a-fA-F]{64})\s*$`)
	if m := re.FindStringSubmatch(notes); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// extraireTgzCloudflared: the cloudflared program inside the Mac archive.
func extraireTgzCloudflared(b []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	t := tar.NewReader(gz)
	for {
		h, err := t.Next()
		if err != nil {
			return nil, errors.New("cloudflared not found in the archive")
		}
		if filepath.Base(h.Name) == "cloudflared" && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(t, 200<<20))
		}
	}
}

var (
	apiCloudflared     = "https://api.github.com/repos/cloudflare/cloudflared/releases/latest"
	origineCloudflared = "https://github.com/cloudflare/cloudflared/releases/download/" // its files come from there only
)

// installerCloudflared: the latest cloudflared for this computer, checked, in the kit's system/.
func (s *Serveur) installerCloudflared() error {
	c := &http.Client{Timeout: 5 * time.Minute}
	lire := func(u string, max int64) ([]byte, error) {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", "Athanor/"+versionAthanor)
		r, err := c.Do(req)
		if err != nil {
			return nil, errors.New("Download failed: check the connection.")
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return nil, fmt.Errorf("Download failed (%d).", r.StatusCode)
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, max+1))
		if err == nil && int64(len(b)) > max {
			err = errors.New("Download refused: the file is too large.")
		}
		return b, err
	}
	b, err := lire(apiCloudflared, 4<<20)
	if err != nil {
		return err
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Notes  string `json:"body"`
		Assets []struct {
			Nom string `json:"name"`
			URL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(b, &rel); err != nil {
		return err
	}
	nom := nomAssetCloudflared()
	somme := sommeDansNotes(rel.Notes, nom)
	lien := ""
	for _, a := range rel.Assets {
		if a.Nom == nom {
			lien = a.URL
		}
	}
	if lien == "" || somme == "" {
		return fmt.Errorf("cloudflared %s: no %s with its checksum in the release", rel.Tag, nom)
	}
	if !strings.HasPrefix(lien, origineCloudflared) {
		return errors.New("cloudflared: unexpected download address")
	}
	prog, err := lire(lien, 200<<20)
	if err != nil {
		return err
	}
	h := sha256.Sum256(prog)
	if hex.EncodeToString(h[:]) != somme {
		return errors.New("cloudflared: the downloaded file does not match its checksum")
	}
	if strings.HasSuffix(nom, ".tgz") {
		if prog, err = extraireTgzCloudflared(prog); err != nil {
			return err
		}
	}
	dest := s.cheminCloudflared()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, prog, 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(filepath.Dir(dest), "VERSION"), []byte(rel.Tag+"\n"), 0o644)
	return os.Rename(tmp, dest)
}

func (s *Serveur) lireTunnelSauve() tunnelSauve {
	var t tunnelSauve
	b, err := os.ReadFile(filepath.Join(s.dossierDistance(), "tunnel.json"))
	if err == nil {
		_ = json.Unmarshal(b, &t)
	}
	return t
}

func (s *Serveur) ecrireTunnelSauve(t tunnelSauve) {
	_ = os.MkdirAll(s.dossierDistance(), 0o755)
	b, _ := json.Marshal(t)
	_ = os.WriteFile(filepath.Join(s.dossierDistance(), "tunnel.json"), b, 0o644)
}

// cloudflaredVivant: the process is still cloudflared (a number reused after a restart is not killed).
func cloudflaredVivant(pid int) bool {
	return pid > 1 && processusVivantPilote(pid) && strings.Contains(strings.ToLower(ligneDeCommande(pid)), "cloudflared")
}

// lancerTunnel: the tunnel to the local server of the remote access (kept from the previous start when it
// still points there), then watched: a tunnel that ends is started again.
func (s *Serveur) lancerTunnel() {
	d := s.dist()
	d.mu.Lock()
	d.tunnel.gen++
	gen := d.tunnel.gen
	d.tunnel.etat, d.tunnel.message, d.tunnel.adresse = "demarrage", "", ""
	d.mu.Unlock()
	s.mu.Lock()
	port := s.config.Distance.Port
	s.mu.Unlock()

	if _, err := os.Stat(s.cheminCloudflared()); err != nil {
		s.etatTunnel(gen, "installation", "", "", 0)
		if err := s.installerCloudflared(); err != nil {
			s.etatTunnel(gen, "erreur", err.Error(), "", 0)
			return
		}
	}
	attente := 5 * time.Second
	for {
		t := s.lireTunnelSauve()
		pid, adresse := t.PID, t.Adresse
		if !(cloudflaredVivant(pid) && t.Port == port && adresse != "") {
			if cloudflaredVivant(pid) {
				terminerProcessus(pid)
			}
			var err error
			if !s.etatTunnel(gen, "demarrage", "", "", 0) {
				return
			}
			pid, adresse, err = s.demarrerCloudflared(port)
			if err != nil {
				if !s.etatTunnel(gen, "erreur", err.Error(), "", 0) {
					return
				}
				time.Sleep(attente)
				attente = min(2*attente, 5*time.Minute)
				continue
			}
			s.ecrireTunnelSauve(tunnelSauve{PID: pid, Port: port, Adresse: adresse})
		}
		if !s.etatTunnel(gen, "pret", "", adresse, pid) {
			if cloudflaredVivant(pid) {
				terminerProcessus(pid)
			}
			return
		}
		attente = 5 * time.Second
		for cloudflaredVivant(pid) {
			time.Sleep(5 * time.Second)
			d.mu.Lock()
			fini := gen != d.tunnel.gen
			d.mu.Unlock()
			if fini {
				return
			}
		}
		// ended (the computer slept, the network dropped): a new tunnel, a new address
	}
}

// demarrerCloudflared: a quick tunnel to the local port, on its own (it outlives Athanor); its address.
func (s *Serveur) demarrerCloudflared(port int) (int, string, error) {
	_ = os.MkdirAll(s.dossierDistance(), 0o755)
	journal := filepath.Join(s.dossierDistance(), "cloudflared.log")
	f, err := os.Create(journal)
	if err != nil {
		return 0, "", err
	}
	c := commande(s.cheminCloudflared(), "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+strconv.Itoa(port))
	detacherProcessus(c)
	c.Stdout, c.Stderr = f, f
	c.Dir = s.dossierDistance()
	if err := c.Start(); err != nil {
		f.Close()
		return 0, "", err
	}
	f.Close()
	pid := c.Process.Pid
	go func() { _ = c.Wait() }()
	adresse := ""
	for i := 0; i < 120; i++ { // a minute at most
		time.Sleep(500 * time.Millisecond)
		b, _ := os.ReadFile(journal)
		if adresse == "" {
			adresse = reAdresseTunnel.FindString(string(b))
		}
		if adresse != "" && bytes.Contains(b, []byte("Registered tunnel connection")) {
			return pid, adresse, nil
		}
		if !processusVivantPilote(pid) {
			break
		}
	}
	terminerProcessus(pid)
	b, _ := os.ReadFile(journal)
	return 0, "", fmt.Errorf("The Cloudflare tunnel did not start: %s", derniereLigneUtile(string(b)))
}

// derniereLigneUtile: cloudflared's last error line (its log), for the message shown.
func derniereLigneUtile(journal string) string {
	lignes := strings.Split(strings.TrimSpace(journal), "\n")
	for i := len(lignes) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lignes[i]); strings.Contains(l, "ERR") {
			if len(l) > 240 {
				l = l[:240]
			}
			return l
		}
	}
	return "no answer from Cloudflare (a firewall?)"
}

// arreterTunnel: the switch turned off: the tunnel stops (its watcher with it).
func (s *Serveur) arreterTunnel() {
	d := s.dist()
	d.mu.Lock()
	d.tunnel.gen++
	pid := d.tunnel.pid
	d.tunnel = etatTunnel{gen: d.tunnel.gen}
	d.mu.Unlock()
	if cloudflaredVivant(pid) {
		terminerProcessus(pid)
	}
	s.arreterTunnelLaisse()
}

// arreterTunnelLaisse: a tunnel a previous start of Athanor left running.
func (s *Serveur) arreterTunnelLaisse() {
	if t := s.lireTunnelSauve(); cloudflaredVivant(t.PID) {
		terminerProcessus(t.PID)
	}
	_ = os.Remove(filepath.Join(s.dossierDistance(), "tunnel.json"))
}

// arreterTunnelSansTuer: Athanor ends (an update restarts it): the watcher stops, the tunnel stays.
func (s *Serveur) arreterTunnelSansTuer() {
	d := s.dist()
	d.mu.Lock()
	d.tunnel.gen++
	d.mu.Unlock()
}
