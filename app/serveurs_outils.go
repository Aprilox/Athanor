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
	"regexp"
	"slices"
	"strconv"
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
	// the change waits for confirmer_changement with this id, else the server undoes it (M93)
	Annulation string `json:"annulation,omitempty"`
	// the call changed the server (a service, a container, a firewall rule): its sheet is made again
	modifie bool
	// what the call kept before changing (an archive, a previous version): the runbook's Backups (M96)
	sauvegardes []string
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
	Action   string  `json:"action,omitempty"`
	Nom      string  `json:"nom,omitempty"`
	Lignes   int     `json:"lignes,omitempty"`
	Dossier  string  `json:"dossier,omitempty"`
	Port     int     `json:"port,omitempty"`
	Proto    string  `json:"proto,omitempty"`
	Regle    string  `json:"regle,omitempty"`
}

type outilServeur func(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error)

var outilsServeurs = map[string]outilServeur{
	"ssh_commande":   outilSSHCommande,
	"fichier_lire":   outilFichierLire,
	"fichier_ecrire": outilFichierEcrire,
	"sftp_envoyer":   outilSFTPEnvoyer,
	"sftp_recuperer": outilSFTPRecuperer,
	"service":        outilService,
	"docker":         outilDocker,
	"pare_feu":       outilPareFeu,
}

// Sauvegarde: what a plan's step archives on the server before it runs (M92); none when Chemins is empty.
type Sauvegarde struct {
	Archive string // <plan>-<step>, the archive's name in dossierSauvegardes
	Chemins []string
}

func sauvegardeDe(e Etape) Sauvegarde {
	return Sauvegarde{Archive: fmt.Sprintf("%s-%d", e.Plan, e.Num), Chemins: e.Sauvegarde}
}

// scriptArchiver: $1 the archive's name, then the paths (relative to /, "./…"); as root, else through sudo -n
// when it is allowed; the folder 0700, the archive 0600; prints the archive's path; a failed archive is removed.
const scriptArchiver = `d=` + dossierSauvegardes + `; a="$d/$1.tar.gz"; shift
S=; [ "$(id -u)" = 0 ] || { sudo -n true 2>/dev/null && S="sudo -n"; }
$S mkdir -p -m 700 "$d" && $S sh -c 'umask 077; tar -czf "$0" -C / "$@"' "$a" "$@" && $S chmod 600 "$a" && echo "$a" && exit 0
$S rm -f "$a"; exit 1`

// archiver makes the step's archive; any failure stops the step.
func archiver(ctx context.Context, c *ssh.Client, sv Sauvegarde) (string, error) {
	args := []string{sv.Archive}
	for _, p := range sv.Chemins {
		args = append(args, "."+p)
	}
	r, err := executerSSH(ctx, c, scriptShell(false, scriptArchiver, args...), nil, delaiCommandeMax)
	if err == nil && r.Code != 0 {
		err = errors.New(couper(strings.TrimSpace(string(r.Sortie)), 500))
	}
	if err != nil {
		return "", fmt.Errorf("the backup of %s failed, the step did not run (propose a new plan): %w", strings.Join(sv.Chemins, ", "), err)
	}
	return fmt.Sprintf("backup: %s (%s)", strings.TrimSpace(string(r.Sortie)), strings.Join(sv.Chemins, ", ")), nil
}

// appelerOutilServeur runs tool outil on server id for de (who asks, for the log), after archiving sv, and
// writes the audit line.
func (s *Serveur) appelerOutilServeur(ctx context.Context, de, id, outil string, args json.RawMessage, sv Sauvegarde) (ResultatOutil, error) {
	r, err := s.executerOutilServeur(ctx, de, id, outil, args, sv)
	r.Sortie = s.masquerSecrets(r.Sortie)
	if len(r.Sortie) > sortieMaxOutil {
		r.Sortie, r.Coupee = r.Sortie[:sortieMaxOutil], true
	}
	// masked before being cut: a cut value would no longer be recognised
	if v := any(nil); json.Unmarshal(args, &v) == nil { // one escaping form (Go's), the one masquerSecrets also knows
		args, _ = json.Marshal(v)
	}
	res := fmt.Sprintf("%s · code %d · %s", couper(s.masquerSecrets(string(args)), auditMaxOutil), r.Code, couper(r.Sortie, auditMaxOutil))
	if err != nil {
		res += " · error: " + couper(s.masquerSecrets(err.Error()), auditMaxOutil)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: de, Vers: "serveur:" + id, Action: "serveurs." + outil, Resultat: res})
	return r, err
}

