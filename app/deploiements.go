package main

// Stage 8's deployment engine (M131, ARCHITECTURE.md §Déploiements), Go code: no shell written by an agent.
// A static deployment: the commit's tree archived here (git archive), checked (no absolute path, no "..", no
// link leaving the release, no special file), sent by SFTP, its SHA-256 checked on each server, extracted into
// /srv/athanor/<projet>-<env>/releases/<AAAAMMJJ-hhmmss>-<sha7>, the site file written for Caddy and
// validated; only then current/previous switch atomically and Caddy reloads. A failure before the switch
// leaves what is served as it was. One deployment at a time per environment; each one in its history.

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	racineDeploiements = "/srv/athanor"
	versionsGardees    = 5
	historiqueMax      = 50
	delaiDeploiement   = 5 * time.Minute  // one step on one server (extraction, validation, reload)
	delaiConstruction  = 10 * time.Minute // the build command (M135)
)

// ConfigDeploiement: how an environment is deployed, recorded after a first deployment so that the next ones
// (relay, Retry) need no agent.
type ConfigDeploiement struct {
	Methode    string `json:"methode"`              // statique | compose (M134)
	Source     string `json:"source"`               // artefact (sent by SFTP) | git (fetched by the server, M132)
	Dossier    string `json:"dossier,omitempty"`    // the published folder, relative to the repository ("" = its root)
	Construire string `json:"construire,omitempty"` // a build command run on this PC before the archive (M135)
}

// Deploiement: one entry of an environment's history.
type Deploiement struct {
	ID           string            `json:"id"`
	Projet       string            `json:"projet"`
	Env          string            `json:"env"`
	Commit       string            `json:"commit"`
	Methode      string            `json:"methode"`
	Version      string            `json:"version"`
	Etat         string            `json:"etat"` // en_cours | ok | echec | revenu
	Erreur       string            `json:"erreur,omitempty"`
	Verification *VerifDeploiement `json:"verification,omitempty"` // the external check once served (M133)
	Auteur       string            `json:"auteur"`                 // agent | relais | proprietaire
	Retour       bool              `json:"retour,omitempty"`       // a way back to the previous version (M133)
	Serveurs     []string          `json:"serveurs"`
	Debut        string            `json:"debut"`
	Fin          string            `json:"fin,omitempty"`
}

type OptionsDeploiement struct {
	Config *ConfigDeploiement // nil: the environment's recorded one
	Auteur string
}

var (
	verrousDeploiement sync.Map // "projet/env" → *sync.Mutex
	historiqueMu       sync.Mutex
	reDossierPublie    = regexp.MustCompile(`^[A-Za-z0-9_\-][A-Za-z0-9._\-]*(/[A-Za-z0-9_\-][A-Za-z0-9._\-]*)*$`)
	reCommitComplet    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func validerConfigDeploiement(c ConfigDeploiement) error {
	switch {
	case c.Methode != "statique" && c.Methode != "compose":
		return errors.New("deployment method: statique or compose")
	case c.Source != "artefact" && c.Source != "git":
		return errors.New("deployment source: artefact or git")
	case c.Dossier != "" && !reDossierPublie.MatchString(c.Dossier):
		return errors.New("dossier: a folder of the repository, relative, without \"..\", spaces or hidden parts")
	case c.Construire != "" && c.Source != "artefact":
		return errors.New("construire needs the artefact source: the build runs on this PC and its output is sent")
	case len([]rune(c.Construire)) > maxCommande || strings.ContainsFunc(c.Construire, func(r rune) bool { return r < ' ' }):
		return fmt.Errorf("construire: one line of %d characters at most", maxCommande)
	}
	return nil
}

// baseDeploiement: the environment's folder on its servers.
func baseDeploiement(projet, env string) string {
	return racineDeploiements + "/" + nomProjetServeur(projet) + "-" + env
}

// nomProjetServeur: the project's name in the servers' paths; a name outside [a-z0-9_-] gets a short hash of
// the name, so two projects never share a folder nor a key file.
func nomProjetServeur(projet string) string {
	n := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, strings.ToLower(projet))
	if n != projet {
		h := sha256.Sum256([]byte(projet))
		n += "-" + hex.EncodeToString(h[:3])
	}
	return n
}

