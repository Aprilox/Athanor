package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
)

// Classe: risk class of a server action (ARCHITECTURE.md §Garde-fous, decision 5): the code decides, the agent only declares.
type Classe int

const (
	ClasseLecture Classe = iota
	ClasseChangement
	ClasseDestructrice
)

func (c Classe) String() string { return [...]string{"lecture", "changement", "destructrice"}[c] }

// verdict keeps the highest class seen and the first reason that reached it.
type verdict struct {
	classe Classe
	raison string
}

func (v *verdict) monter(c Classe, raison string) {
	if c > v.classe {
		v.classe, v.raison = c, raison
	}
}

// classerCommande classes a free shell command (ssh_commande). Anything it cannot follow is destructive.
func classerCommande(cmd string) (Classe, string) {
	if strings.TrimSpace(cmd) == "" {
		return ClasseDestructrice, "empty command: nothing to analyse"
	}
	v := classerTexte(cmd, 0)
	if v.raison == "" {
		v.raison = "only known read-only commands"
	}
	return v.classe, v.raison
}

func classerTexte(cmd string, prof int) verdict {
	for _, r := range cmd {
		if r != '\t' && r != '\n' && caractereInvisible(r) {
			return verdict{ClasseDestructrice, "control or invisible character: what is shown is not what runs"}
		}
	}
	if prof > 8 {
		return verdict{ClasseDestructrice, "substitutions nested too deep to analyse"}
	}
	l := lexeurShell{s: cmd, prof: prof}
	l.lire()
	if l.ko != "" {
		return verdict{ClasseDestructrice, "cannot be analysed (" + l.ko + "): treated as destructive"}
	}
	v := l.v
	for _, sg := range l.segs {
		c, r := classerSegment(sg, cmd)
		v.monter(c, r)
	}
	return v
}

// --- Lexer: a cautious subset of the POSIX shell grammar ---

type motShell struct {
	texte     string // quotes and escapes removed, $HOME as ~
	dynamique bool   // holds an expansion whose value is unknown here
}

type redirShell struct {
	op    string
	cible motShell
}

type segmentShell struct {
	mots   []motShell
	redirs []redirShell
}

type lexeurShell struct {
	s     string
	i     int
	prof  int
	v     verdict // classes of the substitutions met on the way
	ko    string
	segs  []segmentShell
	cour  segmentShell
	mot   strings.Builder
	dans  bool // a word is in progress
	cite  bool // the word holds a quote, an escape or an expansion
	dyn   bool
	redir string // redirection waiting for its target word
}

func (l *lexeurShell) echec(raison string) {
	if l.ko == "" {
		l.ko = raison
	}
}

func (l *lexeurShell) ajouter(s string) { l.mot.WriteString(s); l.dans = true }

func (l *lexeurShell) finirMot() {
	if !l.dans {
		return
	}
	m := motShell{l.mot.String(), l.dyn}
	l.mot.Reset()
	l.dans, l.cite, l.dyn = false, false, false
	if l.redir != "" {
		l.cour.redirs = append(l.cour.redirs, redirShell{l.redir, m})
		l.redir = ""
		return
	}
	l.cour.mots = append(l.cour.mots, m)
}

func (l *lexeurShell) finirSegment() {
	l.finirMot()
	if l.redir != "" {
		l.echec("redirection without a target")
	}
	if len(l.cour.mots)+len(l.cour.redirs) > 0 {
		l.segs = append(l.segs, l.cour)
	}
	l.cour = segmentShell{}
}

func (l *lexeurShell) suivant(k int) byte {
	if l.i+k < len(l.s) {
		return l.s[l.i+k]
	}
	return 0
}

func (l *lexeurShell) lire() {
	for l.i < len(l.s) && l.ko == "" {
		c := l.s[l.i]
		switch {
		case c == ' ' || c == '\t':
			l.finirMot()
			l.i++
		case c == '\n' || c == ';' || c == '(' || c == ')' || c == '|':
			l.finirSegment()
			l.i++
		case c == '&' && l.suivant(1) == '>':
			l.finirMot()
			l.lireRedir()
		case c == '&':
			l.finirSegment()
			l.i++
		case (c == '<' || c == '>') && l.suivant(1) == '(':
			l.substitution(l.i + 1)
		case c == '<' || c == '>':
			l.lireRedir()
		case c == '#' && !l.dans:
			for l.i < len(l.s) && l.s[l.i] != '\n' {
				l.i++
			}
		case c == '\\':
			if l.suivant(1) == 0 {
				l.echec("trailing backslash")
			} else if l.suivant(1) != '\n' {
				l.ajouter(l.s[l.i+1 : l.i+2])
				l.cite = true
			}
			l.i += 2
		case c == '\'':
			j := strings.IndexByte(l.s[l.i+1:], '\'')
			if j < 0 {
				l.echec("unclosed quote")
				return
			}
			l.ajouter(l.s[l.i+1 : l.i+1+j])
			l.cite = true
			l.i += j + 2
		case c == '"':
			l.guillemets()
		case c == '`':
			l.contreGuillemet()
		case c == '$':
			l.dollar(false)
		default:
			l.ajouter(l.s[l.i : l.i+1])
			l.i++
		}
	}
	if l.ko == "" {
		l.finirSegment()
	}
}

var opsRedir = []string{"&>>", "&>", "<<<", "<<-", "<<", "<>", ">>", ">|", ">&", "<&", ">", "<"}

func (l *lexeurShell) lireRedir() {
	if l.dans && !l.cite && !l.dyn && strings.Trim(l.mot.String(), "0123456789") == "" {
		l.mot.Reset() // file descriptor number (2>…)
		l.dans = false
	} else {
		l.finirMot()
	}
	if l.redir != "" {
		l.echec("redirection without a target")
		return
	}
	for _, op := range opsRedir {
		if !strings.HasPrefix(l.s[l.i:], op) {
			continue
		}
		l.i += len(op)
		if op == "<<" || op == "<<-" {
			l.echec("here-documents are not analysed, use fichier_ecrire")
			return
		}
		for l.suivant(0) == ' ' || l.suivant(0) == '\t' {
			l.i++
		}
		if op == ">&" || op == "<&" { // descriptor copy (2>&1, >&-): no file
			j := l.i
			for j < len(l.s) && (l.s[j] >= '0' && l.s[j] <= '9' || l.s[j] == '-') {
				j++
			}
			if j > l.i && (j == len(l.s) || strings.IndexByte(" \t\n;&|()<>", l.s[j]) >= 0) {
				l.i = j
				return
			}
		}
		l.redir = op
		return
	}
}

