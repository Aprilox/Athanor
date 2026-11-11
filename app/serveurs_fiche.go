package main

// A server's sheet (the PC_TOOLS.md of a server): one read-only script sent on the standard input of
// "sh -s" (whatever the login shell), its output parsed tolerantly (busybox, no ss, no sudo: what is
// missing is said, never an error), kept as modules/servers/<id>/fiche.json and FICHE.md. Made again after
// the addition, on Refresh (POST /api/serveurs/{id} {rafraichir:true}) and every 6 hours while the vault
// can open the server's key; each try updates the server's state (ok / unreachable).

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

type FicheServeur struct {
	Date       time.Time        `json:"date"`
	Systeme    SystemeFiche     `json:"systeme"`
	Ressources RessourcesFiche  `json:"ressources"`
	Sudo       bool             `json:"sudo"` // sudo without a password
	Ports      []PortFiche      `json:"ports"`
	Logiciels  []LogicielFiche  `json:"logiciels"`
	PareFeu    string           `json:"pare_feu"` // actif | inactif | inconnu | absent (ufw)
	Services   []string         `json:"services"`
	Conteneurs []ConteneurFiche `json:"conteneurs"`
	Remarques  []string         `json:"remarques"` // what could not be read
}

type SystemeFiche struct {
	Nom          string `json:"nom"` // PRETTY_NAME
	ID           string `json:"id"`  // debian, alpine…
	Version      string `json:"version"`
	Noyau        string `json:"noyau"`
	Architecture string `json:"architecture"`
	NomHote      string `json:"nom_hote"`
	Allume       int64  `json:"allume_s"` // uptime in seconds
}

type RessourcesFiche struct {
	Processeurs    int `json:"processeurs"`
	MemoireMo      int `json:"memoire_mo"`
	MemoireLibreMo int `json:"memoire_libre_mo"`
	DisqueMo       int `json:"disque_mo"` // the / file system
	DisqueLibreMo  int `json:"disque_libre_mo"`
}

type PortFiche struct {
	Proto     string `json:"proto"` // tcp | udp
	Adresse   string `json:"adresse"`
	Port      int    `json:"port"`
	Processus string `json:"processus,omitempty"`
}

type LogicielFiche struct {
	Nom     string `json:"nom"`
	Present bool   `json:"present"`
	Version string `json:"version,omitempty"`
}

type ConteneurFiche struct {
	Nom   string `json:"nom"`
	Image string `json:"image"`
	Etat  string `json:"etat"`
}

// commandeFiche: the script comes on the standard input; its argument tells the test servers what it is.
const commandeFiche = "sh -s athanor-fiche"