// verifierArchive: every entry of the tar.gz stays inside the release (relative, no "..", a link only to a
// place inside it) and is a file, a folder or such a link; dossier must be in it.
func verifierArchive(chemin, dossier string) error {
	f, err := os.Open(chemin)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("unreadable archive: %w", err)
	}
	tr, trouve := tar.NewReader(gz), dossier == ""
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("unreadable archive: %w", err)
		}
		if h.Typeflag == tar.TypeXGlobalHeader { // git archive's commit id
			continue
		}
		nom := path.Clean(h.Name)
		if path.IsAbs(h.Name) || nom == ".." || strings.HasPrefix(nom, "../") || slices.Contains(strings.Split(h.Name, "/"), "..") {
			return fmt.Errorf("archive refused: %q leaves the release", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeDir:
		case tar.TypeSymlink:
			if c := path.Join(path.Dir(nom), h.Linkname); path.IsAbs(h.Linkname) || c == ".." || strings.HasPrefix(c, "../") {
				return fmt.Errorf("archive refused: the link %q points out of the release (%q)", h.Name, h.Linkname)
			}
		default:
			return fmt.Errorf("archive refused: %q is not a file, a folder or a link", h.Name)
		}
		trouve = trouve || nom == dossier || strings.HasPrefix(nom, dossier+"/")
	}
	if !trouve {
		return fmt.Errorf("the commit has no folder %q to publish", dossier)
	}
	return nil
}

// echecSSH: err, or the script's code and the end of its output when it did not exit 0.
func echecSSH(r ResultatSSH, err error) error {
	if err == nil && r.Code != 0 {
		err = fmt.Errorf("code %d: %s", r.Code, couper(strings.TrimSpace(string(r.Sortie)), 600))
	}
	return err
}

const caddyfileServeur = "/etc/caddy/Caddyfile"

// annulerVersion ($r the release, $b the environment's folder, $site the site file): the release removed and
// the site file put back as it was before this deployment.
const fonctionAnnulerVersion = `annuler() { rm -rf -- "$r"; if [ -f "$b/.site-avant" ]; then mv -f -- "$b/.site-avant" "$site"; else rm -f -- "$site"; fi; }
`

// extraireVersion ($a the archive, $2 its SHA-256 ("" for an archive git made on the server), $b the
// environment's folder, $r the release, $4 the version): the archive checked and extracted into the release.
const extraireVersion = `if [ -n "$2" ] && [ "$(sha256sum < "$a" | cut -d' ' -f1)" != "$2" ]; then rm -f -- "$a"; echo "the archive changed on the way: its SHA-256 differs on the server"; exit 3; fi
umask 022
mkdir -p -- "$b/releases" && mkdir -- "$r" || { rm -f -- "$a"; echo "the release $4 cannot be made"; exit 1; }
tar -xzf "$a" -C "$r" --no-same-owner --no-same-permissions || { rm -f -- "$a"; rm -rf -- "$r"; echo "the archive could not be extracted"; exit 1; }
rm -f -- "$a"
`

// scriptPreparer ($1 the archive sent, $2 its SHA-256 ("" for an archive git made on the server), $3 the environment's folder, $4 the version, $5 the
// published folder, $6 the host name; the site block on standard input): nothing served changes.
const scriptPreparer = `a=$1 b=$3 r=$3/releases/$4 site=/etc/caddy/sites/$6.caddy
command -v caddy >/dev/null 2>&1 && [ -f ` + caddyfileServeur + ` ] || { rm -f -- "$a"; echo "Caddy is not installed on this server (` + caddyfileServeur + ` missing): install it first"; exit 5; }
` + extraireVersion + fonctionAnnulerVersion + `mkdir -p /etc/caddy/sites || { rm -rf -- "$r"; exit 1; }
rm -f -- "$b/.site-avant"
if [ -f "$site" ]; then cp -p -- "$site" "$b/.site-avant" || { rm -rf -- "$r"; exit 1; }; fi
t=$(mktemp "$b/.site.XXXXXX") && cat > "$t" && chmod 644 "$t" && mv -f -- "$t" "$site" || { rm -f -- "$t"; annuler; echo "the site file cannot be written"; exit 1; }
grep -qxF 'import /etc/caddy/sites/*' ` + caddyfileServeur + ` || printf '\nimport /etc/caddy/sites/*\n' >> ` + caddyfileServeur + ` || { annuler; exit 1; }
o=$(caddy validate --config ` + caddyfileServeur + ` --adapter caddyfile 2>&1) || { annuler; echo "caddy validate refused the configuration:"; printf '%s\n' "$o" | tail -n 4; exit 4; }
echo ready`