// fin: index of the byte closing the one at i (parentheses or braces), quotes skipped, -1 if none.
func fin(s string, i int, ouvre, ferme byte) int {
	prof := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case '\'':
			k := strings.IndexByte(s[j+1:], '\'')
			if k < 0 {
				return -1
			}
			j += k + 1
		case '"':
			for j++; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' {
					j++
				}
			}
		case ouvre:
			prof++
		case ferme:
			if prof--; prof == 0 {
				return j
			}
		}
	}
	return -1
}

// substitution: $(…), <(…) or >(…) whose '(' is at i: the inner command is classed, the word becomes unknown.
func (l *lexeurShell) substitution(i int) {
	j := fin(l.s, i, '(', ')')
	if j < 0 {
		l.echec("unclosed substitution")
		return
	}
	l.sousCommande(l.s[i+1 : j])
	l.i = j + 1
}

func (l *lexeurShell) sousCommande(inner string) {
	v := classerTexte(inner, l.prof+1)
	l.v.monter(v.classe, v.raison)
	l.dans, l.cite, l.dyn = true, true, true
}

func (l *lexeurShell) contreGuillemet() {
	j := l.i + 1
	for j < len(l.s) && l.s[j] != '`' {
		if l.s[j] == '\\' {
			j++
		}
		j++
	}
	if j >= len(l.s) {
		l.echec("unclosed backquote")
		return
	}
	l.sousCommande(strings.ReplaceAll(l.s[l.i+1:j], "\\`", "`"))
	l.i = j + 1
}

func (l *lexeurShell) guillemets() {
	l.dans, l.cite = true, true
	l.i++
	for l.i < len(l.s) && l.ko == "" {
		c := l.s[l.i]
		switch {
		case c == '"':
			l.i++
			return
		case c == '\\' && strings.IndexByte("$`\"\\\n", l.suivant(1)) >= 0 && l.suivant(1) != 0:
			if l.suivant(1) != '\n' {
				l.ajouter(l.s[l.i+1 : l.i+2])
			}
			l.i += 2
		case c == '`':
			l.contreGuillemet()
		case c == '$':
			l.dollar(true)
		default:
			l.ajouter(l.s[l.i : l.i+1])
			l.i++
		}
	}
	l.echec("unclosed double quote")
}

func lettreNom(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func (l *lexeurShell) dollar(dansGuillemets bool) {
	n := l.suivant(1)
	switch {
	case n == '\'' && !dansGuillemets:
		l.echec("ANSI-C quoting $'…' hides the text")
	case n == '"' && !dansGuillemets:
		l.i++ // $"…" is a plain double-quoted string here
	case n == '(' && l.suivant(2) == '(':
		j := fin(l.s, l.i+1, '(', ')')
		if j < 0 || strings.ContainsAny(l.s[l.i:j], "`") || strings.Contains(l.s[l.i+2:j], "$(") {
			l.echec("arithmetic expansion not analysed")
			return
		}
		l.dans, l.cite, l.dyn = true, true, true
		l.i = j + 1
	case n == '(':
		l.substitution(l.i + 1)
	case n == '{':
		j := fin(l.s, l.i+1, '{', '}')
		if j < 0 || strings.ContainsAny(l.s[l.i:j], "`") || strings.Contains(l.s[l.i+2:j], "$(") {
			l.echec("parameter expansion not analysed")
			return
		}
		l.variable(l.s[l.i+2 : j])
		l.i = j + 1
	case lettreNom(n):
		j := l.i + 1
		for j < len(l.s) && (lettreNom(l.s[j]) || l.s[j] >= '0' && l.s[j] <= '9') {
			j++
		}
		l.variable(l.s[l.i+1 : j])
		l.i = j
	case n != 0 && strings.IndexByte("0123456789@*#?$!-", n) >= 0:
		l.dans, l.cite, l.dyn = true, true, true
		l.i += 2
	default:
		l.ajouter("$")
		l.i++
	}
}

func (l *lexeurShell) variable(nom string) {
	if nom == "HOME" {
		l.ajouter("~")
	} else {
		l.dans, l.dyn = true, true
	}
	l.cite = true
}

// --- Segments ---

var motsClesShell = map[string]bool{"!": true, "{": true, "}": true, "if": true, "then": true, "else": true, "elif": true, "fi": true, "do": true, "done": true, "while": true, "until": true}
var constructionsShell = map[string]bool{"case": true, "esac": true, "select": true, "function": true, "coproc": true, "in": true}
var reAffectation = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\[[^]]*\])?\+?=`)

func classerSegment(sg segmentShell, texte string) (Classe, string) {
	var v verdict
	for _, r := range sg.redirs {
		t := r.cible.texte
		switch {
		case r.op == "<" || r.op == "<<<" || r.op == "<&":
			if c := lectureInterdite(t); c > ClasseLecture {
				v.monter(c, "reads a protected secret path: "+t)
			}
			if disque(t) {
				v.monter(ClasseChangement, "reads a raw disk or memory, where any file's secrets can be read: "+t)
			}
		case r.cible.dynamique:
			v.monter(ClasseChangement, "writes to a path computed at run time")
		case t == "/dev/null" || t == "/dev/stdout" || t == "/dev/stderr" || strings.HasPrefix(t, "/dev/fd/"):
		case cheminCritique(t) || disque(t) || lectureInterdite(t) == ClasseDestructrice:
			v.monter(ClasseDestructrice, "writes to a protected system path: "+t)
		default:
			v.monter(ClasseChangement, "writes to a file: "+t)
		}
	}
	mots := sg.mots
	masque, affecte, sudo := "", false, false
	for len(mots) > 0 {
		m := mots[0]
		nom := m.texte
		switch {
		case reAffectation.MatchString(nom):
			affecte, mots = true, mots[1:]
			continue
		case motsClesShell[nom]:
			mots = mots[1:]
			continue
		case nom == "for":
			if len(mots) < 2 || !reAffectation.MatchString(mots[1].texte+"=") || len(mots) > 2 && mots[2].texte != "in" {
				return ClasseDestructrice, "shell construct not analysed: for"
			}
			v.monter(classerArguments(mots[1:], ClasseLecture))
			return v.classe, v.raison
		case constructionsShell[nom] && !m.dynamique:
			return ClasseDestructrice, "shell construct not analysed: " + nom
		}
		nom = path.Base(nom) // /usr/bin/sudo is sudo
		if _, ok := enveloppes[nom]; !ok || m.dynamique {
			break
		}
		if nom == "sudo" || nom == "doas" {
			sudo = true
		}
		reste, recherche, raison := sauterEnveloppe(nom, mots[1:])
		if raison != "" {
			return ClasseDestructrice, nom + " " + raison
		}
		if recherche {
			mots, masque = nil, "command"
			break
		}
		masque, mots = nom, reste
	}
	if len(mots) == 0 {
		switch {
		case masque == "env" && sudo:
			v.monter(ClasseChangement, "env under sudo shows root's environment, which may hold secrets")
		case masque == "env" || masque == "command" || masque == "" && !affecte:
		case masque == "":
			v.monter(ClasseChangement, "sets a shell variable")
		case masque == "chroot":
			v.monter(ClasseDestructrice, "chroot without a command opens a shell")
		default:
			v.monter(ClasseChangement, masque+" without a command")
		}
		return v.classe, v.raison
	}
	if mots[0].dynamique {
		return ClasseDestructrice, "the command name is computed at run time: cannot be analysed"
	}
	nom := mots[0].texte
	if nom != "[" && nom != "[[" && strings.ContainsAny(nom, "{*?[") {
		return ClasseDestructrice, "the command name is computed at run time (braces or wildcard): cannot be analysed"
	}
	if strings.Contains(nom, "/") {
		nom = path.Base(nom)
	}
	args := mots[1:]
	c, raison := classerNom(nom, textes(args), texte)
	switch {
	case c == ClasseLecture && sudo && nom == "printenv":
		c, raison = ClasseChangement, "printenv under sudo shows root's environment, which may hold secrets"
	case c == ClasseLecture && affecte:
		c, raison = ClasseChangement, "environment variables imposed on "+nom
	case c > ClasseLecture && (masque != "" || affecte):
		pref := masque
		if pref == "" {
			pref = "a variable prefix"
		}
		c, raison = ClasseDestructrice, pref+" hides a command that is not read-only ("+raison+")"
	}
	var a verdict
	a.monter(c, raison)
	// bash evaluates array subscripts in these words (a[$(cmd)]) even when quoted: the command runs
	if evalueIndices[nom] && slices.ContainsFunc(args, func(m motShell) bool {
		return strings.Contains(m.texte, "$(") || strings.Contains(m.texte, "`") || strings.Contains(m.texte, "${")
	}) {
		a.monter(ClasseDestructrice, nom+" evaluates an expansion that can run a command")
	}
	a.monter(classerArguments(args, c))
	if c == ClasseLecture && !metadonneesDisque[nom] && slices.ContainsFunc(args, func(m motShell) bool { return slices.ContainsFunc(variantesMot(m.texte), disque) }) {
		a.monter(ClasseChangement, nom+" reads a raw disk or memory, where any file's secrets can be read")
	}
	if c == ClasseLecture && nom != "echo" && nom != "printf" {
		for _, m := range args {
			if m.dynamique {
				a.monter(ClasseChangement, nom+" with an argument computed at run time")
			}
		}
	}
	a.monter(v.classe, v.raison)
	return a.classe, a.raison
}

