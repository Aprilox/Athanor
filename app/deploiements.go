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
	Methode    string `json:"methode"`              // statique | compose (M134) | k3s (M139, only when asked)
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
	Auteur       string            `json:"auteur"`                 // agent | relais | proprietaire | auto (C76: the automatic way back)
	Retour       bool              `json:"retour,omitempty"`       // a way back to the previous version (M133)
	Accorde      bool              `json:"accorde,omitempty"`      // the owner accepted this very deployment (C68)
	Bascule      bool              `json:"bascule,omitempty"`      // C71: its version was served on every server (a failure before it changed nothing)
	Dossier      *string           `json:"dossier,omitempty"`      // the published folder of this version (C74: going back uses its own; nil before M145)
	Amont        string            `json:"amont,omitempty"`        // compose: the 127.0.0.1:n Caddy proxies to, "" without a site (C70)
	Serveurs     []string          `json:"serveurs"`
	Debut        string            `json:"debut"`
	Fin          string            `json:"fin,omitempty"`
}

type OptionsDeploiement struct {
	Config  *ConfigDeploiement // nil: the environment's recorded one
	Auteur  string
	Accorde bool // the owner accepted it (an accord, a plan, Retry): its build files are the new reference (C68)
}

var (
	verrousDeploiement sync.Map // "projet/env" → *sync.Mutex
	historiqueMu       sync.Mutex
	reDossierPublie    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._\-]*(/[A-Za-z0-9_][A-Za-z0-9._\-]*)*$`) // B82: never "-…", an option for a command
	reCommitComplet    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

const maxDossierPublie = 300

func validerConfigDeploiement(c ConfigDeploiement) error {
	switch {
	case c.Methode != "statique" && c.Methode != "compose" && c.Methode != "k3s":
		return errors.New("deployment method: statique, compose or k3s")
	case c.Source != "artefact" && c.Source != "git":
		return errors.New("deployment source: artefact or git")
	case len(c.Dossier) > maxDossierPublie || c.Dossier != "" && !reDossierPublie.MatchString(c.Dossier):
		return fmt.Errorf("dossier: a folder of the repository, relative, of %d characters at most, without \"..\", spaces, hidden parts or a part starting with \"-\"", maxDossierPublie)
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
// place inside it, links of the archive followed) and is a file, a folder or such a link; dossier must be in it.
// The servers check the links again once extracted (extraireVersion).
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
	liens, noms := map[string]string{}, []string(nil)
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
			liens[nom] = h.Linkname
		default:
			return fmt.Errorf("archive refused: %q is not a file, a folder or a link", h.Name)
		}
		noms = append(noms, nom)
		trouve = trouve || nom == dossier || strings.HasPrefix(nom, dossier+"/")
	}
	for _, n := range noms { // B75: once every link is known, as the server will follow them (chains included)
		if dansVersion(n, liens) {
			continue
		}
		if l, ok := liens[n]; ok {
			return fmt.Errorf("archive refused: the link %q points out of the release (%q)", n, l)
		}
		return fmt.Errorf("archive refused: %q leaves the release through a link", n)
	}
	if !trouve {
		return fmt.Errorf("the commit has no folder %q to publish", dossier)
	}
	return nil
}

// dansVersion (B75): p followed from the release's root through the archive's links, each step resolved before
// the next ".." (not lexically); false when it climbs above the root, meets an absolute link or loops.
func dansVersion(p string, liens map[string]string) bool {
	var pile []string
	reste, sauts := strings.Split(p, "/"), 0
	for len(reste) > 0 {
		c := reste[0]
		reste = reste[1:]
		switch c {
		case "", ".":
			continue
		case "..":
			if len(pile) == 0 {
				return false
			}
			pile = pile[:len(pile)-1]
			continue
		}
		pile = append(pile, c)
		l, ok := liens[strings.Join(pile, "/")]
		if !ok {
			continue
		}
		if sauts++; sauts > 40 || path.IsAbs(l) {
			return false
		}
		pile = pile[:len(pile)-1]
		reste = append(strings.Split(l, "/"), reste...)
	}
	return true
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
rr=$(readlink -f -- "$r") && x=$(find "$r" -type l -exec readlink -m -- {} + | while IFS= read -r t; do case "$t" in "$rr"|"$rr"/*) ;; *) echo "$t"; break;; esac; done) && [ -z "$x" ] || { rm -rf -- "$r"; echo "archive refused: a link of the release points out of it once extracted"; exit 1; }
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

// scriptBascule ($1 folder, $2 version): current then previous replaced in one rename each; prints first the
// links as they were (current, previous; empty when missing) so that scriptRemettre can put them back (C71).
// Idempotent: the version already served stays current, previous unchanged.
const scriptBascule = `cd -- "$1" || exit 1
v=releases/$2 old=$(readlink current 2>/dev/null) p=$(readlink previous 2>/dev/null)
printf '%s\n%s\n' "$old" "$p"
ln -sfn "$v" .current.new && mv -fT .current.new current || exit 1
if [ -n "$old" ] && [ "$old" != "$v" ]; then ln -sfn "$old" .previous.new && mv -fT .previous.new previous; fi`

// scriptRemettre ($1 folder, $2 current, $3 previous, as scriptBascule printed them): the links put back.
const scriptRemettre = `cd -- "$1" || exit 1
if [ -n "$2" ]; then ln -sfn "$2" .current.new && mv -fT .current.new current || exit 1; else rm -f current; fi
if [ -n "$3" ]; then ln -sfn "$3" .previous.new && mv -fT .previous.new previous; else rm -f previous; fi`

// serveurPret: a server connected for a deployment or a way back.
type serveurPret struct {
	id string
	c  *ssh.Client
}

// liensAvant: a server's links before scriptBascule, to put back.
type liensAvant struct {
	serveurPret
	current, previous string
}

// echecBasculeTest (tests only): the switch of the n-th server reported failed once done.
var echecBasculeTest func(n int) bool

// basculerTous (C71): version served on every server, one after the other; on a failure, the links of every
// server touched (the failing one included) are put back, newest first, and touches is empty.
func basculerTous(ctx context.Context, serveurs []serveurPret, base, version string) (touches []liensAvant, err error) {
	for n, x := range serveurs {
		r, e := executerSSH(ctx, x.c, commeRoot(scriptBascule, base, version), nil, delaiCommande)
		if l := strings.SplitN(string(r.Sortie), "\n", 3); len(l) == 3 {
			touches = append(touches, liensAvant{x, l[0], l[1]})
		}
		if e = echecSSH(r, e); e == nil && echecBasculeTest != nil && echecBasculeTest(n) {
			e = errors.New("test failure")
		}
		if e != nil {
			remettreLiens(ctx, touches, base)
			return nil, fmt.Errorf("%s: switch: %w", x.id, e)
		}
	}
	return touches, nil
}

// remettreLiens: the links of touches put back, newest first (best effort: the history says what failed).
func remettreLiens(ctx context.Context, touches []liensAvant, base string) {
	for _, x := range slices.Backward(touches) {
		_, _ = executerSSH(context.WithoutCancel(ctx), x.c, commeRoot(scriptRemettre, base, x.current, x.previous), nil, delaiCommande)
	}
}

// avantBascule (C71): a failure that changed nothing served (nothing to go back from).
func avantBascule(d Deploiement) bool {
	return d.Etat == "echec" && !d.Retour && !d.Bascule && d.Verification == nil
}

// retourAutomatique (C76, ARCHITECTURE §Moteur): a version served then found failing goes back by itself in
// dev and staging under the auto policy; never in prod.
func retourAutomatique(e Environnement, d Deploiement) bool {
	return d.Etat == "echec" && d.Bascule && d.Verification != nil && !d.Verification.OK && d.Env != "prod" && e.Politique == "auto"
}

// scriptRecharger ($1 folder, $2 versions kept): Caddy reloaded when it is there (systemd, else its own command), then the
// oldest releases removed (scriptNettoyer).
const scriptRecharger = `if ! command -v caddy >/dev/null 2>&1; then o=; elif [ -d /run/systemd/system ] && systemctl is-active --quiet caddy 2>/dev/null; then o=$(systemctl reload caddy 2>&1); else o=$(caddy reload --config ` + caddyfileServeur + ` --adapter caddyfile 2>&1); fi || { echo "Caddy did not reload:"; printf '%s\n' "$o" | tail -n 4; exit 1; }
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
	if cfg.Methode == "k3s" && len(e.Serveurs) != 1 {
		return Deploiement{}, nil, errK3sUnServeur
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
	debut, dossier := time.Now().UTC(), cfg.Dossier
	d := Deploiement{ID: aleatoire(8), Projet: projet, Env: env, Commit: sha, Methode: cfg.Methode, Version: debut.Format("20060102-150405") + "-" + sha[:7],
		Etat: "en_cours", Auteur: o.Auteur, Accorde: o.Accorde, Dossier: &dossier, Serveurs: e.Serveurs, Debut: debut.Format(time.RFC3339)}
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
	d.Erreur = s.masquerSecrets(d.Erreur) // B77: a server may echo what it read (key, .env); the history reaches the phone
	if e2 := noterHistorique(d); err == nil && e2 != nil {
		err = fmt.Errorf("deployed, but the history cannot be written: %w", e2)
	}
	if retourAutomatique(e, d) { // after the failure is noted: the way back marks it revenu
		if r, e2 := s.revenirVerrouille(ctx, d.Projet, d.Env, "auto", ""); e2 != nil {
			d.Erreur += "; the automatic way back failed: " + couper(e2.Error(), 300)
		} else {
			d.Etat, d.Erreur = "revenu", d.Erreur+"; went back automatically to "+r.Version
		}
		d.Erreur = s.masquerSecrets(d.Erreur)
		_ = noterHistorique(d)
	}
	return d, err
}

