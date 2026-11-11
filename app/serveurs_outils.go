package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// The Servers tools (stage 5): Go code that reaches a server of the inventory by its id with the vault's key,
// which never leaves this process. Each call goes to the audit log (tool, server, arguments, code, output
// masked then cut). Nobody is given these tools before stage 6 (M80 serves them to a token's perimeter only).

const (
	sortieMaxOutil   = 64 << 10  // output handed back to the caller
	auditMaxOutil    = 1000      // characters of arguments and of output kept in the audit log
	tailleMaxFichier = 1 << 20   // fichier_lire / fichier_ecrire
	tailleMaxSFTP    = 256 << 20 // sftp_envoyer / sftp_recuperer
	delaiCommande    = 60 * time.Second
	delaiCommandeMax = 600 * time.Second
	delaiFichier     = 30 * time.Second
	delaiSFTP        = 10 * time.Minute
)

// ResultatOutil: what a Servers tool answers (Sortie masked of the vault's open values).
type ResultatOutil struct {
	Sortie string `json:"sortie"`
	Code   int    `json:"code"`
	Coupee bool   `json:"coupee,omitempty"`
}

// argsOutilServeur: the arguments of every Servers tool (each one reads its own, unknown ones are refused).
type argsOutilServeur struct {
	Commande string  `json:"commande,omitempty"`
	DelaiS   int     `json:"delai_s,omitempty"`
	Chemin   string  `json:"chemin,omitempty"`
	Contenu  *string `json:"contenu,omitempty"`
	Sudo     bool    `json:"sudo,omitempty"`
	Local    string  `json:"local,omitempty"`
	Distant  string  `json:"distant,omitempty"`
}

type outilServeur func(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error)

var outilsServeurs = map[string]outilServeur{
	"ssh_commande":   outilSSHCommande,
	"fichier_lire":   outilFichierLire,
	"fichier_ecrire": outilFichierEcrire,
	"sftp_envoyer":   outilSFTPEnvoyer,
	"sftp_recuperer": outilSFTPRecuperer,
}

// appelerOutilServeur runs tool outil on server id for de (who asks, for the log) and writes the audit line.
func (s *Serveur) appelerOutilServeur(ctx context.Context, de, id, outil string, args json.RawMessage) (ResultatOutil, error) {
	r, err := s.executerOutilServeur(ctx, id, outil, args)
	r.Sortie = s.masquerSecrets(r.Sortie)
	if len(r.Sortie) > sortieMaxOutil {
		r.Sortie, r.Coupee = r.Sortie[:sortieMaxOutil], true
	}
	// masked before being cut: a cut value would no longer be recognised
	res := fmt.Sprintf("%s · code %d · %s", couper(s.masquerSecrets(string(args)), auditMaxOutil), r.Code, couper(r.Sortie, auditMaxOutil))
	if err != nil {
		res += " · error: " + couper(s.masquerSecrets(err.Error()), auditMaxOutil)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + id, Action: "serveurs." + outil, Resultat: res})
	return r, err
}

func (s *Serveur) executerOutilServeur(ctx context.Context, id, outil string, args json.RawMessage) (ResultatOutil, error) {
	f, ok := outilsServeurs[outil]
	if !ok {
		return ResultatOutil{Code: -1}, fmt.Errorf("unknown tool %q", outil)
	}
	var a argsOutilServeur
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if err := lireArguments(args, &a); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	l, err := lireServeurs()
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		return ResultatOutil{Code: -1}, errServeurInconnu
	}
	c, err := s.connecterServeur(ctx, l[i])
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	defer c.Close()
	return f(ctx, c, l[i], a)
}

// resultatSSH: a command's result as a tool answers it.
func resultatSSH(r ResultatSSH) ResultatOutil {
	return ResultatOutil{Sortie: string(r.Sortie), Code: r.Code, Coupee: r.Coupee}
}