// classerArguments: secret paths for every command, protected system paths for every command that is not read-only.
func classerArguments(args []motShell, c Classe) (Classe, string) {
	var v verdict
	for _, m := range args {
		switch lectureInterdite(m.texte) {
		case ClasseDestructrice:
			v.monter(ClasseDestructrice, "touches a protected secret path: "+m.texte)
		case ClasseChangement:
			v.monter(ClasseChangement, "may reach a protected secret path: "+m.texte)
		}
		if c > ClasseLecture && slices.ContainsFunc(variantesMot(m.texte), func(p string) bool { return cheminCritique(p) || disque(p) }) {
			v.monter(ClasseDestructrice, "changes a protected system path: "+m.texte)
		}
	}
	return v.classe, v.raison
}

func textes(mots []motShell) []string {
	t := make([]string, len(mots))
	for i, m := range mots {
		t[i] = m.texte
	}
	return t
}

// --- Prefixes that run the command after them ---

type enveloppe struct {
	valeurs      string // short options followed by a value
	positionnels int    // operands before the command (timeout DURATION)
	shell        string // short options that open a shell or split a string
}

var enveloppes = map[string]enveloppe{
	"sudo": {"ughpCDrtUTR", 0, "sie"}, "doas": {"uC", 0, "s"}, "env": {"uCP", 0, "S"}, "timeout": {"sk", 1, ""},
	"nohup": {}, "nice": {"n", 0, ""}, "ionice": {"cnpPu", 0, ""}, "stdbuf": {"ioe", 0, ""}, "time": {"fo", 0, ""},
	"command": {}, "exec": {"a", 0, ""}, "builtin": {}, "setsid": {}, "chroot": {"", 1, ""}, "taskset": {"", 1, ""}, "chrt": {"", 1, ""},
}

var longsAvecValeur = map[string]bool{"--user": true, "--group": true, "--host": true, "--prompt": true, "--close-from": true, "--chdir": true, "--role": true, "--type": true, "--other-user": true, "--signal": true, "--kill-after": true, "--unset": true, "--adjustment": true, "--class": true, "--classdata": true, "--pid": true, "--input": true, "--output": true, "--error": true, "--format": true, "--userspec": true, "--groups": true, "--argv0": true}
var longsSansValeur = map[string]bool{"--preserve-env": true, "--preserve-status": true, "--foreground": true, "--verbose": true, "--ignore-environment": true, "--null": true, "--non-interactive": true, "--background": true, "--reset-timestamp": true, "--set-home": true, "--stdin": true, "--preserve-groups": true, "--wait": true, "--fork": true, "--ctty": true, "--version": true, "--help": true}
var longsShell = map[string]bool{"--shell": true, "--login": true, "--edit": true, "--split-string": true}

// sauterEnveloppe returns the inner command, or recherche for `command -v`, or the reason it cannot be followed.
func sauterEnveloppe(nom string, mots []motShell) (reste []motShell, recherche bool, raison string) {
	e := enveloppes[nom]
	apres := func(i int) []motShell {
		if i+e.positionnels >= len(mots) {
			return nil
		}
		return mots[i+e.positionnels:]
	}
	for i := 0; i < len(mots); i++ {
		t := mots[i].texte
		switch {
		case mots[i].dynamique:
			return nil, false, "has an option computed at run time"
		case t == "--":
			return apres(i + 1), false, ""
		case strings.HasPrefix(t, "--"):
			n, _, egal := strings.Cut(t, "=")
			switch {
			case longsShell[n]:
				return nil, false, n + " opens a shell or runs a string"
			case egal || longsSansValeur[n]:
			case longsAvecValeur[n]:
				i++
			default:
				return nil, false, "has an unknown option " + n
			}
		case strings.HasPrefix(t, "-") && !(nom == "env" && reAffectation.MatchString(t)):
			for k := 1; k < len(t); k++ {
				switch {
				case strings.IndexByte(e.shell, t[k]) >= 0:
					return nil, false, "-" + t[k:k+1] + " opens a shell or runs a string"
				case nom == "command" && (t[k] == 'v' || t[k] == 'V'):
					return nil, true, ""
				case strings.IndexByte(e.valeurs, t[k]) >= 0:
					if k == len(t)-1 {
						i++
					}
					k = len(t)
				}
			}
		default:
			return apres(i), false, ""
		}
	}
	return nil, false, ""
}