// scriptAnnulerVersion ($1 folder, $2 version, $3 host): a release prepared on this server while another server failed.
const scriptAnnulerVersion = `b=$1 r=$1/releases/$2 site=/etc/caddy/sites/$3.caddy
` + fonctionAnnulerVersion + `annuler`

// scriptBascule ($1 folder, $2 version): current then previous replaced in one rename each.
const scriptBascule = `cd -- "$1" || exit 1
v=releases/$2 old=$(readlink current 2>/dev/null)
ln -sfn "$v" .current.new && mv -fT .current.new current || exit 1
if [ -n "$old" ] && [ "$old" != "$v" ]; then ln -sfn "$old" .previous.new && mv -fT .previous.new previous; fi`

// scriptRecharger ($1 folder, $2 versions kept): Caddy reloaded (systemd, else its own command), then the
// oldest releases removed (scriptNettoyer).
const scriptRecharger = `if [ -d /run/systemd/system ] && systemctl is-active --quiet caddy 2>/dev/null; then o=$(systemctl reload caddy 2>&1); else o=$(caddy reload --config ` + caddyfileServeur + ` --adapter caddyfile 2>&1); fi || { echo "Caddy did not reload:"; printf '%s\n' "$o" | tail -n 4; exit 1; }
` + scriptNettoyer

// scriptNettoyer ($1 folder, $2 versions kept): the oldest releases removed, never current nor previous.
const scriptNettoyer = `cd -- "$1/releases" || exit 0
c=$(readlink ../current) p=$(readlink ../previous 2>/dev/null)
ls -1 | sort -r | tail -n +$(($2 + 1)) | while IFS= read -r v; do [ "releases/$v" = "$c" ] || [ "releases/$v" = "$p" ] || rm -rf -- "$v"; done`

// deployer: commit of projet deployed to environment env (ARCHITECTURE.md §Déploiements, Moteur); the history
// keeps the attempt whatever happens; the method is recorded in the environment once it went through.
func (s *Serveur) deployer(ctx context.Context, projet, env, commit string, o OptionsDeploiement) (Deploiement, error) {
	d, suite, err := s.commencerDeploiement(projet, env, commit, o)
	if err != nil {
		return d, err
	}
	return suite(ctx)
}