var errK3sUnServeur = errors.New("the k3s method needs exactly one server in the environment: its cluster spreads the pods itself")

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
		if err := s.construireArchive(ctx, p, d.Commit, cfg, archive); err != nil {
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
	// C69: without the owner's accord for this very deployment (auto policy, a rule, the relay), nothing that
	// gives root on the server: the k3s manifests read here, the Compose configuration on each server below
	garde := !d.Accorde && d.Auteur != "proprietaire"
	refus := func(r []string) error {
		return fmt.Errorf("refused without the owner's accord, this would give root on the server: %s; the owner deploys it from the environment's card, or sets its policy to accord; nothing was switched", couper(strings.Join(r, "; "), 400))
	}
	if garde && cfg.Methode == "k3s" {
		if r := refusK3s(p.Dossier, d.Commit, cfg.Dossier); len(r) > 0 {
			return refus(r)
		}
	}
	racine := base + "/current"
	if cfg.Dossier != "" {
		racine += "/" + cfg.Dossier
	}
	var prets []serveurPret
	defer func() {
		for _, x := range prets {
			x.c.Close()
		}
	}()
	annulerTout := func() {
		for _, x := range prets {
			annuler := commeRoot(scriptAnnulerVersion, base, d.Version, hote)
			switch cfg.Methode {
			case "compose":
				annuler = commeRoot(scriptAnnulerCompose, base, d.Version, cfg.Dossier, nomCompose(d.Projet, d.Env), hote)
			case "k3s":
				annuler = commeRoot(scriptAnnulerK3s, base, d.Version, cfg.Dossier)
			}
			_, _ = executerSSH(context.WithoutCancel(ctx), x.c, annuler, nil, delaiDeploiement)
		}
	}
	for _, id := range d.Serveurs {
		script, args, entree := scriptPreparerCompose, []string{somme, base, d.Version, cfg.Dossier, nomCompose(d.Projet, d.Env), genere, hote}, envCompose
		if cfg.Methode == "statique" {
			bloc, err := s.blocCaddyEnv(d.Projet, d.Env, id, racine)
			if err != nil {
				annulerTout()
				return err
			}
			script, args, entree = scriptPreparer, []string{somme, base, d.Version, cfg.Dossier, hote}, bloc
		} else if cfg.Methode == "k3s" {
			script, args, entree = scriptPreparerK3s, []string{somme, base, d.Version, cfg.Dossier}, ""
		}
		c, sortie, err := s.preparerVersion(ctx, l, id, archive, d.ID, g, d.Commit, base, d.Version, script, args, entree)
		if err != nil {
			annulerTout()
			return fmt.Errorf("%s: %w; nothing was switched", id, err)
		}
		prets = append(prets, serveurPret{id, c})
		if cfg.Methode != "compose" {
			continue
		}
		if r := refusCompose(sortie, base+"/releases/"+d.Version); garde && len(r) > 0 {
			annulerTout()
			return fmt.Errorf("%s: %w", id, refus(r))
		}
		bloc := "" // C70: Caddy in front of the first port published on 127.0.0.1, with the protection; none: no site
		if d.Amont = amontCompose(sortie); d.Amont != "" {
			if bloc, err = s.blocCaddyEnv(d.Projet, d.Env, id, d.Amont); err != nil {
				annulerTout()
				return fmt.Errorf("%s: %w; nothing was switched", id, err)
			}
		}
		if err := echecSSH(executerSSH(ctx, c, commeRoot(scriptLancerCompose, base, d.Version, cfg.Dossier, nomCompose(d.Projet, d.Env), hote), strings.NewReader(bloc), delaiDeploiement)); err != nil {
			prets = prets[:len(prets)-1] // the release is already removed and the version served before started again
			c.Close()
			annulerTout()
			return fmt.Errorf("%s: %w; nothing was switched", id, err)
		}
	}
	// every server ready: the switch everywhere (C71: on a failure, the links put back and the releases removed),
	// then the reload, server by server
	if _, err := basculerTous(ctx, prets, base, d.Version); err != nil {
		annulerTout()
		return fmt.Errorf("%w; nothing was switched", err)
	}
	d.Bascule = true
	recharger := scriptRecharger
	if cfg.Methode == "k3s" { // the pods already run the release
		recharger = scriptNettoyer
	}
	for _, x := range prets {
		if err := echecSSH(executerSSH(ctx, x.c, commeRoot(recharger, base, fmt.Sprint(versionsGardees)), nil, delaiDeploiement)); err != nil {
			return fmt.Errorf("%s: %w", x.id, err)
		}
	}
	return nil
}