func (s *Serveur) executerOutilServeur(ctx context.Context, de, id, outil string, args json.RawMessage, sv Sauvegarde) (ResultatOutil, error) {
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
	note := ""
	if len(sv.Chemins) > 0 {
		if note, err = archiver(ctx, c, sv); err != nil {
			return ResultatOutil{Code: -1}, err
		}
	}
	var an AnnulationServeur
	genre, chemin := genreAnnulation(ctx, c, l[i], outil, a)
	if genre != "" {
		if an, err = programmerAnnulation(ctx, c, l[i], outil, genre, chemin, a); err != nil {
			return ResultatOutil{Code: -1, Sortie: note, sauvegardes: []string{note}}, err
		}
	}
	r, err := f(ctx, c, l[i], a)
	if genre != "" {
		r = s.apresChangement(ctx, c, l[i], de, an, r, err)
	}
	if note != "" {
		r.sauvegardes = append([]string{note}, r.sauvegardes...)
		r.Sortie = note + "\n" + r.Sortie // first: the audit line keeps only the start of the output
	}
	if err == nil && r.Code == 0 && r.modifie {
		if _, e := s.rafraichirFiche(ctx, l[i]); e != nil {
			r.Sortie += "\n(the server sheet could not be refreshed: " + e.Error() + ")"
		}
	}
	return r, err
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
	r, err := executerSSH(ctx, c, commandeBornee(a.Commande, delai), nil, delai)
	return resultatSSH(r), err
}

// commandeBornee: an agent's command run by the login shell under `timeout -k 5 <delay>` (C34): killed on the
// server too when its delay ends, its whole process group; without a usable timeout, run as is.
func commandeBornee(commande string, delai time.Duration) string {
	return scriptShell(false, `if timeout -k 1 5 true 2>/dev/null; then exec timeout -k 5 "$2" "${SHELL:-/bin/sh}" -c "$1"; fi; exec "${SHELL:-/bin/sh}" -c "$1"`,
		commande, strconv.Itoa(int((delai+time.Second-1)/time.Second)))
}

// cheminDistant: an absolute path on the server, without control characters (it is always passed quoted).
func cheminDistant(nom, p string) (string, error) {
	if !strings.HasPrefix(p, "/") || len(p) > 4096 || strings.IndexFunc(p, caractereInvisible) >= 0 {
		return "", fmt.Errorf("%s must be an absolute path on the server (/…), without control or invisible characters", nom)
	}
	return p, nil
}

// distantPermis: cheminDistant, and not a refused path (C36) as given.
func distantPermis(nom, p string, lecture bool) (string, error) {
	p, err := cheminDistant(nom, p)
	if err == nil {
		err = cheminRefuse(p, lecture)
	}
	return p, err
}

// cheminResolu (C36): p as the server resolved it (links, ".."), checked again; unresolved ("" or not absolute) = p.
func cheminResolu(p, reel string, lecture bool) (string, error) {
	if !strings.HasPrefix(reel, "/") {
		return p, nil
	}
	if _, err := cheminDistant("the resolved path", reel); err != nil {
		return "", err
	}
	if err := cheminRefuse(reel, lecture); err != nil {
		return "", fmt.Errorf("%s leads to %w", p, err)
	}
	return reel, nil
}

// resoudreDistant: p checked, resolved on the server by readlink -f (as root with sudo) and checked again;
// the tool then works on the resolved path, so swapping a link afterwards changes nothing.
func resoudreDistant(ctx context.Context, c *ssh.Client, sudo bool, nom, p string, lecture bool) (string, error) {
	p, err := distantPermis(nom, p, lecture)
	if err != nil {
		return "", err
	}
	r, err := executerSSH(ctx, c, scriptShell(sudo, `readlink -f -- "$1"`, p), nil, delaiFichier)
	if err != nil {
		return "", err
	}
	if r.Code != 0 { // a missing folder on the way: the tool fails on p as well
		return p, nil
	}
	return cheminResolu(p, strings.TrimSuffix(string(r.Sortie), "\n"), lecture)
}

var errNomInvisible = erreurEntree("The name holds control or invisible characters")

// portsSSHD: the ports sshd listens on, as `sshd -T` says (root or sudo); none when it cannot be read.
func portsSSHD(ctx context.Context, c *ssh.Client) []int {
	r, err := executerSSH(ctx, c, scriptShell(false, preludeRoot+`$S sshd -T 2>/dev/null | awk '$1 == "port" { print $2 }'`), nil, delaiCommande)
	var l []int
	if err == nil {
		for _, x := range strings.Fields(string(r.Sortie)) {
			if p, e := strconv.Atoi(x); e == nil {
				l = append(l, p)
			}
		}
	}
	return l
}