func outilSSHCommande(ctx context.Context, c *ssh.Client, _ ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	if strings.TrimSpace(a.Commande) == "" {
		return ResultatOutil{Code: -1}, errors.New("commande is required")
	}
	delai := delaiCommande
	if a.DelaiS > 0 {
		delai = min(time.Duration(a.DelaiS)*time.Second, delaiCommandeMax)
	}
	r, err := executerSSH(ctx, c, a.Commande, nil, delai)
	return resultatSSH(r), err
}

// cheminDistant: an absolute path on the server, without control characters (it is always passed quoted).
func cheminDistant(nom, p string) (string, error) {
	if !strings.HasPrefix(p, "/") || len(p) > 4096 || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%s must be an absolute path on the server (/…), without control characters", nom)
	}
	return p, nil
}

// scriptShell: sh -c script with its arguments, every one quoted, run through sudo -n when asked.
func scriptShell(sudo bool, script string, args ...string) string {
	cmd := "sh -c " + citerShell(script) + " sh"
	for _, a := range args {
		cmd += " " + citerShell(a)
	}
	if sudo {
		return "sudo -n " + cmd
	}
	return cmd
}

func outilFichierLire(ctx context.Context, c *ssh.Client, _ ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	p, err := cheminDistant("chemin", a.Chemin)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	// one byte more than the limit (or executerSSH's cut) tells a file too large; head bounds what crosses the network
	r, err := executerSSH(ctx, c, scriptShell(a.Sudo, `head -c "$2" -- "$1"`, p, fmt.Sprint(tailleMaxFichier+1)), nil, delaiFichier)
	if err != nil || r.Code != 0 {
		return resultatSSH(r), err
	}
	if r.Coupee || len(r.Sortie) > tailleMaxFichier {
		return ResultatOutil{Code: -1}, fmt.Errorf("%s is larger than %d bytes: fetch it with sftp_recuperer", p, tailleMaxFichier)
	}
	return resultatSSH(r), nil
}

// scriptEcrire: the content goes to a temporary file next to the target, which keeps the target's mode
// (and owner under sudo), then replaces it in one rename; a failure leaves the target as it was.
const scriptEcrire = `t=$(mktemp "$1.athanor.XXXXXX") || exit 1
if cat > "$t"; then
  if [ -e "$1" ]; then chmod "$(stat -c %a -- "$1")" "$t" && { [ "$(id -u)" != 0 ] || chown "$(stat -c %u:%g -- "$1")" "$t"; }; else chmod 644 "$t"; fi && mv -f -- "$t" "$1" && echo "$2 bytes written to $1" && exit 0
fi
rm -f -- "$t"; exit 1`

func outilFichierEcrire(ctx context.Context, c *ssh.Client, _ ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	p, err := cheminDistant("chemin", a.Chemin)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	if a.Contenu == nil {
		return ResultatOutil{Code: -1}, errors.New("contenu is required")
	}
	if len(*a.Contenu) > tailleMaxFichier {
		return ResultatOutil{Code: -1}, fmt.Errorf("contenu is larger than %d bytes: send it with sftp_envoyer", tailleMaxFichier)
	}
	r, err := executerSSH(ctx, c, scriptShell(a.Sudo, scriptEcrire, p, fmt.Sprint(len(*a.Contenu))), strings.NewReader(*a.Contenu), delaiFichier)
	return resultatSSH(r), err
}

// dossierFichiersServeur: the only local folder the SFTP tools read or write for server id.
func dossierFichiersServeur(id string) string {
	return filepath.Join(dossierModule("servers"), id, "fichiers")
}

// racineFichiers opens the server's local folder and checks rel: relative, without "..", and no link on
// the way (os.Root then keeps every access inside, even if a link appears meanwhile).
func racineFichiers(srv ServeurInventaire, rel string) (*os.Root, string, error) {
	if !idServeurValide.MatchString(srv.ID) {
		return nil, "", errServeurInconnu
	}
	rel = filepath.FromSlash(rel)
	if !filepath.IsLocal(rel) {
		return nil, "", fmt.Errorf("local must be a path inside modules/servers/%s/fichiers/, without \"..\"", srv.ID)
	}
	d := dossierFichiersServeur(srv.ID)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(d)
	if err != nil {
		return nil, "", err
	}
	partiel := ""
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		partiel = filepath.Join(partiel, part)
		st, err := root.Lstat(partiel)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil || st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			root.Close()
			return nil, "", fmt.Errorf("local %s: links are refused", filepath.ToSlash(rel))
		}
	}
	return root, rel, nil
}

