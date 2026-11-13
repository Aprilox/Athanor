package main

// athanor construire-deploiement KIT DEPOT COMMIT DOSSIER ARCHIVE [--sans-barriere] -- COMMAND (C68): a
// deployment's build command, run on the pilot's side and inside the pilot's barrier (athanor mcp-cage:
// bubblewrap in WSL and Linux, Seatbelt on macOS), in a temporary folder outside Athanor's: the commit's
// tree is written there, the command builds it, and only then is the release archived from a git index and
// object folder created after the build (a build cannot plant a configuration, a filter or a hook in them).
// Native Windows has no barrier: the command runs there only with --sans-barriere, which the application
// passes when the project is forced (Access page, logged).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// constructionSansCage: the build runs without bubblewrap (Native Windows); set only by the test binary on a Linux
// without bwrap (deploiements_construire_test.go), never by the product.
var constructionSansCage = runtime.GOOS == "windows"

var errConstruireSansBarriere = errors.New("Native Windows mode has no barrier: a deployment's build command could reach every file of this PC, so it is refused. Switch to WSL in Settings › Options, or tick Force, knowing the risk on this project's Access page.")

func lancerConstruireDeploiement(args []string) int {
	sans, i := false, 0
	var pos []string
	for ; i < len(args) && args[i] != "--"; i++ {
		if args[i] == "--sans-barriere" {
			sans = true
		} else {
			pos = append(pos, args[i])
		}
	}
	if len(pos) != 5 || i != len(args)-2 {
		fmt.Fprintln(os.Stderr, "usage: construire-deploiement KIT DEPOT COMMIT DOSSIER ARCHIVE [--sans-barriere] -- COMMAND")
		return 2
	}
	kit, commandeBuild := cheminOutil(pos[0]), args[i+1]
	var cage func(src string) (*exec.Cmd, error)
	if constructionSansCage {
		if !sans {
			fmt.Fprintln(os.Stderr, errConstruireSansBarriere)
			return 1
		}
		cage = func(src string) (*exec.Cmd, error) {
			sh := bashNatif()
			if runtime.GOOS != "windows" {
				sh = "sh" // only the tests without bubblewrap come here off Windows
			}
			if sh == "" {
				return nil, errors.New("the build command needs Git for Windows' bash on this PC")
			}
			c := commande(sh, "-c", commandeBuild)
			c.Dir = src
			return c, nil
		}
	} else {
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		cage = func(src string) (*exec.Cmd, error) {
			return exec.Command(exe, "mcp-cage", "construire", filepath.Join(kit, "tools"), "--rw", src, "--", "sh", "-c", commandeBuild), nil
		}
	}
	if err := construireDeploiement(cheminOutil(pos[1]), pos[2], pos[3], cheminOutil(pos[4]), cage, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// construireDeploiement: commit of the repository depot written into a temporary folder, cage(src) run there
// (its output to journal), then that folder archived into archive: what the commit tracks, what the build
// added that git does not ignore, and the published folder even when ignored (a dist/ usually is).
func construireDeploiement(depot, commit, dossier, archive string, cage func(src string) (*exec.Cmd, error), journal io.Writer) error {
	gitDir, err := gitSortie(depot, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return errMoteur("{0} is not a git repository", depot)
	}
	commun, err := gitSortie(depot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	sures := optionsSuresGit(depot, nil) // the project's own filters and hooks never run here
	tmp, err := os.MkdirTemp("", "athanor-construire-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "src")
	if err := os.Mkdir(src, 0o700); err != nil {
		return err
	}
	git := func(prive string, args ...string) (string, error) {
		c := commande("git", append(append([]string{"--git-dir=" + gitDir, "--work-tree=" + src}, sures...), args...)...)
		c.Dir = src
		// the index and the new objects in a folder of their own, the project's objects read only
		c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_INDEX_FILE="+filepath.Join(prive, "index"),
			"GIT_OBJECT_DIRECTORY="+filepath.Join(prive, "objects"), "GIT_ALTERNATE_OBJECT_DIRECTORIES="+filepath.Join(commun, "objects"))
		var erreurs strings.Builder
		c.Stderr = &erreurs
		cacherFenetre(c)
		out, err := c.Output()
		if err != nil {
			return "", errMoteur("git {0}: {1} {2}", args[0], err, couper(strings.TrimSpace(erreurs.String()), 300))
		}
		return strings.TrimSpace(string(out)), nil
	}
	avant, err := os.MkdirTemp("", "athanor-construire-git-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(avant)
	if err := os.Mkdir(filepath.Join(avant, "objects"), 0o700); err != nil {
		return err
	}
	if _, err := git(avant, "read-tree", commit); err != nil {
		return err
	}
	if _, err := git(avant, "checkout-index", "--all", "--force"); err != nil {
		return err
	}
	c, err := cage(src)
	if err != nil {
		return err
	}
	ctx, fin := context.WithTimeout(context.Background(), delaiConstruction)
	defer fin()
	c.Stdout, c.Stderr, c.WaitDelay = journal, journal, 10*time.Second
	cacherFenetre(c)
	if err := c.Start(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Process.Kill() })
	err = c.Wait()
	stop()
	if err != nil {
		return errMoteur("the build failed ({0}); nothing was switched", err)
	}
	// created after the build: nothing the build left in the temporary folders is read
	apres, err := os.MkdirTemp("", "athanor-construire-git-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(apres)
	if err := os.Mkdir(filepath.Join(apres, "objects"), 0o700); err != nil {
		return err
	}
	if _, err := git(apres, "read-tree", commit); err != nil {
		return err
	}
	if _, err := git(apres, "add", "-A"); err != nil {
		return err
	}
	if dossier != "" {
		if _, err := git(apres, "add", "-f", "--", dossier); err != nil {
			return errMoteur("the published folder {0} is missing after the build: {1}", dossier, err)
		}
	}
	arbre, err := git(apres, "write-tree")
	if err != nil {
		return err
	}
	_, err = git(apres, "archive", "--format=tar.gz", "-o", archive, arbre)
	return err
}

// nomsBuild: files whose change changes what a build command runs (C68): the package managers' manifests,
// lockfiles and settings, and the makefiles, wherever they are; fichiersBuildChanges adds the files the
// command names.
var nomsBuild = map[string]bool{"package.json": true, "package-lock.json": true, "npm-shrinkwrap.json": true, "yarn.lock": true,
	".yarnrc": true, ".yarnrc.yml": true, "pnpm-lock.yaml": true, "pnpm-workspace.yaml": true, "bun.lock": true, "bun.lockb": true,
	".npmrc": true, "Makefile": true, "makefile": true, "GNUmakefile": true}

var reMotCommande = regexp.MustCompile("[^\\s;&|()<>'\"`]+")

// reFichierConteneur (C69): what docker compose reads to start the containers: its files and overrides, and the
// Dockerfiles.
var reFichierConteneur = regexp.MustCompile(`(?i)^((docker-)?compose([.-][^/]*)?\.ya?ml|dockerfile([.-][^/]*)?|[^/]*\.dockerfile)$`)

// fichiersBuildChanges (C68, C69): the build files of commit changed since the environment's last deployment the
// owner accepted (every one of them when there is none), and the files that say how the containers or pods run:
// the compose files and Dockerfiles (compose), the manifests' folder (k3s); nil for a static site without a
// build command. A non-empty answer means the owner's accord for this commit (destructive): never the auto
// policy, a rule nor the relay's standing accord.
func fichiersBuildChanges(p Projet, env, commit string, cfg *ConfigDeploiement) []string {
	if cfg == nil || cfg.Methode == "statique" && strings.TrimSpace(cfg.Construire) == "" {
		return nil
	}
	nommes := map[string]bool{}
	for _, m := range reMotCommande.FindAllString(cfg.Construire, -1) {
		nommes[path.Clean(strings.TrimPrefix(m, "./"))] = true
	}
	sensible := func(c string) bool {
		switch {
		case nommes[c] || cfg.Construire != "" && nomsBuild[path.Base(c)]:
			return true
		case cfg.Methode == "compose":
			return reFichierConteneur.MatchString(path.Base(c))
		case cfg.Methode == "k3s":
			return cfg.Dossier == "" || strings.HasPrefix(c, cfg.Dossier+"/")
		}
		return false
	}
	base, l := "", historiqueDeploiements(p.Nom, env)
	for i := len(l) - 1; i >= 0 && base == ""; i-- {
		if l[i].Accorde && !l[i].Retour {
			base = l[i].Commit
		}
	}
	sortie, err := "", errors.New("no deployment accepted by the owner yet")
	if base != "" {
		sortie, err = gitSortie(p.Dossier, "diff", "--no-ext-diff", "--no-renames", "--name-only", base, commit, "--")
	}
	if err != nil {
		sortie, _ = gitSortie(p.Dossier, "ls-tree", "-r", "--name-only", commit)
	}
	var f []string
	for _, c := range strings.Split(sortie, "\n") {
		if c != "" && sensible(c) {
			f = append(f, c)
		}
	}
	return f
}