// caractereInvisible: control, format (U+202E, zero widths) and line/paragraph separators: what is shown would not be what runs.
func caractereInvisible(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp)
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
	p, err := resoudreDistant(ctx, c, a.Sudo, "chemin", a.Chemin, true)
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

// scriptEcrire: a link is followed to its real target (C32: the link stays); the content goes to a temporary
// file next to that target, which keeps its mode (and owner under sudo), then replaces it in one rename;
// a failure leaves the target as it was. An existing file is copied first to <target>.athanor-prev (M92).
const scriptEcrire = `p=$(readlink -f -- "$1") && [ -n "$p" ] || p=$1
[ -d "$p" ] && { echo "$1 is a directory" >&2; exit 1; }
t=$(mktemp "$p.athanor.XXXXXX") || exit 1
if cat > "$t"; then
  k=
  if [ -e "$p" ]; then chmod "$(stat -L -c %a -- "$p")" "$t" && { [ "$(id -u)" != 0 ] || chown "$(stat -L -c %u:%g -- "$p")" "$t"; } && cp -p -- "$p" "$p.athanor-prev" && k="; previous version kept in $p.athanor-prev"; else chmod 644 "$t"; fi && mv -f -- "$t" "$p" && echo "$2 bytes written to $1$k" && exit 0
fi
rm -f -- "$t"; exit 1`

func outilFichierEcrire(ctx context.Context, c *ssh.Client, _ ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	if a.Contenu == nil {
		return ResultatOutil{Code: -1}, errors.New("contenu is required")
	}
	if len(*a.Contenu) > tailleMaxFichier {
		return ResultatOutil{Code: -1}, fmt.Errorf("contenu is larger than %d bytes: send it with sftp_envoyer", tailleMaxFichier)
	}
	// the script follows the link itself (C32); here the target is only checked
	if _, err := resoudreDistant(ctx, c, a.Sudo, "chemin", a.Chemin, false); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	p := a.Chemin
	r, err := executerSSH(ctx, c, scriptShell(a.Sudo, scriptEcrire, p, fmt.Sprint(len(*a.Contenu))), strings.NewReader(*a.Contenu), delaiFichier)
	res := resultatSSH(r)
	if _, prev, ok := strings.Cut(res.Sortie, "previous version kept in "); ok && err == nil && res.Code == 0 {
		res.sauvegardes = []string{"previous version: " + strings.TrimSpace(strings.SplitN(prev, "\n", 2)[0])}
	}
	return res, err
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
	distant, err := distantPermis("distant", a.Distant, false)
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
	cible := distant // C40: a link is followed (it stays a link), the target keeps its mode; C36: and checked
	if st, e := cs.Lstat(distant); e == nil && st.Mode()&fs.ModeSymlink != 0 {
		if cible, err = cs.RealPath(distant); err != nil {
			return ResultatOutil{Code: -1}, fmt.Errorf("%s: %w", distant, err)
		}
	} else if r, e := cs.RealPath(distant); e == nil {
		cible = r // a link on the way (a folder)
	}
	if cible, err = cheminResolu(distant, cible, false); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	part := cible + ".athanor-part"
	df, err := cs.Create(part)
	if err != nil {
		return ResultatOutil{Code: -1}, fmt.Errorf("%s: %w", part, err)
	}
	n, err := df.ReadFrom(io.LimitReader(f, tailleMaxSFTP))
	if errF := df.Close(); err == nil {
		err = errF
	}
	prev := ""
	if st, e := cs.Stat(cible); err == nil && e == nil {
		err = cs.Chmod(part, st.Mode().Perm())
		if err == nil && st.Mode().IsRegular() { // M92: the previous version kept, mode and owner too
			prev = cible + ".athanor-prev"
			r, e := executerSSH(ctx, c, scriptShell(false, `cp -p -- "$1" "$2"`, cible, prev), nil, delaiFichier)
			if err = e; err == nil && r.Code != 0 {
				err = fmt.Errorf("keeping the previous version: %s", couper(strings.TrimSpace(string(r.Sortie)), 300))
			}
		}
	}
	if err == nil {
		err = cs.PosixRename(part, cible) // replaces the target in one step
	}
	if err != nil {
		_ = cs.Remove(part)
		return ResultatOutil{Code: -1}, fmt.Errorf("sending to %s: %w", distant, err)
	}
	sortie := fmt.Sprintf("%d bytes sent to %s", n, distant)
	r := ResultatOutil{Sortie: sortie}
	if prev != "" {
		r.Sortie += "; previous version kept in " + prev
		r.sauvegardes = []string{"previous version: " + prev}
	}
	return r, nil
}