// --- Commands ---

var lecturesSimples = ensemble("cat tac head tail zcat bzcat xzcat wc cut nl od hexdump file stat du df ls pwd whoami id groups uname uptime free top pgrep pidof w who last lastlog lsblk lsof findmnt lscpu lsmem lspci lsusb lsmod blkid printenv echo printf true false test [ [[ which type whereis basename dirname realpath readlink md5sum sha1sum sha256sum sha512sum cksum diff cmp ss netstat ping dig nslookup host getent jq column tr nproc arch vmstat iostat sleep seq comm join paste fold rev read set cd pushd popd exit return : iptables-save ip6tables-save apt-cache locale tty")
var destructricesSimples = ensemble("rm unlink shred truncate dd wipefs fdisk sfdisk cfdisk parted gdisk sgdisk mkswap mke2fs blkdiscard shutdown reboot halt poweroff userdel deluser groupdel delgroup killall5 srm wipe dropdb dropuser iptables-restore ip6tables-restore")
var interpretes = ensemble("sh bash dash zsh ksh mksh fish csh tcsh busybox eval source . xargs parallel watch su runuser flock nsenter unshare systemd-run at batch screen tmux ssh sshpass script expect strace ltrace gdb trap shopt enable osascript deno bun julia Rscript java pkexec run0 sg setpriv ssh-agent fakeroot")
var metadonneesDisque = ensemble("ls stat lsblk blkid df du findmnt lsof realpath readlink basename dirname test [ [[ echo printf file") // read a disk's name, not its content
var evalueIndices = ensemble("[[ [ test printf read let declare typeset local export unset mapfile readarray")
var prefixesInterpretes = []string{"python", "pypy", "perl", "ruby", "php", "node", "lua", "tclsh", "pwsh", "mkfs"}

func ensemble(s string) map[string]bool {
	m := map[string]bool{}
	for _, x := range strings.Fields(s) {
		m[x] = true
	}
	return m
}

func classerNom(nom string, a []string, texte string) (Classe, string) {
	if r, ok := reglesCommandes[nom]; ok {
		return r(a, texte)
	}
	switch {
	case destructricesSimples[nom]:
		return ClasseDestructrice, nom + " deletes or overwrites data"
	case interpretes[nom] || slices.ContainsFunc(prefixesInterpretes, func(p string) bool { return strings.HasPrefix(nom, p) }):
		if strings.HasPrefix(nom, "mkfs") {
			return ClasseDestructrice, nom + " formats a disk"
		}
		if len(a) == 1 && (a[0] == "--version" || a[0] == "-V") {
			return ClasseLecture, ""
		}
		return ClasseDestructrice, nom + " runs code that cannot be analysed (interpreter or hidden command)"
	case lecturesSimples[nom]:
		return ClasseLecture, ""
	}
	return ClasseChangement, nom + " is not a known read-only command"
}

// optionShell: an option among the short letters (alone or grouped: -rf) or the long names (--x or --x=…), before "--".
func optionShell(a []string, courtes string, longues ...string) bool {
	for _, x := range a {
		if x == "--" {
			return false
		}
		n, _, _ := strings.Cut(x, "=")
		if strings.HasPrefix(x, "--") && slices.Contains(longues, n) {
			return true
		}
		if len(x) > 1 && x[0] == '-' && x[1] != '-' && strings.ContainsAny(x[1:], courtes) && courtes != "" {
			return true
		}
	}
	return false
}

// positionnels: the words that are not options (option values included: a cautious approximation).
func positionnels(a []string) []string {
	var p []string
	for i, x := range a {
		if x == "--" {
			return append(p, a[i+1:]...)
		}
		if !strings.HasPrefix(x, "-") || x == "-" {
			p = append(p, x)
		}
	}
	return p
}

func unParmi(p []string, mots string) bool {
	e := ensemble(mots)
	return slices.ContainsFunc(p, func(x string) bool { return e[x] })
}

type regle func(a []string, texte string) (Classe, string)

var (
	reSedLecture  = regexp.MustCompile(`^\s*(((\d+|\$|/[^/]*/)(\s*,\s*(\d+|\$|/[^/]*/))?\s*[pd=])|s/([^/\\]|\\.)*/([^/\\]|\\.)*/[gpiI0-9]*)\s*$`)
	reSQLDetruit  = regexp.MustCompile(`(?i)\b(drop\s+(table|database|schema|index|view|user|role|owned)|truncate|delete\s+from|flushall|flushdb|dropdatabase|deletemany)\b|\.(drop|remove)\s*\(`)
	unitesVitales = ensemble("ssh sshd networking network systemd-networkd NetworkManager")
)

func uniteVitale(p []string) bool {
	return slices.ContainsFunc(p, func(u string) bool {
		u = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(u, ".service"), ".socket"), ".target")
		return unitesVitales[u]
	})
}

// classerSous: destructive verbs anywhere among the operands (a misread option value then errs on the safe side),
// read-only verbs only as the first operand, anything else is a change.
func classerSous(nom string, p []string, lecture, destructeurs string) (Classe, string) {
	if unParmi(p, destructeurs) {
		return ClasseDestructrice, nom + " " + strings.Join(p, " ") + " deletes data or runs a hidden command"
	}
	if premier(p) == "" || ensemble(lecture)[p[0]] {
		return ClasseLecture, ""
	}
	return ClasseChangement, nom + " " + p[0] + " changes the server"
}

func regleDocker(nom string) regle {
	return func(a []string, _ string) (Classe, string) {
		p := positionnels(a)
		if nom == "docker-compose" {
			p = append([]string{"compose"}, p...)
		}
		if len(p) > 1 && !unParmi(p, "rm rmi kill exec run prune down remove leave delete") &&
			(p[1] == "inspect" || p[0] == "inspect" || p[0] == "compose" && p[1] == "config") {
			return ClasseChangement, nom + " " + strings.Join(p[:2], " ") + " shows the configuration and environment, which may hold secrets"
		}
		switch premier(p) {
		case "compose":
			return classerSous(nom, p[1:], "ps logs ls images top version port events", "down rm kill exec run")
		case "volume", "network", "image", "container", "system", "builder", "buildx", "context", "plugin", "secret", "config", "node", "service", "stack", "swarm":
			return classerSous(nom, p[1:], "ls list df history", "rm remove prune kill exec run leave delete")
		}
		return classerSous(nom, p, "ps images logs version info stats top port history diff search events", "rm rmi kill exec run prune")
	}
}