// commencerDeploiement (M135): the checks, the environment's lock and the history's "en_cours" entry; suite runs
// the deployment and frees the lock, it must be called once (a hub request runs it in the background).
func (s *Serveur) commencerDeploiement(projet, env, commit string, o OptionsDeploiement) (Deploiement, func(context.Context) (Deploiement, error), error) {
	e, ok := s.acces.Environnements(projet)[env]
	if !ok {
		return Deploiement{}, nil, errors.New("unknown environment")
	}
	cfg := e.Deploiement
	if o.Config != nil {
		cfg = o.Config
	}
	if cfg == nil {
		return Deploiement{}, nil, errors.New("this environment has no deployment method yet: a first deployment is made by a Servers task")
	}
	if err := validerConfigDeploiement(*cfg); err != nil {
		return Deploiement{}, nil, err
	}
	p, ok := s.projetNomme(projet)
	if !ok {
		return Deploiement{}, nil, errors.New("unknown project")
	}
	if len(e.Serveurs) == 0 || hoteEnv(&e) == "" {
		return Deploiement{}, nil, errors.New("the environment needs a URL and at least one server")
	}
	if cfg.Source == "git" && e.CleDeploiement == "" {
		return Deploiement{}, nil, errors.New("the git source needs the project's deployment key: create it in its Environments panel (or use the artefact source)")
	}
	v, _ := verrousDeploiement.LoadOrStore(projet+"/"+env, &sync.Mutex{})
	verrou := v.(*sync.Mutex)
	if !verrou.TryLock() {
		return Deploiement{}, nil, errors.New("a deployment of this environment is already running")
	}
	sha, err := gitSortie(p.Dossier, "rev-parse", "--verify", "--quiet", "--end-of-options", commit+"^{commit}")
	if err != nil || !reCommitComplet.MatchString(sha) {
		verrou.Unlock()
		return Deploiement{}, nil, fmt.Errorf("commit %q is not in the project's repository", commit)
	}
	debut := time.Now().UTC()
	d := Deploiement{ID: aleatoire(8), Projet: projet, Env: env, Commit: sha, Methode: cfg.Methode, Version: debut.Format("20060102-150405") + "-" + sha[:7],
		Etat: "en_cours", Auteur: o.Auteur, Serveurs: e.Serveurs, Debut: debut.Format(time.RFC3339)}
	if err := noterHistorique(d); err != nil {
		verrou.Unlock()
		return d, nil, fmt.Errorf("the deployment history cannot be written: %w", err)
	}
	return d, func(ctx context.Context) (Deploiement, error) {
		defer verrou.Unlock()
		return s.finirDeploiement(ctx, p, e, *cfg, d)
	}, nil
}

// finirDeploiement: the release deployed, the method recorded once it went through, the external check.
func (s *Serveur) finirDeploiement(ctx context.Context, p Projet, e Environnement, cfg ConfigDeploiement, d Deploiement) (Deploiement, error) {
	err := s.deployerVersion(ctx, p, e, cfg, &d)
	d.Etat, d.Fin = "ok", time.Now().UTC().Format(time.RFC3339)
	if err != nil {
		d.Etat, d.Erreur = "echec", err.Error()
	} else {
		if e2 := s.acces.NoterDeploiement(d.Projet, d.Env, cfg); e2 != nil {
			d.Erreur = "deployed, but the method could not be recorded: " + e2.Error()
		}
		err = s.verifierApres(ctx, e, &d)
	}
	if e2 := noterHistorique(d); err == nil && e2 != nil {
		err = fmt.Errorf("deployed, but the history cannot be written: %w", e2)
	}
	return d, err
}