// scriptFiche reads only. Each section starts with an @@name line; a missing tool leaves its section empty.
const scriptFiche = `export LC_ALL=C PATH="$PATH:/usr/sbin:/sbin:/usr/local/sbin"
T=""; command -v timeout >/dev/null 2>&1 && T="timeout 10"
S=""; sudo -n true >/dev/null 2>&1 && S="sudo -n"
echo "@@sudo"; [ -n "$S" ] && echo oui
echo "@@os"; cat /etc/os-release 2>/dev/null
echo "@@uname"; uname -n; uname -r; uname -m
echo "@@cpu"; nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo
echo "@@memoire"; grep -E "^(MemTotal|MemAvailable|MemFree):" /proc/meminfo 2>/dev/null
echo "@@disque"; df -Pk / 2>/dev/null | tail -n 1
echo "@@allume"; cat /proc/uptime 2>/dev/null
if command -v ss >/dev/null 2>&1; then echo "@@ports-ss"; $S ss -tulnp 2>/dev/null || ss -tuln 2>/dev/null
elif command -v netstat >/dev/null 2>&1; then echo "@@ports-netstat"; $S netstat -tulnp 2>/dev/null || netstat -tuln 2>/dev/null
else echo "@@ports-proc"; for f in tcp tcp6 udp udp6; do [ -r /proc/net/$f ] && sed "s/^/$f /" /proc/net/$f; done; fi
echo "@@logiciels"
for t in docker k3s kubectl nginx caddy ufw; do
  p=$(command -v $t 2>/dev/null) || { echo "$t|"; continue; }
  case $t in
    kubectl) v=$($T kubectl version --client 2>/dev/null);;
    nginx) v=$($T nginx -v 2>&1);;
    caddy) v=$($T caddy version 2>/dev/null);;
    *) v=$($T $t --version 2>/dev/null);;
  esac
  echo "$t|$p|$(echo "$v" | head -n 1)"
done
if command -v docker >/dev/null 2>&1 && v=$($T docker compose version --short 2>/dev/null) && [ -n "$v" ]; then echo "compose|docker compose|$v"
elif p=$(command -v docker-compose 2>/dev/null); then echo "compose|$p|$($T docker-compose version --short 2>/dev/null | head -n 1)"
else echo "compose|"; fi
echo "@@pare-feu"; command -v ufw >/dev/null 2>&1 && { $S ufw status 2>/dev/null | head -n 1; }
if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then echo "@@services-systemd"; systemctl list-units --type=service --state=running --no-legend --plain --no-pager 2>/dev/null
elif command -v rc-status >/dev/null 2>&1; then echo "@@services-openrc"; rc-status -a 2>/dev/null
else echo "@@processus"; ps -eo comm= 2>/dev/null || ps -o comm 2>/dev/null; fi
echo "@@conteneurs"
if command -v docker >/dev/null 2>&1; then F="{{.Names}}|{{.Image}}|{{.Status}}"
  $T docker ps --format "$F" 2>/dev/null || { [ -n "$S" ] && $T $S docker ps --format "$F" 2>/dev/null; } || echo "!refuse"; fi
echo "@@fin"
`

var (
	delaiFiche        = 45 * time.Second
	errFicheIllisible = errors.New("The server's sheet could not be read")
	reVersion         = regexp.MustCompile(`\d+\.\d+(\.\d+)*`)
	logicielsFiche    = []string{"docker", "compose", "k3s", "kubectl", "nginx", "caddy", "ufw"}
	// the script's own commands and kernel helpers, left out of the running programs
	processusScript = []string{"sh", "bash", "ps", "sudo", "timeout", "head", "sed", "grep", "COMMAND", "kthreadd"}
)

// sectionsFiche: the script's output cut at its @@name lines (the lines before the first one are dropped).
func sectionsFiche(sortie string) map[string][]string {
	m := map[string][]string{}
	nom := ""
	for l := range strings.Lines(sortie) {
		l = strings.TrimRight(l, "\r\n")
		if n, ok := strings.CutPrefix(l, "@@"); ok {
			nom = strings.TrimSpace(n)
			if m[nom] == nil {
				m[nom] = []string{} // a section seen, even empty
			}
			continue
		}
		if nom != "" && strings.TrimSpace(l) != "" {
			m[nom] = append(m[nom], l)
		}
	}
	return m
}

func premier(l []string) string {
	if len(l) == 0 {
		return ""
	}
	return strings.TrimSpace(l[0])
}