// construireArchive (M135, C68): the build command run by the kit's construire-deploiement (on the pilot's
// side, in its barrier, in a temporary folder outside Athanor's; without Athanor's secrets in its
// environment); in native Windows mode, only for a project forced on its Access page (logged).
func (s *Serveur) construireArchive(ctx context.Context, p Projet, commit string, cfg ConfigDeploiement, archive string) error {
	k := s.kitCourant()
	args := []string{"construire-deploiement", k.Racine, p.Dossier, commit, cfg.Dossier, archive}
	if modeNatif.Load() {
		if s.acces == nil || !s.acces.Projet(p.Nom).ForcerInfraNatif {
			return errConstruireSansBarriere
		}
		args = append(args, "--sans-barriere")
		s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "hub", Projet: p.Nom, Action: "construction sans barrière",
			Resultat: "commit " + commit + " (projet forcé en mode Windows natif)"})
	}
	c := commandeConstruction(k, append(args, "--", cfg.Construire)...)
	c.Env = envSansSecrets(s, c.Env)
	var journal strings.Builder
	c.Stdout, c.Stderr, c.WaitDelay = &journal, &journal, 10*time.Second
	cacherFenetre(c)
	if err := c.Start(); err != nil {
		return err
	}
	cctx, fin := context.WithTimeout(ctx, delaiConstruction+time.Minute)
	defer fin()
	stop := context.AfterFunc(cctx, func() { _ = c.Process.Kill() })
	err := c.Wait()
	stop()
	if err != nil {
		l := strings.Split(strings.TrimSpace(nettoyerANSI(journal.String())), "\n")
		return fmt.Errorf("the build failed (%v): %s; nothing was switched", err, couper(s.masquerSecrets(strings.Join(l[max(0, len(l)-6):], "\n")), 600))
	}
	return nil
}

// commandeConstruction: the kit's command (replaced in the tests, whose binary is not the kit's).
var commandeConstruction = commandeKit

// preparerVersion: on server id, the archive sent (or with g, the commit fetched there by git), then script run
// as root with the archive's remote path and args, entree on its standard input; the connection stays open for
// the switch (closed on failure).
func (s *Serveur) preparerVersion(ctx context.Context, l []ServeurInventaire, id, archive, nom string, g *sourceGit, commit, base, version, script string, args []string, entree string) (*ssh.Client, string, error) {
	i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		return nil, "", errServeurInconnu
	}
	c, err := s.connecterServeur(ctx, l[i])
	if err != nil {
		return nil, "", err
	}
	var distant string
	if g != nil {
		distant, err = recupererGit(ctx, c, g, base, commit, version)
	} else {
		distant, err = envoyerArchive(ctx, c, archive, nom)
	}
	var r ResultatSSH
	if err == nil {
		r, err = executerSSH(ctx, c, commeRoot(script, append([]string{distant}, args...)...), strings.NewReader(entree), delaiDeploiement)
		err = echecSSH(r, err)
	}
	if err != nil {
		c.Close()
		return nil, "", err
	}
	return c, string(r.Sortie), nil
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