// clientSFTP: an SFTP session on c, closed (with c) when ctx ends so that a stuck transfer stops.
func clientSFTP(ctx context.Context, c *ssh.Client) (*sftp.Client, func(), error) {
	ctx, annuler := context.WithTimeout(ctx, delaiSFTP)
	stop := context.AfterFunc(ctx, func() { c.Close() })
	cs, err := sftp.NewClient(c)
	if err != nil {
		stop()
		annuler()
		return nil, nil, fmt.Errorf("no SFTP on this server: %w", err)
	}
	return cs, func() { cs.Close(); stop(); annuler() }, nil
}

func outilSFTPEnvoyer(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	distant, err := cheminDistant("distant", a.Distant)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	root, rel, err := racineFichiers(srv, a.Local)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	defer root.Close()
	f, err := root.Open(rel)
	if err != nil {
		return ResultatOutil{Code: -1}, fmt.Errorf("local %s: %w", filepath.ToSlash(rel), fs.ErrNotExist)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > tailleMaxSFTP {
		return ResultatOutil{Code: -1}, fmt.Errorf("local %s must be a file of %d bytes at most", filepath.ToSlash(rel), tailleMaxSFTP)
	}
	cs, fermer, err := clientSFTP(ctx, c)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	defer fermer()
	part := distant + ".athanor-part"
	df, err := cs.Create(part)
	if err != nil {
		return ResultatOutil{Code: -1}, fmt.Errorf("%s: %w", part, err)
	}
	n, err := df.ReadFrom(io.LimitReader(f, tailleMaxSFTP))
	if errF := df.Close(); err == nil {
		err = errF
	}
	if err == nil {
		err = cs.PosixRename(part, distant) // replaces the target in one step
	}
	if err != nil {
		_ = cs.Remove(part)
		return ResultatOutil{Code: -1}, fmt.Errorf("sending to %s: %w", distant, err)
	}
	return ResultatOutil{Sortie: fmt.Sprintf("%d bytes sent to %s", n, distant)}, nil
}

func outilSFTPRecuperer(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	distant, err := cheminDistant("distant", a.Distant)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	root, rel, err := racineFichiers(srv, a.Local)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	defer root.Close()
	cs, fermer, err := clientSFTP(ctx, c)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	defer fermer()
	sf, err := cs.Open(distant)
	if err != nil {
		return ResultatOutil{Code: -1}, fmt.Errorf("%s: %w", distant, err)
	}
	defer sf.Close()
	if st, err := sf.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > tailleMaxSFTP {
		return ResultatOutil{Code: -1}, fmt.Errorf("%s must be a file of %d bytes at most", distant, tailleMaxSFTP)
	}
	if d := filepath.Dir(rel); d != "." {
		if err := root.MkdirAll(d, 0o700); err != nil {
			return ResultatOutil{Code: -1}, err
		}
	}
	part := rel + ".athanor-part"
	lf, err := root.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	n, err := io.Copy(lf, io.LimitReader(sf, tailleMaxSFTP+1))
	if errF := lf.Close(); err == nil {
		err = errF
	}
	if err == nil && n > tailleMaxSFTP {
		err = fmt.Errorf("larger than %d bytes", tailleMaxSFTP)
	}
	if err == nil {
		err = root.Rename(part, rel)
	}
	if err != nil {
		_ = root.Remove(part)
		return ResultatOutil{Code: -1}, fmt.Errorf("fetching %s: %w", distant, err)
	}
	return ResultatOutil{Sortie: fmt.Sprintf("%d bytes fetched to fichiers/%s", n, filepath.ToSlash(rel))}, nil
}