// analyserFiche: never fails; what is missing stays empty and is told in Remarques.
func analyserFiche(sortie string) FicheServeur {
	sec := sectionsFiche(sortie)
	f := FicheServeur{PareFeu: "inconnu", Ports: []PortFiche{}, Logiciels: []LogicielFiche{}, Services: []string{}, Conteneurs: []ConteneurFiche{}, Remarques: []string{}}
	remarque := func(r string) { f.Remarques = append(f.Remarques, r) }
	if _, ok := sec["fin"]; !ok {
		remarque("The sheet is incomplete: the script stopped before its end")
	}
	f.Sudo = premier(sec["sudo"]) == "oui"
	if !f.Sudo {
		remarque("No sudo without a password: some ports may show no program and some containers may be missing")
	}

	// system
	for _, l := range sec["os"] {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "PRETTY_NAME":
			f.Systeme.Nom = v
		case "ID":
			f.Systeme.ID = v
		case "VERSION_ID":
			f.Systeme.Version = v
		}
	}
	if u := sec["uname"]; len(u) >= 3 {
		f.Systeme.NomHote, f.Systeme.Noyau, f.Systeme.Architecture = strings.TrimSpace(u[0]), strings.TrimSpace(u[1]), strings.TrimSpace(u[2])
	}
	if c := strings.Fields(premier(sec["allume"])); len(c) > 0 {
		if x, err := strconv.ParseFloat(c[0], 64); err == nil {
			f.Systeme.Allume = int64(x)
		}
	}
	if f.Systeme.Nom == "" && f.Systeme.Noyau == "" {
		remarque("The operating system could not be read")
	}

	// resources
	f.Ressources.Processeurs, _ = strconv.Atoi(premier(sec["cpu"]))
	libre := -1
	for _, l := range sec["memoire"] {
		c := strings.Fields(l)
		if len(c) < 2 {
			continue
		}
		ko, _ := strconv.Atoi(c[1])
		switch c[0] {
		case "MemTotal:":
			f.Ressources.MemoireMo = ko / 1024
		case "MemAvailable:":
			libre = ko / 1024
		case "MemFree:":
			if libre < 0 {
				f.Ressources.MemoireLibreMo = ko / 1024
			}
		}
	}
	if libre >= 0 {
		f.Ressources.MemoireLibreMo = libre
	}
	if c := strings.Fields(premier(sec["disque"])); len(c) >= 6 { // fs total used available capacity mount (from the end: spaces in fs)
		n := len(c)
		total, _ := strconv.Atoi(c[n-5])
		dispo, _ := strconv.Atoi(c[n-3])
		f.Ressources.DisqueMo, f.Ressources.DisqueLibreMo = total/1024, dispo/1024
	}

	// listening ports
	switch {
	case sec["ports-ss"] != nil || sec["ports-netstat"] != nil:
		for _, l := range append(sec["ports-ss"], sec["ports-netstat"]...) {
			if p, ok := portOutil(l); ok {
				f.Ports = append(f.Ports, p)
			}
		}
	case sec["ports-proc"] != nil:
		remarque("Neither ss nor netstat on the server: ports read from /proc/net, without their programs")
		for _, l := range sec["ports-proc"] {
			if p, ok := portProc(l); ok {
				f.Ports = append(f.Ports, p)
			}
		}
	default:
		remarque("The listening ports could not be read")
	}
	slices.SortFunc(f.Ports, func(a, b PortFiche) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Proto+a.Adresse, b.Proto+b.Adresse)
	})
	f.Ports = slices.CompactFunc(f.Ports, func(a, b PortFiche) bool { return a.Port == b.Port && a.Proto == b.Proto && a.Adresse == b.Adresse })

	// software
	vus := map[string]LogicielFiche{}
	for _, l := range sec["logiciels"] {
		c := strings.SplitN(l, "|", 3)
		if !slices.Contains(logicielsFiche, c[0]) {
			continue
		}
		lf := LogicielFiche{Nom: c[0], Present: len(c) > 1 && strings.TrimSpace(c[1]) != ""}
		if lf.Present && len(c) == 3 {
			if lf.Version = reVersion.FindString(c[2]); lf.Version == "" {
				lf.Version = tronquerRunes(strings.TrimSpace(c[2]), 60)
			}
		}
		vus[c[0]] = lf
	}
	for _, n := range logicielsFiche {
		if lf, ok := vus[n]; ok {
			f.Logiciels = append(f.Logiciels, lf)
		}
	}
	if len(f.Logiciels) == 0 {
		remarque("The installed software could not be read")
	}
	if i := slices.IndexFunc(f.Logiciels, func(l LogicielFiche) bool { return l.Nom == "ufw" }); i >= 0 && !f.Logiciels[i].Present {
		f.PareFeu = "absent"
	} else if s := strings.ToLower(premier(sec["pare-feu"])); strings.Contains(s, "inactive") {
		f.PareFeu = "inactif"
	} else if strings.Contains(s, "active") {
		f.PareFeu = "actif"
	}

	// services, or the running programs when there is no service manager
	switch {
	case sec["services-systemd"] != nil:
		for _, l := range sec["services-systemd"] {
			if c := strings.Fields(l); len(c) > 0 && strings.HasSuffix(c[0], ".service") {
				f.Services = append(f.Services, strings.TrimSuffix(c[0], ".service"))
			}
		}
	case sec["services-openrc"] != nil:
		for _, l := range sec["services-openrc"] {
			if c := strings.Fields(l); len(c) > 1 && strings.Contains(l, "started") {
				f.Services = append(f.Services, c[0])
			}
		}
	case sec["processus"] != nil:
		remarque("No service manager on the server: its running programs are listed instead")
		for _, l := range sec["processus"] {
			c := strings.Fields(l)
			if len(c) == 0 {
				continue
			}
			n := c[len(c)-1] // busybox ps prints PID USER TIME COMMAND
			if strings.HasPrefix(n, "[") || slices.Contains(processusScript, n) || slices.Contains(f.Services, n) || strings.HasPrefix(n, "kworker") {
				continue
			}
			f.Services = append(f.Services, n)
		}
	default:
		remarque("The running services could not be read")
	}
	slices.Sort(f.Services)
	f.Services = slices.Compact(f.Services)

	// containers
	for _, l := range sec["conteneurs"] {
		if l == "!refuse" {
			remarque("Docker refused to list the containers (the user is not in the docker group and has no sudo)")
			continue
		}
		if c := strings.SplitN(l, "|", 3); len(c) == 3 {
			f.Conteneurs = append(f.Conteneurs, ConteneurFiche{Nom: c[0], Image: c[1], Etat: c[2]})
		}
	}
	return f
}