// deployerVersion: the commit prepared on every server (statique: the Caddy site validated; compose: the
// containers started from the release), then current/previous switched everywhere.
func (s *Serveur) deployerVersion(ctx context.Context, p Projet, e Environnement, cfg ConfigDeploiement, d *Deploiement) error {
	dossier := filepath.Join(dossierModule("servers"), "deploiements")
	if err := os.MkdirAll(dossier, 0o700); err != nil {
		return err
	}
	archive := filepath.Join(dossier, d.ID+".tar.gz")
	defer os.Remove(archive)
	if cfg.Construire != "" {
		if err := s.construireArchive(ctx, p.Dossier, d.Commit, cfg, archive); err != nil {
			return err
		}
	} else if out, err := gitP(p.Dossier, nil, "archive", "--format=tar.gz", "-o", archive, d.Commit).CombinedOutput(); err != nil {
		return fmt.Errorf("git archive: %v %s", err, couper(strings.TrimSpace(string(out)), 300))
	}
	if err := verifierArchive(archive, cfg.Dossier); err != nil {
		return err
	}
	somme, err := sha256Fichier(archive)
	if err != nil {
		return err
	}
	var g *sourceGit
	if cfg.Source == "git" { // the commit's archive above is only checked: each server fetches the same tree
		url, hotes, err := depotGitServeur(p)
		if err != nil {
			return err
		}
		cle, err := s.coffre.Valeur(e.CleDeploiement)
		if err != nil {
			return fmt.Errorf("the deployment key cannot be read from the vault: %w", err)
		}
		g, somme = &sourceGit{url, hotes, cle, "/etc/athanor/deploy/" + nomProjetServeur(d.Projet)}, ""
	}
	l, err := lireServeurs()
	if err != nil {
		return err
	}
	base, hote := baseDeploiement(d.Projet, d.Env), hoteEnv(&e)
	var envCompose, genere string
	if cfg.Methode == "compose" {
		if envCompose, err = s.fichierEnvCompose(d.Projet, d.Env); err != nil {
			return err
		}
		if genere, err = composeGenere(p.Dossier, d.Commit, cfg.Dossier); err != nil {
			return err
		}
	}
	racine := base + "/current"
	if cfg.Dossier != "" {
		racine += "/" + cfg.Dossier
	}
	type pret struct {
		id string
		c  *ssh.Client
	}
	var prets []pret
	defer func() {
		for _, x := range prets {
			x.c.Close()
		}
	}()
	annulerTout := func() {
		for _, x := range prets {
			annuler := commeRoot(scriptAnnulerVersion, base, d.Version, hote)
			if cfg.Methode == "compose" {
				annuler = commeRoot(scriptAnnulerCompose, base, d.Version, cfg.Dossier, nomCompose(d.Projet, d.Env))
			}
			_, _ = executerSSH(context.WithoutCancel(ctx), x.c, annuler, nil, delaiDeploiement)
		}
	}
	for _, id := range d.Serveurs {
		script, args, entree := scriptPreparerCompose, []string{somme, base, d.Version, cfg.Dossier, nomCompose(d.Projet, d.Env), genere}, envCompose
		if cfg.Methode == "statique" {
			bloc, err := s.blocCaddyEnv(d.Projet, d.Env, id, racine)
			if err != nil {
				annulerTout()
				return err
			}
			script, args, entree = scriptPreparer, []string{somme, base, d.Version, cfg.Dossier, hote}, bloc
		}
		c, err := s.preparerVersion(ctx, l, id, archive, d.ID, g, d.Commit, base, d.Version, script, args, entree)
		if err != nil {
			annulerTout()
			return fmt.Errorf("%s: %w; nothing was switched", id, err)
		}
		prets = append(prets, pret{id, c})
	}
	// every server ready: the switch, then the reload, server by server
	for _, x := range prets {
		if err := echecSSH(executerSSH(ctx, x.c, commeRoot(scriptBascule, base, d.Version), nil, delaiCommande)); err != nil {
			return fmt.Errorf("%s: switch: %w", x.id, err)
		}
	}
	recharger := scriptRecharger
	if cfg.Methode == "compose" { // the containers already run the release
		recharger = scriptNettoyer
	}
	for _, x := range prets {
		if err := echecSSH(executerSSH(ctx, x.c, commeRoot(recharger, base, fmt.Sprint(versionsGardees)), nil, delaiDeploiement)); err != nil {
			return fmt.Errorf("%s: %w", x.id, err)
		}
	}
	return nil
}

// construireArchive (M135): the commit checked out in a clone of its own, the build command run there on this PC
// (sh, Git Bash on Windows; time-limited; without Athanor's secrets in its environment), then that tree archived:
// what git tracks plus the published folder even when ignored (a dist/ usually is).
func (s *Serveur) construireArchive(ctx context.Context, depot, commit string, cfg ConfigDeploiement, archive string) error {
	src := strings.TrimSuffix(archive, ".tar.gz") + "-src"
	defer os.RemoveAll(src)
	git := func(dossier string, args ...string) (string, error) {
		out, err := gitP(dossier, nil, args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git %s: %v %s", args[0], err, couper(strings.TrimSpace(string(out)), 300))
		}
		return strings.TrimSpace(string(out)), nil
	}
	// the project's own repository only (protocol.allow=never stays for everything else)
	if _, err := git(filepath.Dir(src), "-c", "protocol.file.allow=always", "clone", "--quiet", "--shared", "--no-checkout", "--", depot, src); err != nil {
		return err
	}
	if _, err := git(src, "-c", "advice.detachedHead=false", "checkout", "--quiet", "--detach", commit); err != nil {
		return err
	}
	sh := "sh"
	if runtime.GOOS == "windows" {
		if sh = bashNatif(); sh == "" {
			return errors.New("the build command needs Git for Windows' bash on this PC")
		}
	}
	cctx, fin := context.WithTimeout(ctx, delaiConstruction)
	defer fin()
	c := commandeContexte(cctx, sh, "-c", cfg.Construire)
	c.Dir, c.Env, c.WaitDelay = src, envSansSecrets(s, nil), 10*time.Second
	cacherFenetre(c)
	if out, err := c.CombinedOutput(); err != nil {
		l := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("the build failed (%v): %s; nothing was switched", err, couper(s.masquerSecrets(strings.Join(l[max(0, len(l)-6):], "\n")), 600))
	}
	if _, err := git(src, "add", "-A"); err != nil {
		return err
	}
	if cfg.Dossier != "" {
		if _, err := git(src, "add", "-f", "--", cfg.Dossier); err != nil {
			return fmt.Errorf("the published folder %s is missing after the build: %w", cfg.Dossier, err)
		}
	}
	arbre, err := git(src, "write-tree")
	if err != nil {
		return err
	}
	_, err = git(src, "archive", "--format=tar.gz", "-o", archive, arbre)
	return err
}