func regleIptables(a []string, _ string) (Classe, string) {
	lit, change := false, false
	for _, x := range a {
		switch {
		case x == "--flush" || x == "--delete-chain" || x == "--policy":
			return ClasseDestructrice, "iptables " + x + " removes firewall rules"
		case x == "--list" || x == "--list-rules":
			lit = true
		case strings.HasPrefix(x, "--"):
			change = change || slices.Contains([]string{"--append", "--insert", "--delete", "--replace", "--new-chain", "--rename-chain"}, x)
		case len(x) > 1 && x[0] == '-':
			if strings.ContainsAny(x[1:], "FXP") {
				return ClasseDestructrice, "iptables " + x + " removes firewall rules or the default policy"
			}
			lit = lit || strings.ContainsAny(x[1:], "LS")
			change = change || strings.ContainsAny(x[1:], "AIDRNEZ")
		}
	}
	if lit && !change {
		return ClasseLecture, ""
	}
	return ClasseChangement, "iptables changes the firewall"
}

var reglesCommandes = map[string]regle{
	"grep": regleGrep, "egrep": regleGrep, "fgrep": regleGrep, "zgrep": regleGrep,
	"ps": reglePs,
	"find": func(a []string, _ string) (Classe, string) {
		switch {
		case slices.Contains(a, "-delete"):
			return ClasseDestructrice, "find -delete deletes files"
		case unParmi(a, "-exec -execdir -ok -okdir"):
			return ClasseDestructrice, "find -exec runs a hidden command"
		case unParmi(a, "-fprint -fprint0 -fprintf -fls"):
			return ClasseChangement, "find writes a file"
		}
		return ClasseLecture, ""
	},
	"sed": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "if", "--in-place", "--file") {
			return ClasseChangement, "sed edits files in place or runs a script file"
		}
		var scripts []string
		for i, x := range a {
			if s, ok := strings.CutPrefix(x, "--expression="); ok {
				scripts = append(scripts, s)
			} else if (x == "--expression" || len(x) > 1 && x[0] == '-' && x[1] != '-' && strings.HasSuffix(x, "e")) && i+1 < len(a) {
				scripts = append(scripts, a[i+1])
			}
		}
		if len(scripts) == 0 {
			scripts = positionnels(a)[:min(1, len(positionnels(a)))]
		}
		for _, s := range scripts {
			if !reSedLecture.MatchString(s) {
				return ClasseChangement, "sed script not recognised as read-only: " + s
			}
		}
		return ClasseLecture, ""
	},
	"awk": regleAwk, "gawk": regleAwk, "mawk": regleAwk, "nawk": regleAwk,
	"sort": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "", "--compress-program") {
			return ClasseDestructrice, "sort --compress-program runs a command"
		}
		if optionShell(a, "o", "--output") {
			return ClasseChangement, "sort -o writes a file"
		}
		return ClasseLecture, ""
	},
	"uniq": func(a []string, _ string) (Classe, string) {
		if len(positionnels(a)) > 1 {
			return ClasseChangement, "uniq writes its second file"
		}
		return ClasseLecture, ""
	},
	"tee": func([]string, string) (Classe, string) { return ClasseChangement, "tee writes files" },
	"date": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "s", "--set") || slices.ContainsFunc(positionnels(a), func(x string) bool { return !strings.HasPrefix(x, "+") }) {
			return ClasseChangement, "date sets the clock"
		}
		return ClasseLecture, ""
	},
	"hostname": func(a []string, _ string) (Classe, string) {
		lus := ensemble("-I -i -f -s -d -A -a -y --fqdn --long --short --all-ip-addresses --all-fqdns --ip-address --domain")
		if slices.ContainsFunc(a, func(x string) bool { return !lus[x] }) {
			return ClasseChangement, "hostname changes the host name"
		}
		return ClasseLecture, ""
	},
	"dmesg": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "cCDEn", "--clear", "--read-clear", "--console-off", "--console-on", "--console-level") {
			return ClasseChangement, "dmesg clears or changes the kernel log"
		}
		return ClasseLecture, ""
	},
	"journalctl": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "", "--vacuum-size", "--vacuum-time", "--vacuum-files", "--rotate", "--flush", "--sync", "--relinquish-var", "--smart-relinquish-var", "--setup-keys", "--update-catalog") {
			return ClasseChangement, "journalctl removes or rotates logs"
		}
		return ClasseLecture, ""
	},
	"mount": func(a []string, _ string) (Classe, string) {
		if len(a) == 0 || len(a) == 1 && a[0] == "-l" {
			return ClasseLecture, ""
		}
		return ClasseChangement, "mount changes the mounted file systems"
	},
	"sysctl": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "wp", "--write", "--load", "--system") || slices.ContainsFunc(a, func(x string) bool { return strings.Contains(x, "=") }) {
			return ClasseChangement, "sysctl changes kernel settings"
		}
		return ClasseLecture, ""
	},
	"curl": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "oOdXTFKcD", "--output", "--remote-name", "--remote-name-all", "--output-dir", "--data", "--data-raw", "--data-binary", "--data-urlencode", "--data-ascii", "--json", "--request", "--upload-file", "--form", "--form-string", "--config", "--cookie-jar", "--dump-header", "--trace", "--trace-ascii", "--stderr", "--libcurl", "--etag-save", "--hsts", "--alt-svc") {
			return ClasseChangement, "curl writes a file or sends data"
		}
		return ClasseLecture, ""
	},
	"wget": func(a []string, _ string) (Classe, string) {
		sortie := false
		for i, x := range a {
			suivant := i+1 < len(a) && a[i+1] == "-"
			sortie = sortie || x == "-O-" || x == "-qO-" || x == "--output-document=-" || (x == "-O" || x == "-qO" || x == "--output-document") && suivant
		}
		if !sortie || optionShell(a, "oa", "--output-file", "--append-output", "--post-data", "--post-file", "--method", "--body-data", "--body-file") {
			return ClasseChangement, "wget writes a file or sends data"
		}
		return ClasseLecture, ""
	},
	"kill": func(a []string, _ string) (Classe, string) {
		if len(a) > 0 && strings.HasPrefix(a[0], "-") && a[0] != "--" {
			a = a[1:] // the signal
		}
		if unParmi(a, "1 -1") {
			return ClasseDestructrice, "kill targets init or every process"
		}
		return ClasseChangement, "kill stops processes"
	},
	"pkill":   reglePkill,
	"killall": reglePkill,
	"systemctl": func(a []string, _ string) (Classe, string) {
		p := positionnels(a)
		switch {
		case unParmi(p, "poweroff reboot halt kexec isolate rescue emergency suspend hibernate default soft-reboot"):
			return ClasseDestructrice, "systemctl stops or restarts the whole server"
		case unParmi(p, "stop disable mask kill") && uniteVitale(p):
			return ClasseDestructrice, "systemctl cuts SSH or the network"
		}
		return classerSous("systemctl", p, "status show list-units list-unit-files list-sockets list-timers list-dependencies list-jobs is-active is-enabled is-failed is-system-running cat get-default show-environment help", "")
	},
	"service": func(a []string, _ string) (Classe, string) {
		p := positionnels(a)
		switch {
		case slices.Contains(a, "--status-all") || len(p) == 2 && p[1] == "status":
			return ClasseLecture, ""
		case len(p) >= 2 && unParmi(p[1:2], "stop kill disable") && uniteVitale(p[:1]):
			return ClasseDestructrice, "service stops SSH or the network"
		}
		return ClasseChangement, "service changes a running service"
	},
	"ufw": func(a []string, _ string) (Classe, string) {
		p := positionnels(a)
		switch {
		case unParmi(p, "reset disable"):
			return ClasseDestructrice, "ufw " + p[0] + " removes the firewall rules"
		case unParmi(p, "deny reject delete") && unParmi(p, "22 22/tcp ssh OpenSSH"):
			return ClasseDestructrice, "ufw cuts SSH access"
		case premier(p) == "app" && unParmi(p[1:], "list info"):
			return ClasseLecture, ""
		}
		return classerSous("ufw", p, "status show version", "")
	},
	"iptables": regleIptables, "ip6tables": regleIptables, "iptables-legacy": regleIptables, "ip6tables-legacy": regleIptables,
	"iptables-nft": regleIptables, "ip6tables-nft": regleIptables, "ebtables": regleIptables, "arptables": regleIptables,
	"nft": func(a []string, _ string) (Classe, string) {
		return classerSous("nft", positionnels(a), "list", "flush delete destroy")
	},
	"ip": func(a []string, _ string) (Classe, string) {
		p := positionnels(a)
		switch {
		case slices.ContainsFunc(a, func(x string) bool {
			return x == "-b" || strings.HasPrefix(x, "-batch") || strings.HasPrefix(x, "--batch")
		}):
			return ClasseDestructrice, "ip -batch runs commands read from a file"
		case unParmi(p, "flush del delete down exec"):
			return ClasseDestructrice, "ip removes addresses or routes, takes a link down or runs a command"
		case unParmi(p, "set add change replace append prepend up save restore"):
			return ClasseChangement, "ip changes the network"
		}
		return ClasseLecture, ""
	},
	"ifconfig": func(a []string, _ string) (Classe, string) {
		switch {
		case slices.Contains(a, "down"):
			return ClasseDestructrice, "ifconfig takes a link down"
		case len(positionnels(a)) <= 1:
			return ClasseLecture, ""
		}
		return ClasseChangement, "ifconfig changes the network"
	},
	"docker": regleDocker("docker"), "podman": regleDocker("podman"), "docker-compose": regleDocker("docker-compose"),
	"kubectl": func(a []string, _ string) (Classe, string) {
		return classerSous("kubectl", positionnels(a), "get describe logs top version cluster-info api-resources api-versions explain", "delete drain exec run debug")
	},
	"iptables-save": regleSave, "ip6tables-save": regleSave,
	"ss": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "K", "--kill") {
			return ClasseDestructrice, "ss -K closes connections"
		}
		return ClasseLecture, ""
	},
	"git": func(a []string, _ string) (Classe, string) {
		// shortcut: a pager or external diff set in the repository's own config still runs on read commands; refuse such repos when agents may write them.
		if optionShell(a, "c", "--config-env", "--exec-path") {
			return ClasseDestructrice, "git -c can run a command"
		}
		if optionShell(a, "", "--output") {
			return ClasseChangement, "git --output writes a file"
		}
		p := positionnels(a)
		switch {
		case premier(p) == "clean", premier(p) == "reset" && slices.Contains(a, "--hard"),
			premier(p) == "push" && optionShell(a, "f", "--force", "--force-with-lease", "--delete", "--mirror", "--prune"):
			return ClasseDestructrice, "git " + p[0] + " drops work for good"
		}
		return classerSous("git", p, "status log diff show rev-parse ls-files blame describe shortlog", "")
	},
	"crontab": func(a []string, _ string) (Classe, string) {
		switch {
		case optionShell(a, "r"):
			return ClasseDestructrice, "crontab -r removes every scheduled task"
		case optionShell(a, "l"):
			return ClasseLecture, ""
		}
		return ClasseChangement, "crontab changes scheduled tasks"
	},
	"dpkg": func(a []string, _ string) (Classe, string) {
		switch {
		case optionShell(a, "rP", "--remove", "--purge"):
			return ClasseDestructrice, "dpkg removes packages"
		case optionShell(a, "lLsSp", "--list", "--listfiles", "--status", "--search", "--get-selections", "--print-avail"):
			return ClasseLecture, ""
		}
		return ClasseChangement, "dpkg changes packages"
	},
	"rpm": func(a []string, _ string) (Classe, string) {
		switch {
		case optionShell(a, "e", "--erase"):
			return ClasseDestructrice, "rpm removes packages"
		case optionShell(a, "qV", "--query", "--verify"):
			return ClasseLecture, ""
		}
		return ClasseChangement, "rpm changes packages"
	},
	"rsync": func(a []string, _ string) (Classe, string) {
		if optionShell(a, "e", "--rsh") || slices.ContainsFunc(a, func(x string) bool {
			return strings.HasPrefix(x, "--delete") || strings.HasPrefix(x, "--remove-source")
		}) {
			return ClasseDestructrice, "rsync deletes files or runs a remote shell"
		}
		return ClasseChangement, "rsync copies files"
	},
	"mv": func(a []string, _ string) (Classe, string) {
		if slices.Contains(a, "/dev/null") {
			return ClasseDestructrice, "mv to /dev/null destroys the file"
		}
		return ClasseChangement, "mv moves or overwrites files"
	},
	"init":    regleInit,
	"telinit": regleInit,
	"alias": func(a []string, _ string) (Classe, string) {
		if len(a) == 0 || len(a) == 1 && a[0] == "-p" {
			return ClasseLecture, ""
		}
		return ClasseDestructrice, "alias redefines a command"
	},
}