// portOutil: a listening socket in a line of ss or netstat (tcp/udp first, the local address the first
// field ending in :port, the program in users:(("x",…)) or pid/x).
func portOutil(l string) (PortFiche, bool) {
	c := strings.Fields(l)
	if len(c) < 4 {
		return PortFiche{}, false
	}
	p := PortFiche{Proto: strings.ToLower(c[0])}
	switch {
	case strings.HasPrefix(p.Proto, "tcp"):
		p.Proto = "tcp"
	case strings.HasPrefix(p.Proto, "udp"):
		p.Proto = "udp"
	default:
		return PortFiche{}, false // a header line
	}
	trouve := false
	for _, x := range c[1:] {
		if i := strings.LastIndex(x, ":"); i >= 0 && !strings.HasPrefix(x, "users:") {
			if n, err := strconv.Atoi(x[i+1:]); err == nil && n > 0 && n < 65536 {
				p.Adresse, p.Port, trouve = strings.Trim(x[:i], "[]"), n, true
				if j := strings.Index(p.Adresse, "%"); j >= 0 {
					p.Adresse = p.Adresse[:j]
				}
				break
			}
		}
	}
	if !trouve {
		return PortFiche{}, false
	}
	if i := strings.Index(l, `(("`); i >= 0 {
		if j := strings.Index(l[i+3:], `"`); j > 0 {
			p.Processus = l[i+3 : i+3+j]
		}
	} else if d := c[len(c)-1]; strings.Contains(d, "/") {
		_, p.Processus, _ = strings.Cut(d, "/")
	}
	return p, true
}