func outilSFTPRecuperer(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	distant, err := distantPermis("distant", a.Distant, true)
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
	reel := distant // C36: opened as the server resolved it, once checked
	if r, e := cs.RealPath(distant); e == nil {
		if reel, err = cheminResolu(distant, r, true); err != nil {
			return ResultatOutil{Code: -1}, err
		}
	}
	sf, err := cs.Open(reel)
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

// The service, docker and pare_feu tools: each argument is checked against a strict form (a trapped one,
// with ;, $( or a new line, is refused) then given to a fixed script as a quoted positional parameter.
var (
	reNomService     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9@._:-]{0,127}$`)
	reNomConteneur   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	reImage          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/:@-]{0,254}$`)
	reDossierCompose = regexp.MustCompile(`^/[A-Za-z0-9._/@+,= -]{0,999}$`)
)

// preludeRoot: root's commands go through sudo -n when the user is not root and may use it.
const preludeRoot = `export LC_ALL=C PATH="$PATH:/usr/sbin:/sbin:/usr/local/sbin"
S=""; [ "$(id -u)" = 0 ] || { sudo -n true >/dev/null 2>&1 && S="sudo -n"; }
`

// choix: v must be one of permis.
func choix(nom, v string, permis ...string) error {
	if !slices.Contains(permis, v) {
		return fmt.Errorf("%s must be one of: %s", nom, strings.Join(permis, ", "))
	}
	return nil
}

// logicielAbsent: an error when the server's sheet says the program is missing (no sheet: the script says it).
func logicielAbsent(srv ServeurInventaire, nom string) error {
	var f FicheServeur
	b, err := os.ReadFile(filepath.Join(dossierModule("servers"), srv.ID, "fiche.json"))
	if err != nil || json.Unmarshal(b, &f) != nil {
		return nil
	}
	if i := slices.IndexFunc(f.Logiciels, func(l LogicielFiche) bool { return l.Nom == nom }); i >= 0 && !f.Logiciels[i].Present {
		return fmt.Errorf("%s is not installed on %s (server sheet of %s): install it, or refresh the sheet if it was installed since",
			nom, srv.Nom, f.Date.Format("2006-01-02 15:04"))
	}
	return nil
}

// scriptService ($1 action, $2 service): systemd, then OpenRC, then the SysV service command.
const scriptService = `if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then exec $S systemctl --no-pager "$1" "$2"
elif command -v rc-service >/dev/null 2>&1; then
  case $1 in enable) exec $S rc-update add "$2" default;; disable) exec $S rc-update del "$2" default;; *) exec $S rc-service "$2" "$1";; esac
elif command -v service >/dev/null 2>&1; then
  case $1 in enable|disable) command -v update-rc.d >/dev/null 2>&1 && exec $S update-rc.d "$2" "$1"
    echo "$1 needs systemd, OpenRC or update-rc.d on this server"; exit 127;; *) exec $S service "$2" "$1";; esac
fi
echo "No service manager on this server (systemctl, rc-service or service)"; exit 127`

func outilService(ctx context.Context, c *ssh.Client, _ ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	if err := choix("action", a.Action, "status", "start", "stop", "restart", "reload", "enable", "disable"); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	if !reNomService.MatchString(a.Nom) {
		return ResultatOutil{Code: -1}, errors.New("nom must be a service name (letters, digits, @ . _ : -)")
	}
	r, err := executerSSH(ctx, c, scriptShell(false, preludeRoot+scriptService, a.Action, a.Nom), nil, delaiCommande)
	res := resultatSSH(r)
	res.modifie = a.Action != "status"
	return res, err
}

// scriptDocker ($1 action, $2 container, image or compose folder, $3 log lines): docker through sudo when the
// user may not reach the daemon; compose as the docker plugin, else docker-compose.
const scriptDocker = `command -v docker >/dev/null 2>&1 || { echo "docker is not installed on this server"; exit 127; }
D=docker; docker version >/dev/null 2>&1 || { [ -n "$S" ] && D="$S docker"; }
case $1 in
  ps) exec $D ps -a;;
  logs) exec $D logs --tail "$3" "$2";;
  pull) exec $D pull "$2";;
  restart) exec $D restart "$2";;
esac
cd "$2" || exit 1
case $1 in compose_up) set -- up -d;; compose_down) set -- down;; *) set -- ps;; esac
$D compose version >/dev/null 2>&1 && exec $D compose "$@"
command -v docker-compose >/dev/null 2>&1 && exec ${D%docker}docker-compose "$@"
echo "docker compose is not installed on this server"; exit 127`