func init() {
	for _, n := range []string{"apt", "apt-get", "dnf", "yum", "zypper", "apk"} {
		reglesCommandes[n] = func(a []string, _ string) (Classe, string) {
			return classerSous(n, positionnels(a), "list show search policy info depends rdepends madison showpkg check-update repolist", "remove purge autoremove autopurge erase del uninstall")
		}
	}
	for _, n := range []string{"chmod", "chown", "chgrp", "setfacl", "chattr"} {
		reglesCommandes[n] = func(a []string, _ string) (Classe, string) {
			if optionShell(a, "R", "--recursive") {
				return ClasseDestructrice, n + " -R changes a whole tree"
			}
			return ClasseChangement, n + " changes permissions"
		}
	}
	for _, n := range []string{"psql", "mysql", "mariadb", "sqlite3", "redis-cli", "mongo", "mongosh", "clickhouse-client", "cqlsh"} {
		reglesCommandes[n] = func(_ []string, texte string) (Classe, string) {
			if reSQLDetruit.MatchString(texte) {
				return ClasseDestructrice, n + " deletes database data (DROP, TRUNCATE, DELETE…)"
			}
			return ClasseChangement, n + " runs database statements"
		}
	}
}

func regleSave(a []string, _ string) (Classe, string) {
	if optionShell(a, "f", "--file") {
		return ClasseChangement, "iptables-save -f writes a file"
	}
	return ClasseLecture, ""
}