// portProc: a listening socket in a /proc/net line prefixed by its file ("tcp6 0: 00000000…:0016 … 0A").
func portProc(l string) (PortFiche, bool) {
	c := strings.Fields(l)
	if len(c) < 5 || !strings.HasSuffix(c[1], ":") {
		return PortFiche{}, false
	}
	proto, etat := strings.TrimSuffix(c[0], "6"), c[4]
	if proto == "tcp" && etat != "0A" || proto == "udp" && etat != "07" || proto != "tcp" && proto != "udp" {
		return PortFiche{}, false
	}
	adr, portHex, ok := strings.Cut(c[2], ":")
	n, err := strconv.ParseUint(portHex, 16, 16)
	b, err2 := hex.DecodeString(adr)
	if !ok || err != nil || err2 != nil || n == 0 || (len(b) != 4 && len(b) != 16) {
		return PortFiche{}, false
	}
	for i := 0; i < len(b); i += 4 { // the kernel writes each 32-bit word in its own byte order
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	return PortFiche{Proto: proto, Adresse: net.IP(b).String(), Port: int(n)}, true
}

// markdownFiche: FICHE.md, what an agent reads before working on the server.
func markdownFiche(srv ServeurInventaire, f FicheServeur) string {
	var b strings.Builder
	vide := func(s string) string {
		if s == "" {
			return "unknown"
		}
		return s
	}
	fmt.Fprintf(&b, "# Server %s (written by Athanor on %s, do not edit)\n\n", srv.Nom, f.Date.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintf(&b, "- **Address**: `%s@%s`\n", srv.Utilisateur, adresseSSH(srv.Hote, srv.Port))
	if len(srv.Roles) > 0 {
		fmt.Fprintf(&b, "- **Roles**: %s\n", strings.Join(srv.Roles, ", "))
	}
	fmt.Fprintf(&b, "- **System**: %s (kernel %s, %s), host name %s, up %s\n", vide(f.Systeme.Nom), vide(f.Systeme.Noyau), vide(f.Systeme.Architecture),
		vide(f.Systeme.NomHote), (time.Duration(f.Systeme.Allume) * time.Second).Truncate(time.Minute))
	r := f.Ressources
	fmt.Fprintf(&b, "- **Resources**: %d CPU, memory %d MB (%d MB free), disk / %d MB (%d MB free)\n", r.Processeurs, r.MemoireMo, r.MemoireLibreMo, r.DisqueMo, r.DisqueLibreMo)
	fmt.Fprintf(&b, "- **sudo without a password**: %s\n", map[bool]string{true: "yes", false: "no"}[f.Sudo])
	fmt.Fprintf(&b, "- **Firewall (ufw)**: %s\n", map[string]string{"actif": "active", "inactif": "inactive", "absent": "not installed", "inconnu": "unknown"}[f.PareFeu])
	b.WriteString("\n## Software\n\n")
	for _, l := range f.Logiciels {
		switch {
		case !l.Present:
			fmt.Fprintf(&b, "- %s: absent\n", l.Nom)
		case l.Version != "":
			fmt.Fprintf(&b, "- %s: %s\n", l.Nom, l.Version)
		default:
			fmt.Fprintf(&b, "- %s: present\n", l.Nom)
		}
	}
	b.WriteString("\n## Listening ports\n\n")
	for _, p := range f.Ports {
		fmt.Fprintf(&b, "- %d/%s on %s", p.Port, p.Proto, p.Adresse)
		if p.Processus != "" {
			fmt.Fprintf(&b, " (%s)", p.Processus)
		}
		b.WriteString("\n")
	}
	if len(f.Ports) == 0 {
		b.WriteString("- none found\n")
	}
	b.WriteString("\n## Running services\n\n")
	if len(f.Services) == 0 {
		b.WriteString("- none found\n")
	} else {
		fmt.Fprintf(&b, "- %s\n", strings.Join(f.Services, ", "))
	}
	b.WriteString("\n## Containers\n\n")
	for _, c := range f.Conteneurs {
		fmt.Fprintf(&b, "- %s (%s): %s\n", c.Nom, c.Image, c.Etat)
	}
	if len(f.Conteneurs) == 0 {
		b.WriteString("- none\n")
	}
	if len(f.Remarques) > 0 {
		b.WriteString("\n## Not read\n\n")
		for _, x := range f.Remarques {
			fmt.Fprintf(&b, "- %s\n", x)
		}
	}
	return b.String()
}

// rafraichirFiche: connects with the server's key, runs the script, writes the sheet and the server's state
// (unreachable when the server cannot be opened; a locked vault or an unreadable output changes nothing).
func (s *Serveur) rafraichirFiche(ctx context.Context, srv ServeurInventaire) (FicheServeur, error) {
	f, err := s.lireFicheDistante(ctx, srv)
	injoignable := errors.Is(err, errServeurInjoignable) || errors.Is(err, errServeurCleRefusee) || errors.Is(err, errEmpreinteChangee)
	if err != nil && !injoignable {
		return f, err
	}
	errEcrire := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		i := slices.IndexFunc(l, func(x ServeurInventaire) bool { return x.ID == srv.ID })
		if i < 0 || !idServeurValide.MatchString(srv.ID) {
			return nil, errServeurInconnu // removed meanwhile: no sheet for it
		}
		if err != nil {
			l[i].Etat = "injoignable"
			return l, nil
		}
		dossier := filepath.Join(dossierModule("servers"), srv.ID)
		j, e := json.MarshalIndent(f, "", "  ")
		if e == nil {
			if e = os.MkdirAll(dossier, 0o700); e == nil {
				if e = ecrireAtomique(filepath.Join(dossier, "fiche.json"), j, 0o600); e == nil {
					e = ecrireAtomique(filepath.Join(dossier, "FICHE.md"), []byte(markdownFiche(l[i], f)), 0o600)
				}
			}
		}
		if e != nil {
			return nil, e
		}
		l[i].Etat, l[i].Vu = "ok", f.Date
		return l, nil
	})
	if err != nil {
		return f, err
	}
	return f, errEcrire
}