func outilDocker(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	if err := choix("action", a.Action, "ps", "logs", "pull", "restart", "compose_up", "compose_down", "compose_ps"); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	cible, lignes := "", a.Lignes
	switch a.Action {
	case "pull":
		if !reImage.MatchString(a.Nom) {
			return ResultatOutil{Code: -1}, errors.New("nom must be an image (letters, digits, . _ / : @ -)")
		}
		cible = a.Nom
	case "logs", "restart":
		if !reNomConteneur.MatchString(a.Nom) {
			return ResultatOutil{Code: -1}, errors.New("nom must be a container name (letters, digits, . _ -)")
		}
		cible = a.Nom
	case "compose_up", "compose_down", "compose_ps":
		if !reDossierCompose.MatchString(a.Dossier) {
			return ResultatOutil{Code: -1}, errors.New("dossier must be the absolute path of the compose project's folder (letters, digits, spaces, . _ / @ + , = -)")
		}
		cible = a.Dossier
	}
	if lignes == 0 {
		lignes = 200
	}
	if lignes < 1 || lignes > 10000 {
		return ResultatOutil{Code: -1}, errors.New("lignes must be between 1 and 10000")
	}
	if err := logicielAbsent(srv, "docker"); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	if strings.HasPrefix(a.Action, "compose_") {
		if err := logicielAbsent(srv, "compose"); err != nil {
			return ResultatOutil{Code: -1}, err
		}
	}
	delai := delaiCommande
	if a.Action == "pull" || a.Action == "compose_up" {
		delai = delaiCommandeMax
	}
	r, err := executerSSH(ctx, c, scriptShell(false, preludeRoot+scriptDocker, a.Action, cible, strconv.Itoa(lignes)), nil, delai)
	res := resultatSSH(r)
	res.modifie = !slices.Contains([]string{"ps", "logs", "compose_ps"}, a.Action)
	return res, err
}

// scriptPareFeu ($1 action, $2 rule kind for delete, $3 port[/proto]): ufw; the status lists the added
// rules too, which an inactive ufw does not show.
const scriptPareFeu = `command -v ufw >/dev/null 2>&1 || { echo "ufw is not installed on this server"; exit 127; }
case $1 in
  status) $S ufw status verbose && exec $S ufw show added;;
  delete) exec $S ufw --force delete "$2" "$3";;
  *) exec $S ufw "$1" "$3";;
esac`

// specPareFeu: pare_feu's arguments checked without the server; the rule as ufw takes it ("" for status).
func specPareFeu(srv ServeurInventaire, a argsOutilServeur) (string, error) {
	if err := choix("action", a.Action, "status", "allow", "deny", "delete"); err != nil || a.Action == "status" {
		return "", err
	}
	if a.Port < 1 || a.Port > 65535 {
		return "", errors.New("port must be between 1 and 65535")
	}
	if err := choix("proto", a.Proto, "", "tcp", "udp"); err != nil {
		return "", err
	}
	if a.Action == "delete" {
		if err := choix("regle", a.Regle, "allow", "deny"); err != nil {
			return "", err
		}
	}
	if coupeSSH(a) && a.Port == srv.Port {
		return "", errPortSSH(a.Port)
	}
	spec := strconv.Itoa(a.Port)
	if a.Proto != "" {
		spec += "/" + a.Proto
	}
	return spec, nil
}

// coupeSSH: the rule could close its port to Athanor (deny, or delete an allow, on TCP).
func coupeSSH(a argsOutilServeur) bool {
	return a.Proto != "udp" && (a.Action == "deny" || a.Regle == "allow")
}

func errPortSSH(p int) error {
	return fmt.Errorf("port %d is the SSH port Athanor reaches this server by: this rule could cut its access", p)
}

func outilPareFeu(ctx context.Context, c *ssh.Client, srv ServeurInventaire, a argsOutilServeur) (ResultatOutil, error) {
	spec, err := specPareFeu(srv, a)
	if err != nil {
		return ResultatOutil{Code: -1}, err
	}
	// Athanor's own way in stays open, whatever port sshd itself listens on
	if spec != "" && coupeSSH(a) && slices.Contains(portsSSHD(ctx, c), a.Port) {
		return ResultatOutil{Code: -1}, errPortSSH(a.Port)
	}
	if err := logicielAbsent(srv, "ufw"); err != nil {
		return ResultatOutil{Code: -1}, err
	}
	r, err := executerSSH(ctx, c, scriptShell(false, preludeRoot+scriptPareFeu, a.Action, a.Regle, spec), nil, delaiCommande)
	res := resultatSSH(r)
	res.modifie = a.Action != "status"
	return res, err
}