// regleGrep (B60): a recursive grep reads every file of a tree, so the .env, keys and /etc/shadow it holds,
// whatever the account (root needs no sudo): the owner sees it in a plan.
func regleGrep(a []string, _ string) (Classe, string) {
	if optionShell(a, "rR", "--recursive", "--dereference-recursive") || slices.ContainsFunc(a, func(x string) bool { return strings.HasSuffix(x, "recurse") }) {
		return ClasseChangement, "a recursive grep reads every file of a folder, secrets included"
	}
	return ClasseLecture, ""
}

// reglePs (C47): the BSD option e shows each process's environment (passwords, tokens).
// shortcut: a value after a value option (-o, -p, axo…) is skipped, a BSD group with e elsewhere errs on the safe side.
func reglePs(a []string, _ string) (Classe, string) {
	for i, x := range a {
		prev := ""
		if i > 0 {
			prev = a[i-1]
		}
		valeur := ensemble("--format --sort --pid --ppid --user --User --group --Group --tty --sid --quick-pid --cols --columns --rows --lines --width")[prev] ||
			!strings.HasPrefix(prev, "--") && prev != "" && strings.ContainsAny(prev[len(prev)-1:], "oOpqUutgGCk")
		if x != "" && x[0] != '-' && strings.Contains(x, "e") && !valeur {
			return ClasseChangement, "ps " + x + " shows each process's environment, which may hold secrets"
		}
	}
	return ClasseLecture, ""
}

func regleAwk(a []string, _ string) (Classe, string) {
	programme := ""
	for i := 0; i < len(a); i++ {
		x := a[i]
		switch {
		case x == "-f" || x == "-E" || x == "-i" || x == "-l" || strings.HasPrefix(x, "--file") || strings.HasPrefix(x, "--exec") || strings.HasPrefix(x, "--include") || strings.HasPrefix(x, "--load"):
			return ClasseDestructrice, "awk runs a program file"
		case x == "-F" || x == "-v" || x == "--field-separator" || x == "--assign":
			i++
		case (x == "-e" || x == "--source") && i+1 < len(a):
			i++
			programme += " " + a[i]
		case strings.HasPrefix(x, "--source="):
			programme += " " + strings.TrimPrefix(x, "--source=")
		case strings.HasPrefix(x, "-"):
		default:
			programme += " " + x
			i = len(a)
		}
	}
	switch {
	case strings.Contains(programme, "system") || strings.Contains(programme, "|") || strings.Contains(programme, "getline") || strings.Contains(programme, "@"):
		return ClasseDestructrice, "awk program can run commands"
	case strings.Contains(programme, ">"):
		return ClasseChangement, "awk program may write a file"
	}
	return ClasseLecture, ""
}

func reglePkill(a []string, _ string) (Classe, string) {
	if slices.ContainsFunc(positionnels(a), func(x string) bool {
		return strings.Contains(x, "ssh") || strings.Contains(x, "systemd") || x == "init"
	}) {
		return ClasseDestructrice, "kills SSH or the init system"
	}
	return ClasseChangement, "stops processes"
}

func regleInit(a []string, _ string) (Classe, string) {
	if unParmi(a, "0 6") {
		return ClasseDestructrice, "stops or restarts the server"
	}
	return ClasseChangement, "changes the run level"
}

// --- Paths ---

// variantesMot: the word, its value after '=', a glued short option's value (-f/etc/x), each with its braces expanded.
func variantesMot(t string) []string {
	v := []string{t}
	if _, apres, ok := strings.Cut(t, "="); ok {
		v = append(v, apres)
	}
	if len(t) > 2 && t[0] == '-' && t[1] != '-' {
		v = append(v, t[2:])
	}
	var tout []string
	for _, x := range v {
		tout = append(tout, accolades(x, 64)...)
	}
	return tout
}

// accolades: brace expansion ({a,b}) up to max results; ranges and odd braces stay as written.
func accolades(s string, max int) []string {
	i := strings.IndexByte(s, '{')
	if i < 0 {
		return []string{s}
	}
	j := fin(s, i, '{', '}')
	if j < 0 {
		return []string{s}
	}
	var alts []string
	prof, debut := 0, i+1
	for k := i + 1; k < j; k++ {
		switch s[k] {
		case '{':
			prof++
		case '}':
			prof--
		case ',':
			if prof == 0 {
				alts, debut = append(alts, s[debut:k]), k+1
			}
		}
	}
	if alts == nil {
		return []string{s}
	}
	alts = append(alts, s[debut:j])
	var r []string
	for _, a := range alts {
		for _, x := range accolades(s[:i]+a+s[j+1:], max) {
			if len(r) >= max {
				return r
			}
			r = append(r, x)
		}
	}
	return r
}

func nettoyer(p string) []string {
	if strings.Contains(p, "/") {
		p = path.Clean(p)
	}
	return strings.Split(p, "/")
}

func nomSecret(b string) bool {
	switch {
	case b == "shadow", b == "gshadow", b == "shadow-", b == "gshadow-", b == "environ", b == ".env",
		strings.HasSuffix(b, ".env"), strings.HasPrefix(b, ".env."),
		(strings.HasPrefix(b, "id_") || strings.HasSuffix(b, "_key")) && !strings.HasSuffix(b, ".pub"),
		b == ".my.cnf", b == "debian.cnf", b == ".pgpass", b == "credentials", b == ".netrc",
		strings.HasPrefix(b, "wg") && strings.HasSuffix(b, ".conf"):
		return true
	}
	return slices.Contains([]string{".key", ".pem", ".p12", ".pfx", ".ppk"}, path.Ext(b))
}

// cheminSecret: nomSecret, or a file that is only a secret in its folder (~/.docker/config.json, ~/.kube/config).
func cheminSecret(c []string) bool {
	n := len(c)
	return nomSecret(c[n-1]) || n > 1 && slices.Contains([]string{".docker/config.json", ".kube/config"}, c[n-2]+"/"+c[n-1])
}