func (s *Serveur) lireFicheDistante(ctx context.Context, srv ServeurInventaire) (FicheServeur, error) {
	client, err := s.connecterServeur(ctx, srv)
	if err != nil {
		return FicheServeur{}, err
	}
	defer client.Close()
	r, err := executerSSH(ctx, client, commandeFiche, strings.NewReader(scriptFiche), delaiFiche)
	if err != nil || !strings.Contains(string(r.Sortie), "@@sudo") {
		return FicheServeur{}, errFicheIllisible
	}
	f := analyserFiche(string(r.Sortie))
	f.Date = time.Now().UTC().Truncate(time.Second)
	return f, nil
}

// apiServeurRafraichir: POST /api/serveurs/{id} {rafraichir:true}: the sheet made again, with the server.
func (s *Serveur) apiServeurRafraichir(w http.ResponseWriter, r *http.Request, id string) {
	if !s.coffrePret(w) {
		return
	}
	liste, err := lireServeurs()
	if err != nil {
		s.erreurServeurs(w, err)
		return
	}
	i := slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == id })
	if i < 0 {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	f, err := s.rafraichirFiche(r.Context(), liste[i])
	if err != nil {
		s.erreur(w, statutAjout(err), err.Error())
		return
	}
	liste, _ = lireServeurs()
	if i = slices.IndexFunc(liste, func(x ServeurInventaire) bool { return x.ID == id }); i < 0 {
		s.erreurServeurs(w, errServeurInconnu)
		return
	}
	s.ecrireJSON(w, map[string]any{"serveur": liste[i], "fiche": f})
}

// rafraichirFichesAnciennes: the sheets older than age (or missing) made again, for the servers whose key
// the vault can open now (an infra key only while the vault is unlocked); a generated key never verified
// is left alone.
func (s *Serveur) rafraichirFichesAnciennes(ctx context.Context, age time.Duration) {
	if s.coffre == nil {
		return
	}
	liste, _ := lireServeurs()
	for _, srv := range liste {
		if srv.Vu.IsZero() || !idServeurValide.MatchString(srv.ID) {
			continue
		}
		if st, err := os.Stat(filepath.Join(dossierModule("servers"), srv.ID, "fiche.json")); err == nil && time.Since(st.ModTime()) < age {
			continue
		}
		if _, err := s.coffre.Valeur(srv.Cle); err != nil {
			continue
		}
		_, _ = s.rafraichirFiche(ctx, srv)
	}
}

// fichesServeursFond: every 10 minutes, the sheets more than 6 hours old (main.go).
func (s *Serveur) fichesServeursFond() {
	for range time.Tick(10 * time.Minute) {
		s.rafraichirFichesAnciennes(context.Background(), 6*time.Hour)
	}
}