// preparerVersion: on server id, the archive sent (or with g, the commit fetched there by git), then script run
// as root with the archive's remote path and args, entree on its standard input; the connection stays open for
// the switch (closed on failure).
func (s *Serveur) preparerVersion(ctx context.Context, l []ServeurInventaire, id, archive, nom string, g *sourceGit, commit, base, version, script string, args []string, entree string) (*ssh.Client, error) {
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		return nil, errServeurInconnu
	}
	c, err := s.connecterServeur(ctx, l[i])
	if err != nil {
		return nil, err
	}
	var distant string
	if g != nil {
		distant, err = recupererGit(ctx, c, g, base, commit, version)
	} else {
		distant, err = envoyerArchive(ctx, c, archive, nom)
	}
	if err == nil {
		err = echecSSH(executerSSH(ctx, c, commeRoot(script, append([]string{distant}, args...)...), strings.NewReader(entree), delaiDeploiement))
	}
	if err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// envoyerArchive: local sent by SFTP into ~/.athanor-deploy (0700) of the login user; the answer: its remote path.
func envoyerArchive(ctx context.Context, c *ssh.Client, local, nom string) (string, error) {
	cs, fin, err := clientSFTP(ctx, c)
	if err != nil {
		return "", err
	}
	defer fin()
	home, err := cs.Getwd()
	if err != nil {
		return "", err
	}
	d := path.Join(home, ".athanor-deploy")
	if err := cs.MkdirAll(d); err != nil {
		return "", err
	}
	if err := cs.Chmod(d, 0o700); err != nil {
		return "", err
	}
	src, err := os.Open(local)
	if err != nil {
		return "", err
	}
	defer src.Close()
	distant := path.Join(d, nom+".tar.gz")
	dst, err := cs.OpenFile(distant, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return "", fmt.Errorf("sending the archive: %w", err)
	}
	return distant, dst.Close()
}

// shortcut: the history follows the project's name; renaming the project starts a new one (move the file in RenommerProjet if that matters).
func cheminHistorique(projet, env string) string {
	return filepath.Join(dossierModule("servers"), "deploiements", projet+"-"+env+".json")
}

func historiqueDeploiements(projet, env string) []Deploiement {
	var l []Deploiement
	lireJSONFichier(cheminHistorique(projet, env), &l)
	return l
}

// noterHistorique: d added to its environment's history, or replacing its own entry; the newest historiqueMax kept.
func noterHistorique(d Deploiement) error {
	historiqueMu.Lock()
	defer historiqueMu.Unlock()
	l := historiqueDeploiements(d.Projet, d.Env)
	if i := slices.IndexFunc(l, func(x Deploiement) bool { return x.ID == d.ID }); i >= 0 {
		l[i] = d
	} else {
		l = append(l, d)
	}
	if len(l) > historiqueMax {
		l = l[len(l)-historiqueMax:]
	}
	return ecrireJSONFichier(cheminHistorique(d.Projet, d.Env), l, 0o600)
}
