package main

// The compose deployment method (M134, ARCHITECTURE.md §Déploiements): the release extracted like a static one,
// its .env (0600 root) written from the code entries granted to the environment, passed on standard input and
// never in an argument nor in an answer, then `docker compose -p <projet>-<env> up -d --build` in the release
// before the switch; going back = the same command in the previous release. Caddy is not touched: the
// project's compose file publishes its own ports.

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
)

// fichiersCompose: the files docker compose reads by itself in its folder.
var fichiersCompose = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

var reExpose = regexp.MustCompile(`(?im)^[ \t]*EXPOSE[ \t]+(.+)$`)

// nomCompose: the Compose project of an environment (lower case, digits, - and _ only, like nomProjetServeur).
func nomCompose(projet, env string) string {
	return nomProjetServeur(projet) + "-" + env
}

// fichiersDossierCommit: the names in folder dossier ("" = the root) of commit.
func fichiersDossierCommit(depot, commit, dossier string) ([]string, error) {
	o, err := gitSortie(depot, "ls-tree", "--name-only", commit+":"+dossier)
	if err != nil {
		return nil, fmt.Errorf("the commit has no folder %q", dossier)
	}
	return strings.Split(o, "\n"), nil
}

// detecterMethode: compose when the commit's folder has a compose file or a Dockerfile, else statique.
func detecterMethode(depot, commit, dossier string) (string, error) {
	noms, err := fichiersDossierCommit(depot, commit, dossier)
	if err != nil {
		return "", err
	}
	for _, n := range append(slices.Clone(fichiersCompose), "Dockerfile") {
		if slices.Contains(noms, n) {
			return "compose", nil
		}
	}
	return "statique", nil
}

// composeGenere: "" when the folder has its own compose file; with only a Dockerfile, the base64 of a
// compose.yaml building it, its .env given to the container and its EXPOSE ports published.
func composeGenere(depot, commit, dossier string) (string, error) {
	noms, err := fichiersDossierCommit(depot, commit, dossier)
	if err != nil {
		return "", err
	}
	if slices.ContainsFunc(noms, func(n string) bool { return slices.Contains(fichiersCompose, n) }) {
		return "", nil
	}
	if !slices.Contains(noms, "Dockerfile") {
		return "", fmt.Errorf("the Compose method needs a compose.yaml or a Dockerfile in %q", "/"+dossier)
	}
	df, err := gitSortie(depot, "show", commit+":"+path.Join(dossier, "Dockerfile"))
	if err != nil {
		return "", err
	}
	y := "# Written by Athanor: the project has a Dockerfile and no compose file.\nservices:\n  app:\n    build: .\n    env_file: .env\n    restart: unless-stopped\n"
	var ports []string
	for _, m := range reExpose.FindAllStringSubmatch(df, -1) {
		for _, p := range strings.Fields(m[1]) {
			n, proto, _ := strings.Cut(p, "/")
			if strings.Trim(n, "0123456789") == "" && len(n) <= 5 && (proto == "" || proto == "tcp" || proto == "udp") && !slices.Contains(ports, p) {
				ports = append(ports, p)
			}
		}
	}
	if len(ports) > 0 {
		y += "    ports:\n"
		for _, p := range ports {
			n, proto, _ := strings.Cut(p, "/")
			if proto != "" {
				proto = "/" + proto
			}
			y += "      - \"" + n + ":" + n + proto + "\"\n"
		}
	}
	return base64.StdEncoding.EncodeToString([]byte(y)), nil
}

// fichierEnvCompose: the .env of the environment, NAME='value' for each code entry granted to it (Compose
// reads a single-quoted value as is). A value with a quote or a line break is refused, never cut.
func (s *Serveur) fichierEnvCompose(projet, env string) (string, error) {
	if s.acces == nil || s.coffre == nil {
		return "", errors.New("the vault is not available")
	}
	entrees := map[string]EntreeCoffre{}
	for _, e := range s.coffre.Liste() {
		entrees[e.ID] = e
	}
	lignes := map[string]string{}
	for _, a := range s.acces.Projet(projet).Acces {
		e, ok := entrees[a.Entree]
		if !ok || a.Env != env || e.Portee != "code" {
			continue
		}
		v, err := s.coffre.Valeur(e.ID)
		if err != nil {
			return "", fmt.Errorf("%s cannot be read from the vault (open its code key): %w", a.Variable, err)
		}
		if strings.ContainsAny(v, "'\r\n\x00") {
			return "", fmt.Errorf("the value of %s has a quote or a line break, which a Compose .env cannot hold", a.Variable)
		}
		lignes[a.Variable] = a.Variable + "='" + v + "'\n"
	}
	var b strings.Builder
	for _, n := range slices.Sorted(maps.Keys(lignes)) {
		b.WriteString(lignes[n])
	}
	return b.String(), nil
}

// fonctionRelancer ($b the environment's folder, $dd the compose folder, $p the Compose project): the
// version served (current) started again; nothing to do when nothing is served yet.
const fonctionRelancer = `relancer() { c=$(readlink -f -- "$b/current") && [ -d "$c" ] || return 0; ro=$(cd -- "$c${dd:+/$dd}" && docker compose -p "$p" up -d --build 2>&1) || { echo "docker compose up failed:"; printf '%s\n' "$ro" | tail -n 8; return 4; }; }
`

// scriptPreparerCompose ($1 the archive, $2 its SHA-256 or "", $3 the environment's folder, $4 the version, $5
// the compose folder, $6 the Compose project, $7 a generated compose.yaml in base64 or ""; the .env on standard
// input): the release started; on failure it is removed and the version served before started again.
const scriptPreparerCompose = `a=$1 b=$3 r=$3/releases/$4 dd=$5 p=$6
docker compose version >/dev/null 2>&1 || { rm -f -- "$a"; echo "Docker Compose is not installed on this server: install it first"; exit 5; }
` + extraireVersion + fonctionRelancer + `cd -- "$r${dd:+/$dd}" || { rm -rf -- "$r"; echo "the release has no folder $dd"; exit 1; }
if [ -n "$7" ]; then printf '%s' "$7" | base64 -d > compose.yaml || { rm -rf -- "$r"; exit 1; }; fi
rm -f .env && (umask 077; cat > .env) || { rm -rf -- "$r"; echo "the .env cannot be written"; exit 1; }
o=$(docker compose -p "$p" up -d --build 2>&1) || { cd /; relancer >/dev/null || echo "(the version served before could not be started again)"; rm -rf -- "$r"; echo "docker compose up failed:"; printf '%s\n' "$o" | tail -n 8; exit 4; }
echo ready`

// scriptAnnulerCompose ($1 folder, $2 version, $3 compose folder, $4 Compose project): a release started on this
// server while another server failed: the version served before started again, the release removed.
const scriptAnnulerCompose = `b=$1 dd=$3 p=$4
` + fonctionRelancer + `relancer; rm -rf -- "$1/releases/$2"`

// scriptRelancerCompose ($1 folder, $2 compose folder, $3 Compose project): after going back, current started.
const scriptRelancerCompose = `b=$1 dd=$2 p=$3
` + fonctionRelancer + `[ -d "$b/current" ] || exit 1
relancer`
