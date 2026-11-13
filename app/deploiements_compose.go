package main

// The compose deployment method (M134, ARCHITECTURE.md §Déploiements): the release extracted like a static one,
// its .env (0600 root) written from the code entries granted to the environment, passed on standard input and
// never in an argument nor in an answer, then `docker compose -p <projet>-<env> up -d --build` in the release
// before the switch (C69: its configuration checked by refusCompose first when the owner did not accept this
// deployment); going back = the same command in the previous release. C70: Caddy stands in front, its site
// block proxying to the first port published on 127.0.0.1 (none: no site file), the protection included; a
// port published beyond 127.0.0.1 needs the owner's accord, since Docker's rules bypass ufw.

import (
	"encoding/base64"
	"encoding/json"
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
		return nil, errMoteur("the commit has no folder \"{0}\"", dossier)
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
		return "", errMoteur("the Compose method needs a compose.yaml or a Dockerfile in \"{0}\"", "/"+dossier)
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
			y += "      - \"127.0.0.1:" + n + ":" + n + proto + "\"\n"
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
			return "", errMoteur("{0} cannot be read from the vault (open its code key): {1}", a.Variable, err)
		}
		if strings.ContainsAny(v, "'\r\n\x00") {
			return "", errMoteur("the value of {0} has a quote or a line break, which a Compose .env cannot hold", a.Variable)
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
// the compose folder, $6 the Compose project, $7 a generated compose.yaml in base64 or "", $8 the host name; the
// .env on standard input): the release extracted with its .env, the site file kept as it is (sauverSite), its
// configuration printed (docker compose config --format json; C69: it may hold the .env's values, so
// refusCompose reads it and nothing quotes it); nothing started.
const scriptPreparerCompose = `a=$1 b=$3 r=$3/releases/$4 dd=$5 p=$6 site=/etc/caddy/sites/$8.caddy
docker compose version >/dev/null 2>&1 || { rm -f -- "$a"; echo "Docker Compose is not installed on this server: install it first"; exit 5; }
` + extraireVersion + fonctionSite + `sauverSite || { rm -rf -- "$r"; exit 1; }
cd -- "$r${dd:+/$dd}" || { rm -rf -- "$r"; echo "the release has no folder $dd"; exit 1; }
if [ -n "$7" ]; then printf '%s' "$7" | base64 -d > compose.yaml || { rm -rf -- "$r"; exit 1; }; fi
rm -f .env && (umask 077; cat > .env) || { rm -rf -- "$r"; echo "the .env cannot be written"; exit 1; }
o=$(docker compose -p "$p" config --format json 2>/dev/null) || { rm -rf -- "$r"; echo "docker compose config failed: check the compose file with docker compose config"; exit 4; }
printf '%s\n' "$o"`

// scriptLancerCompose ($1 the environment's folder, $2 the version, $3 the compose folder, $4 the Compose
// project, $5 the host name; the site block on standard input, empty = no site), after scriptPreparerCompose:
// the site file written and validated (Caddy reloads after the switch), then the release started; on failure
// the site file is put back, the release removed and the version served before started again.
const scriptLancerCompose = `b=$1 r=$1/releases/$2 dd=$3 p=$4 site=/etc/caddy/sites/$5.caddy
` + fonctionRelancer + fonctionSite + `ecrireSite || { e=$?; remettreSite; rm -rf -- "$r"; exit $e; }
cd -- "$r${dd:+/$dd}" || { remettreSite; exit 1; }
o=$(docker compose -p "$p" up -d --build 2>&1) || { cd /; remettreSite; relancer >/dev/null || echo "(the version served before could not be started again)"; rm -rf -- "$r"; echo "docker compose up failed:"; printf '%s\n' "$o" | tail -n 8; exit 4; }
echo ready`

// refusCompose (C69): what the project's Compose configuration (docker compose config --format json, every
// file and variable resolved) asks that would give root on the server: privileged, added capabilities,
// devices, security options, the host's or another container's namespaces, a host path outside the release.
// Refused unless the owner accepted this very deployment; an unreadable configuration is refused too.
func refusCompose(sortie, release string) []string {
	type fichier struct{ File string }
	var c struct {
		Services map[string]struct {
			Privileged  bool     `json:"privileged"`
			CapAdd      []string `json:"cap_add"`
			Devices     []any    `json:"devices"`
			SecurityOpt []string `json:"security_opt"`
			NetworkMode string   `json:"network_mode"`
			Pid, Ipc    string
			UsernsMode  string `json:"userns_mode"`
			Cgroup      string
			Volumes     []struct{ Type, Source string }
			Ports       []portCompose
		}
		Volumes map[string]struct {
			DriverOpts map[string]string `json:"driver_opts"`
		}
		Secrets, Configs map[string]fichier
	}
	i := strings.IndexByte(sortie, '{')
	if i < 0 || json.Unmarshal([]byte(sortie[i:]), &c) != nil {
		return []string{"its configuration (docker compose config) cannot be read"}
	}
	partage := func(v string) bool { return v == "host" || strings.HasPrefix(v, "container:") }
	dehors := func(src string) bool {
		src = path.Clean(src)
		return src != release && !strings.HasPrefix(src, release+"/")
	}
	var r []string
	for n, v := range c.Services {
		q := "service " + n + ": "
		if v.Privileged {
			r = append(r, q+"privileged")
		}
		if len(v.CapAdd) > 0 {
			r = append(r, q+"cap_add "+strings.Join(v.CapAdd, " "))
		}
		if len(v.Devices) > 0 {
			r = append(r, q+"devices")
		}
		if len(v.SecurityOpt) > 0 {
			r = append(r, q+"security_opt "+strings.Join(v.SecurityOpt, " "))
		}
		for k, m := range map[string]string{"network_mode": v.NetworkMode, "pid": v.Pid, "ipc": v.Ipc, "userns_mode": v.UsernsMode, "cgroup": v.Cgroup} {
			if partage(m) {
				r = append(r, q+k+" "+m)
			}
		}
		for _, m := range v.Volumes {
			if m.Type == "bind" && dehors(m.Source) {
				r = append(r, q+"mounts the host's "+m.Source)
			}
		}
		for _, p := range v.Ports { // C70: Docker publishes past ufw; Caddy reaches 127.0.0.1
			if p.HostIP != "127.0.0.1" && p.HostIP != "::1" {
				r = append(r, q+"port "+fmt.Sprint(p.Target)+" published beyond 127.0.0.1")
			}
		}
	}
	for n, v := range c.Volumes {
		if v.DriverOpts["device"] != "" {
			r = append(r, "volume "+n+": a host device or path")
		}
	}
	for genre, l := range map[string]map[string]fichier{"secret": c.Secrets, "config": c.Configs} {
		for n, v := range l {
			if v.File != "" && dehors(v.File) {
				r = append(r, genre+" "+n+": the host's "+v.File)
			}
		}
	}
	slices.Sort(r)
	return r
}

// portCompose: a published port as docker compose config --format json writes it (published: a string or a number).
type portCompose struct {
	HostIP    string `json:"host_ip"`
	Target    any    `json:"target"`
	Published any    `json:"published"`
	Protocol  string `json:"protocol"`
}

// amontCompose (C70): the upstream Caddy proxies to, 127.0.0.1:n for the first TCP port published (services in
// name order), "" when none is (no site: a worker). shortcut: the first port only; a project serving several
// sites would need a port chosen per environment.
func amontCompose(sortie string) string {
	var c struct {
		Services map[string]struct{ Ports []portCompose }
	}
	i := strings.IndexByte(sortie, '{')
	if i < 0 || json.Unmarshal([]byte(sortie[i:]), &c) != nil {
		return ""
	}
	for _, n := range slices.Sorted(maps.Keys(c.Services)) {
		for _, p := range c.Services[n].Ports {
			pub := fmt.Sprint(p.Published)
			if (p.Protocol == "" || p.Protocol == "tcp") && pub != "" && len(pub) <= 5 && strings.Trim(pub, "0123456789") == "" {
				return "127.0.0.1:" + pub
			}
		}
	}
	return ""
}

// fonctionSite ($b the environment's folder, $site the site file): sauverSite keeps the site file as it is
// (remettreSite puts it back); ecrireSite writes the block read on standard input (empty: the site file
// removed, C74), then Caddy's configuration is validated.
const fonctionSite = `sauverSite() { rm -f -- "$b/.site-avant"; if [ -f "$site" ]; then cp -p -- "$site" "$b/.site-avant"; fi; }
remettreSite() { if [ -f "$b/.site-avant" ]; then mv -f -- "$b/.site-avant" "$site"; else rm -f -- "$site"; fi; }
ecrireSite() { mkdir -p -- "$b" && t=$(mktemp "$b/.site.XXXXXX") && cat > "$t" || { rm -f -- "$t"; echo "the site file cannot be written"; return 1; }
  if [ ! -s "$t" ]; then rm -f -- "$t" "$site"; return 0; fi
  command -v caddy >/dev/null 2>&1 && [ -f ` + caddyfileServeur + ` ] || { rm -f -- "$t"; echo "Caddy is not installed on this server (` + caddyfileServeur + ` missing): install it first"; return 5; }
  mkdir -p /etc/caddy/sites && chmod 644 "$t" && mv -f -- "$t" "$site" || { rm -f -- "$t"; echo "the site file cannot be written"; return 1; }
  grep -qxF 'import /etc/caddy/sites/*' ` + caddyfileServeur + ` || printf '\nimport /etc/caddy/sites/*\n' >> ` + caddyfileServeur + ` || return 1
  o=$(caddy validate --config ` + caddyfileServeur + ` --adapter caddyfile 2>&1) || { echo "caddy validate refused the configuration:"; printf '%s\n' "$o" | tail -n 4; return 4; }; }
`

// scriptSite ($1 folder, $2 host; the block on standard input, empty = no site): the site file replaced when
// going back (each version with its own method's site), put back as it was when Caddy refuses it.
const scriptSite = `b=$1 site=/etc/caddy/sites/$2.caddy
` + fonctionSite + `sauverSite || exit 1
ecrireSite || { e=$?; remettreSite; exit $e; }
rm -f -- "$b/.site-avant"`

// scriptAnnulerCompose ($1 folder, $2 version, $3 compose folder, $4 Compose project, $5 host): a release
// started on this server while another server failed: the version served before started again, its site file
// put back, the release removed.
const scriptAnnulerCompose = `b=$1 dd=$3 p=$4 site=/etc/caddy/sites/$5.caddy
` + fonctionRelancer + fonctionSite + `relancer; remettreSite; rm -rf -- "$1/releases/$2"`

// scriptRelancerCompose ($1 folder, $2 compose folder, $3 Compose project): after going back, current started.
const scriptRelancerCompose = `b=$1 dd=$2 p=$3
` + fonctionRelancer + `[ -d "$b/current" ] || exit 1
relancer`