var candidatsSecrets = []string{"shadow", "gshadow", "environ", ".env", "prod.env", ".env.local", "id_rsa", "id_ed25519", "ssh_host_ed25519_key", "server.key", "privkey.pem", "cert.p12", ".my.cnf", "debian.cnf", ".pgpass", "credentials", ".netrc", "wg0.conf"}

// sauvegardes: a word that may reach Athanor's backups (M92), also through a glob (/var/back*/athanor) or a relative
// path (backups/athanor). shortcut: `cd /var/backups && cat athanor/x` is only caught by the cd (a change), not refused.
func sauvegardes(v string, c []string) bool {
	if dansSauvegardes(v) || strings.Contains(v, "backups/athanor") {
		return true
	}
	d := strings.Split(dossierSauvegardes, "/")
	if len(c) < len(d) {
		return false
	}
	for i := range d {
		if ok, _ := path.Match(c[i], d[i]); !ok {
			return false
		}
	}
	return true
}

// lectureInterdite: whether a word may name a file whose content is a secret (C36: /etc/shadow, ~/.ssh except
// authorized_keys, *.env, private keys). Destructive for a direct hit, a change for a bare '*' that could match one.
func lectureInterdite(t string) Classe {
	pire := ClasseLecture
	for _, v := range variantesMot(t) {
		c := nettoyer(v)
		if sauvegardes(v, c) { // M92: Athanor's archives may hold secrets; restoring one is the owner's accord
			return ClasseDestructrice
		}
		if p := strings.Join(c, "/"); p == "/var/backups" || p == "/var/backups/" {
			pire = ClasseChangement // the folder that holds Athanor's backups: cd there then a relative path reaches them
		}
		base := c[len(c)-1]
		joker := strings.ContainsAny(v, "*?[")
		correspond := func(motif, nom string) bool {
			ok, _ := path.Match(motif, nom)
			return motif == nom || joker && ok
		}
		touche := base != "authorized_keys" && slices.ContainsFunc(c, func(x string) bool { return correspond(x, ".ssh") })
		if touche || cheminSecret(c) {
			return ClasseDestructrice
		}
		if joker && slices.ContainsFunc(candidatsSecrets, func(n string) bool { return correspond(base, n) }) {
			if base != "*" {
				return ClasseDestructrice
			}
			pire = ClasseChangement
		}
	}
	return pire
}

// cheminCritique: paths whose change can lock the owner out, break the boot or drive the kernel (/proc/sys*,
// /proc/sysrq-trigger, /sys): writes there are destructive.
func cheminCritique(t string) bool {
	c := nettoyer(t)
	p := strings.Join(c, "/")
	if strings.HasPrefix(p, "/proc/sys") || p == "/sys" || strings.HasPrefix(p, "/sys/") {
		return true
	}
	for _, d := range []string{"/etc/ssh", "/boot", "/etc/pam.d", "/etc/sudoers.d"} {
		if p == d || strings.HasPrefix(p, d+"/") {
			return true
		}
	}
	if strings.HasPrefix(p, "/etc/sudoers") || unParmi(c, ".ssh sudoers.d sshd_config.d") {
		return true
	}
	return ensemble("sshd_config sudoers shadow gshadow passwd group fstab authorized_keys")[c[len(c)-1]]
}

func disque(t string) bool {
	return slices.ContainsFunc([]string{"/dev/sd", "/dev/hd", "/dev/vd", "/dev/xvd", "/dev/nvme", "/dev/mmcblk", "/dev/mapper/", "/dev/disk/", "/dev/md", "/dev/dm-", "/dev/mem", "/dev/kmem", "/dev/port"},
		func(d string) bool { return strings.HasPrefix(t, d) })
}

// Etape: a Servers tool call as the code classed it (M90's plans are made of these).
type Etape struct {
	Outil     string
	Arguments json.RawMessage
	Classe    Classe
	Raison    string
	// set by planOuRegle for a step of an accepted plan: archived before it runs (M92)
	Plan       string
	Num        int
	Sauvegarde []string
}

// classerOutil classes a typed tool's call by the tool and its arguments (ARCHITECTURE.md §Garde-fous).
func classerOutil(outil string, a argsOutilServeur) (Classe, string) {
	switch outil {
	case "ssh_commande":
		return classerCommande(a.Commande)
	case "fichier_lire", "sftp_recuperer":
		return ClasseLecture, "reads a file"
	case "fichier_ecrire", "sftp_envoyer":
		p := cmp.Or(a.Chemin, a.Distant)
		if cheminCritique(p) {
			return ClasseDestructrice, "writing " + p + " can lock the owner out or break the boot"
		}
		return ClasseChangement, "writes " + p
	case "service":
		if a.Action == "status" {
			return ClasseLecture, "reads a service's state"
		}
		// the same rules as the free command (stopping SSH or the network is destructive)
		c, r := classerCommande("systemctl " + citerShell(a.Action) + " " + citerShell(a.Nom))
		if c < ClasseChangement {
			c, r = ClasseChangement, a.Action+" changes a service"
		}
		return c, r
	case "docker":
		switch a.Action {
		case "ps", "logs", "compose_ps":
			return ClasseLecture, "reads Docker's state"
		case "compose_down":
			return ClasseDestructrice, "compose_down stops and removes a Compose project's containers"
		}
		return ClasseChangement, a.Action + " changes containers or images"
	case "pare_feu":
		if a.Action == "status" {
			return ClasseLecture, "reads the firewall's state"
		}
		return ClasseChangement, a.Action + " changes a firewall rule"
	}
	return ClasseDestructrice, "unknown tool"
}

// cheminRefuse (C36): a path no Servers tool reads or writes: /etc/shadow, /etc/gshadow, ~/.ssh/ (except
// reading authorized_keys), *.env, private keys. Checked as given, then by the tools once resolved on the server.
func cheminRefuse(p string, lecture bool) error {
	c := nettoyer(p)
	base := c[len(c)-1]
	if cheminSecret(c) || slices.Contains(c, ".ssh") && !(lecture && base == "authorized_keys") {
		return fmt.Errorf("%s is refused to every Servers tool: secrets (/etc/shadow, ~/.ssh, *.env, private keys) are never read nor overwritten by an agent", p)
	}
	if dansSauvegardes(p) {
		return fmt.Errorf("%s is refused to every Servers tool: the backups Athanor makes (%s) may hold secrets and belong to the owner", p, dossierSauvegardes)
	}
	return nil
}

// dossierSauvegardes: where Athanor archives a plan's step before it runs (M92); no agent reads it.
const dossierSauvegardes = "/var/backups/athanor"

func dansSauvegardes(p string) bool {
	return strings.HasPrefix(path.Clean(p)+"/", dossierSauvegardes+"/")
}
