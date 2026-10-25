package main

// athanor pilote [PROJECT] (or athanor pilot [PROJECT]): the autonomous work loop, parallel workers
// included, started by athanor demarrer (which starts it again after an internal error of its own). Its
// files: the workshop (.athanor/STATUS.txt, STATE.json, stats.tsv, pilot.log, pilot.pid, logs/cycle-*.jsonl)
// and the pilot's private folder (logs/pilotes/<id>/).

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	reCleEnvPilote      = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)
	reEntierPilote      = regexp.MustCompile(`^[0-9]{1,12}$`)
	rePidSurPilote      = regexp.MustCompile(`^[0-9]{2,9}$`)
	reJalonOuvertPilote = regexp.MustCompile(`(?m)^- \[ \] (.*)$`)
	reJalonCochePilote  = regexp.MustCompile(`(?m)^- \[[xX]\]`)
	reJalonToutPilote   = regexp.MustCompile(`(?m)^- \[[ xX]\] `)
	reStatusDonePilote  = regexp.MustCompile(`STATUS: *DONE[^*]*`)
	reVersionClaude     = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)
	reHitLimitPilote    = regexp.MustCompile(`(?i)hit your .*limit`)
	reQuotaTextePilote  = regexp.MustCompile(`(?i)hit your .*limit|usage limit|limit reached|rate[ _-]?limit|spend limit`)
	reConnTextePilote   = regexp.MustCompile(`(?i)not logged in|/login|invalid api key|oauth token|authentication_failed|invalid authentication|login expired`)
	reResetsAtPilote    = regexp.MustCompile(`(?i)resets? (?:at )?([A-Za-z]{0,3}) ?([0-9]{1,2})(?::([0-9]{2}))? ?(am|pm)?`)
	reGitignoreEnv      = regexp.MustCompile(`(?m)^[[:space:]]*!?(\*\*/|/)?\*?\.env\*?[[:space:]]*$`)
	reGitignoreAtelier  = regexp.MustCompile(`(?m)^[[:space:]]*/?\.(athanor|claude)(/|/\*)?[[:space:]]*$`)
	reLigneDemande      = regexp.MustCompile(`^\s*(#|\(|$)`)
	reAlnumPilote       = regexp.MustCompile(`[[:alnum:]]`)
)

const formatJalonPilote = `each milestone one line starting with "- [ ] M1 : " (the pilot reads only those lines) and written as a self-contained sheet (Files, Depends on, Acceptance, Read)`

type pilote struct {
	kit, systeme, tools, projet, etat, prive, reglages, modeles, consignes string
	natif                                                                  bool
	claudeBin, cheminClaude, configClaude                                  string

	maxHours, quotaWaitMin, errorWaitMin, errorWaitMaxMin, loginWaitMin int
	maxCyclesWithoutProgress, maxTurnsPerCycle, framingMaxMin           int
	relayContextK, idleMaxMin, cycleMaxMin, watchInterval               int
	secondsPerMinute                                                    int
	closeAfterMilestone, stayAliveAtEnd, keepAwake                      string
	model, fallbackModel, effort, subagentModel, framingModel           string
	workerModel, cheapModel, language, phraseLangue                     string

	argsSurs                                                     []string
	appendFichierOK, agentsFichierOK, agentsParJSON, pluginDirOK bool
	agentsJSON, pluginAthanor, extensionsAvant, ouvrirAvant      string
	pluginsDirs                                                  []string
	unrealMCP, robloxMCP, blenderMCP, mcpProgrammes              bool
	unrealURL, unrealPrecedent, blenderEchec, identitePrecedente string
	robloxSansPlace, syncAnnonce, paralleleRefusSignale          bool
	ecranImposs, ecranSignale                                    bool
	ecranAvant                                                   map[string]string

	cycle          int
	debut          int64
	etatPhase      string
	etatReprise    string
	etatDebutCycle string
	etatOuvriers   string
	modeleCycle    string
	fichierLog     string
	verrouVu       bool
	parallel       bool
	parallelMax    int
	quotaOuvriers  bool
	modeleVerif    string
	verrouFichier  *os.File
	pontCmd        *exec.Cmd
	syncCmd        *exec.Cmd
	ecranCmd       *exec.Cmd
	arretCh        chan struct{}
	arretOnce      sync.Once
	enArret        atomic.Bool
	battementStop  chan struct{}
	// several computers on the project (pcs_pilote.go): the milestone this computer took, its team
	jalonPC     *Jalon
	equipePC    *equipePC
	logsUneFois map[string]bool
	envoiGH     string // the last commit the automatic push was tried with (githubPousserNouveau)
	battementPC bool
	finiPC      atomic.Bool
}

// logUneFois: a message said once while its cause lasts (oublierLogUneFois: it may be said again).
func (p *pilote) logUneFois(cle, msg string) {
	if p.logsUneFois == nil {
		p.logsUneFois = map[string]bool{}
	}
	if !p.logsUneFois[cle] {
		p.logsUneFois[cle] = true
		p.log(msg)
	}
}

func (p *pilote) oublierLogUneFois(cle string) { delete(p.logsUneFois, cle) }

func lireTexteCourt(chemin string) string {
	b, err := os.ReadFile(chemin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func entierPilote(s string, defaut int) int {
	s = strings.TrimSpace(s)
	if !reEntierPilote.MatchString(s) {
		return defaut
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return defaut
	}
	return n
}

func entier64Pilote(s string, defaut int64) int64 {
	s = strings.TrimSpace(s)
	if !reEntierPilote.MatchString(s) {
		return defaut
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return defaut
	}
	return n
}

func pidSurPilote(s string) int {
	s = strings.TrimSpace(s)
	if !rePidSurPilote.MatchString(s) {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}

// priveDePilote: safety.sh's prive_de (same SHA-1 in WSL and native Windows for the same folder).
func priveDePilote(kit, projet string, natif bool) string {
	p := strings.TrimSuffix(strings.ReplaceAll(projet, `\`, "/"), "/")
	if natif {
		if len(p) >= 2 && p[1] == ':' {
			p = "/mnt/" + strings.ToLower(p[:1]) + p[2:]
		} else if m := reCheminGitBash.FindStringSubmatch(p); m != nil {
			p = "/mnt/" + strings.ToLower(m[1]) + m[2]
		}
	}
	h := sha1.Sum([]byte(p))
	return filepath.Join(kit, "logs", "pilotes", hex.EncodeToString(h[:])[:16])
}

func publierFichier(dest string, contenu []byte) error {
	dir := filepath.Dir(dest)
	if estLien(dir) {
		return os.ErrPermission
	}
	t, err := os.CreateTemp(dir, ".pub.*")
	if err != nil {
		return err
	}
	nom := t.Name()
	_, err = t.Write(contenu)
	_ = t.Chmod(0o644)
	if e := t.Close(); err == nil {
		err = e
	}
	if err != nil {
		_ = os.Remove(nom)
		return err
	}
	if err := os.Rename(nom, dest); err != nil {
		_ = os.Remove(nom)
		return err
	}
	return nil
}

func assainirAtelier(projet string) {
	etat := filepath.Join(projet, ".athanor")
	if estLien(etat) {
		_ = os.Remove(etat)
	}
	_ = os.MkdirAll(etat, 0o755)
	work := filepath.Join(etat, "work")
	_ = filepath.WalkDir(etat, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if p == work && d.IsDir() {
			return filepath.SkipDir
		}
		if d.Type()&os.ModeSymlink != 0 {
			_ = os.Remove(p)
		}
		return nil
	})
	for _, rel := range []string{".git", filepath.Join(".git", "info"), filepath.Join(".git", "info", "exclude")} {
		p := filepath.Join(projet, rel)
		if estLien(p) {
			_ = os.Remove(p)
		}
	}
}

func copierDossierModeles(src, dst string) {
	_ = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return nil
		}
		cible := filepath.Join(dst, rel)
		if d.IsDir() {
			_ = os.MkdirAll(cible, 0o755)
			return nil
		}
		if d.Type().IsRegular() {
			if b, err := os.ReadFile(p); err == nil {
				_ = os.MkdirAll(filepath.Dir(cible), 0o755)
				perm := fs.FileMode(0o644)
				if info, e := d.Info(); e == nil {
					perm = info.Mode().Perm()
				}
				_ = os.WriteFile(cible, b, perm)
			}
		}
		return nil
	})
}

func (p *pilote) log(msg string) {
	m := fmt.Sprintf("[%s] %s", time.Now().Format("2006-01-02 15:04:05"), msg)
	fmt.Println(m)
	jPrive := filepath.Join(p.prive, "pilot.log")
	if f, err := os.OpenFile(jPrive, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(m + "\n")
		f.Close()
	}
	if b, err := os.ReadFile(jPrive); err == nil {
		lignes := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lignes) > 3000 {
			lignes = lignes[len(lignes)-3000:]
		}
		_ = publierFichier(filepath.Join(p.etat, "pilot.log"), []byte(strings.Join(lignes, "\n")+"\n"))
	}
}

func (p *pilote) compterFileAttente() int {
	fs, _ := filepath.Glob(filepath.Join(p.etat, "queue", "*.md"))
	n := 0
	for _, f := range fs {
		if st, err := os.Stat(f); err == nil && !st.IsDir() {
			n++
		}
	}
	_ = os.Setenv("FILE_ATTENTE_N", strconv.Itoa(n))
	return n
}

func couperCaracteres(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

func (p *pilote) statut(etat string) {
	now := time.Now()
	txt := fmt.Sprintf("Updated : %s\nCycle   : %d\nState   : %s\n", now.Format("2006-01-02 15:04:05"), p.cycle, etat)
	_ = publierFichier(filepath.Join(p.etat, "STATUS.txt"), []byte(txt))
	phase := p.etatPhase
	if phase == "" {
		phase = "attente"
	}
	attente := p.compterFileAttente()
	jalon := ""
	if p.jalonPC != nil && p.jalonPC.Complet != "" { // several computers: the one this computer took
		jalon = couperCaracteres(p.jalonPC.Complet, 194)
	} else if b, err := os.ReadFile(filepath.Join(p.etat, "PLAN.md")); err == nil {
		if m := reJalonOuvertPilote.FindSubmatch(b); m != nil {
			jalon = couperCaracteres(strings.TrimSuffix(string(m[1]), "\r"), 194)
		}
	}
	commit := ""
	if out, err := gitSortie(p.projet, "log", "-1", "--pretty=%h %s"); err == nil {
		commit = couperCaracteres(strings.TrimSpace(out), 160)
	}
	modele := p.modeleCycle
	if modele == "" {
		modele = p.model
	}
	var o objetJSON
	o.mettre("maj", now.Format("2006-01-02T15:04:05"))
	o.mettre("pid", os.Getpid())
	o.mettre("phase", phase)
	o.mettre("etat", etat)
	o.mettre("cycle", p.cycle)
	o.mettre("template", modele)
	o.mettre("journal", p.fichierLog)
	o.mettre("jalon_en_cours", jalon)
	o.mettre("dernier_commit", commit)
	o.mettre("reprise_prevue", p.etatReprise)
	o.mettre("debut_cycle", p.etatDebutCycle)
	o.mettre("ouvriers", p.etatOuvriers)
	o.mettre("demandes_en_attente", attente)
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if enc.Encode(o) == nil {
		_ = publierFichier(filepath.Join(p.etat, "STATE.json"), b.Bytes())
	}
}

func (p *pilote) synchroniserDemande() {
	req := filepath.Join(p.etat, "REQUEST.md")
	b, err := os.ReadFile(req)
	if err != nil {
		return
	}
	sum := sha256.Sum256(b)
	nouveau := hex.EncodeToString(sum[:])
	shaFile := filepath.Join(p.prive, "demande.sha")
	ancien := lireTexteCourt(shaFile)
	if nouveau != ancien {
		_ = os.WriteFile(shaFile, []byte(nouveau+"\n"), 0o644)
		if ancien != "" {
			_ = os.WriteFile(filepath.Join(p.etat, "REQUEST_CHANGED"), []byte(time.Now().Format("2006-01-02 15:04")+"\n"), 0o644)
			p.log("REQUEST.md has changed: Claude will take it into account on the next cycle.")
		}
	}
}

func (p *pilote) prochaineDemande() bool {
	fs, _ := filepath.Glob(filepath.Join(p.etat, "queue", "*.md"))
	var fichiers []string
	for _, f := range fs {
		if st, err := os.Lstat(f); err == nil && st.Mode().IsRegular() {
			fichiers = append(fichiers, f)
		}
	}
	sort.Strings(fichiers)
	if len(fichiers) == 0 {
		return false
	}
	f := fichiers[0]
	nom := strings.TrimSuffix(filepath.Base(f), ".md")
	faite := filepath.Join(p.etat, "done", fmt.Sprintf("%s-%s.md", time.Now().Format("20060102-150405"), nom))
	if os.MkdirAll(filepath.Join(p.etat, "done"), 0o755) != nil || os.Rename(f, faite) != nil {
		p.log(fmt.Sprintf("Queue: cannot remove %q (%s).", nom, f))
		return false
	}
	b, _ := os.ReadFile(faite)
	texte := strings.ReplaceAll(string(b), "\r", "")
	if req, err := os.OpenFile(filepath.Join(p.etat, "REQUEST.md"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = fmt.Fprintf(req, "\n\n## Next request (%s, %s)\n\n%s\n", time.Now().Format("2006-01-02 15:04"), nom, texte)
		req.Close()
	}
	fr := filepath.Join(p.etat, "FINAL_REPORT.md")
	if fb, err := os.ReadFile(fr); err == nil {
		nf := reStatusDonePilote.ReplaceAllString(string(fb), "STATUS: REOPENED (next request in REQUEST.md)")
		_ = os.WriteFile(fr, []byte(nf), 0o644)
	}
	plan := filepath.Join(p.etat, "PLAN.md")
	if _, err := os.Stat(plan); err == nil {
		if pf, err := os.OpenFile(plan, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			_, _ = fmt.Fprintf(pf, "\n- [ ] Resume: new request %q (\"Next request\" section of .athanor/REQUEST.md)\n  - Re-read this request, turn it into verifiable milestones appended to this plan, the last one being a new delivery (.athanor/FINAL_REPORT.md with STATUS: DONE).\n", nom)
			pf.Close()
		}
	}
	p.synchroniserDemande()
	restants := p.compterFileAttente()
	p.log(fmt.Sprintf("Request done: moving on to %q (queue: %d remaining).", nom, restants))
	return true
}

func arrondirStat(v float64, mult float64) string {
	r := math.Round(v*mult) / mult
	if r == 0 {
		return "0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

func (p *pilote) enregistrerStats(f, etat, etiquette, depot string) {
	if etiquette == "" {
		etiquette = strconv.Itoa(p.cycle)
	}
	if depot == "" {
		depot = p.projet
	}
	statsPrive := filepath.Join(p.prive, "stats.tsv")
	if st, err := os.Stat(statsPrive); err != nil || st.Size() == 0 {
		_ = os.WriteFile(statsPrive, []byte("horodatage\tcycle\tmodele\tetat\tminutes\tminutes_api\ttours\tcout_usd\tentree_tokens\tcache_lu\tcache_ecrit\tsortie_tokens\tcontexte_max\tcommit\testimation\n"), 0o644)
	}
	tarifs := filepath.Join(p.kit, "logs", "tarifs.json")
	var res map[string]any
	var pic int64
	for _, o := range lignesJSON(f, 0) {
		switch o["type"] {
		case "result":
			res = o
		case "assistant":
			if o["parent_tool_use_id"] == nil {
				u := dict(dict(o["message"])["usage"])
				if u != nil {
					var tot int64
					for _, k := range []string{"input_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"} {
						if v, ok := entierJSON(u[k]); ok {
							tot += v
						}
					}
					if tot > pic {
						pic = tot
					}
				}
			}
		}
	}
	estime := 0
	if res != nil {
		coutApprendre(tarifs, f)
	} else {
		debutS := time.Now().Unix()
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", p.etatDebutCycle, time.Local); err == nil {
			debutS = t.Unix()
		}
		var brut string

		brut = strings.TrimSpace(coutEstimer(tarifs, f, p.configClaude))

		if brut != "" {
			dec := json.NewDecoder(strings.NewReader(brut))
			dec.UseNumber()
			if dec.Decode(&res) == nil && res != nil {
				d := time.Now().Unix() - debutS
				res["duration_ms"] = json.Number(strconv.FormatInt(d*1000, 10))
				estime = 1
			}
		}
	}
	var minutes, minutesAPI, tours, coutUSD, entree, cacheLu, cacheEcrit, sortie string
	if res != nil {
		dur, _ := nombreJSON(res["duration_ms"])
		durAPI, _ := nombreJSON(res["duration_api_ms"])
		nt, _ := entierJSON(res["num_turns"])
		cost, _ := nombreJSON(res["total_cost_usd"])
		u := dict(res["usage"])
		it, _ := entierJSON(u["input_tokens"])
		cr, _ := entierJSON(u["cache_read_input_tokens"])
		cw, _ := entierJSON(u["cache_creation_input_tokens"])
		ot, _ := entierJSON(u["output_tokens"])
		minutes = arrondirStat(dur/60000.0, 10)
		minutesAPI = arrondirStat(durAPI/60000.0, 10)
		tours = strconv.FormatInt(nt, 10)
		coutUSD = arrondirStat(cost, 100)
		entree = strconv.FormatInt(it, 10)
		cacheLu = strconv.FormatInt(cr, 10)
		cacheEcrit = strconv.FormatInt(cw, 10)
		sortie = strconv.FormatInt(ot, 10)
	}
	commit := ""
	if strings.HasPrefix(depot, "ref:") {
		commit, _ = gitSortie(p.projet, "log", "-1", "--pretty=%h", strings.TrimPrefix(depot, "ref:"))
	} else {
		commit, _ = gitSortie(depot, "log", "-1", "--pretty=%h")
	}
	commit = strings.TrimSpace(commit)
	modele := p.modeleCycle
	if modele == "" {
		modele = "defaut"
	}
	ligne := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%d\n",
		time.Now().Format("2006-01-02T15:04:05"), etiquette, modele, etat,
		minutes, minutesAPI, tours, coutUSD, entree, cacheLu, cacheEcrit, sortie,
		pic, commit, estime)
	if sf, err := os.OpenFile(statsPrive, os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = sf.WriteString(ligne)
		sf.Close()
	}
	if b, err := os.ReadFile(statsPrive); err == nil {
		_ = publierFichier(filepath.Join(p.etat, "stats.tsv"), b)
	}
}

func (p *pilote) dormir(secondes int) {
	s := secondes * p.secondsPerMinute / 60
	var d time.Duration
	if s > 0 {
		d = time.Duration(s) * time.Second
	} else if secondes > 0 && p.secondsPerMinute > 0 {
		d = 50 * time.Millisecond
	} else {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-p.arretCh:
		p.quitterSurSignal()
	}
}

func (p *pilote) estTermine() bool {
	b, err := os.ReadFile(filepath.Join(p.etat, "FINAL_REPORT.md"))
	return err == nil && reStatusDonePilote.Match(b)
}

func (p *pilote) terminer(msg string) {
	p.finPC() // its milestone is free for the other computers
	p.log(msg)
	if p.etatPhase == "" {
		p.etatPhase = "termine"
	}
	p.statut(msg)
	if p.stayAliveAtEnd == "1" {
		p.log("The pilot stays on standby. To stop it: stop")
		for {
			if _, err := os.Stat(filepath.Join(p.etat, "STOP")); err == nil {
				break
			}
			t := time.NewTimer(60 * time.Second)
			select {
			case <-t.C:
			case <-p.arretCh:
				t.Stop()
				p.quitterSurSignal()
			}
		}
		p.log("Stop requested (STOP file).")
	}
	p.nettoyerSortie()
	os.Exit(0)
}

func (p *pilote) connexionPresente() bool {
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" || os.Getenv("ANTHROPIC_API_KEY") != "" {
		return true
	}
	_, err := os.Stat(filepath.Join(p.configClaude, ".credentials.json"))
	return err == nil
}

func (p *pilote) tachesEtat() (pourVous, aTraiter int) {
	return etatTachesCompte(p.projet)
}

func etatTachesCompte(projet string) (int, int) {
	f := lireTachesUtil(projet)
	r := lireReponsesUtil(projet)
	pv, at := 0, 0
	for _, t := range f.taches {
		if t.statut == "ok" || t.statut == "resolu" {
			continue
		}
		if int64(len(r[t.id])) > t.tour {
			at++
		} else {
			pv++
		}
	}
	return pv, at
}

func (p *pilote) signalerReponses() {
	pv, at := p.tachesEtat()
	fAns := filepath.Join(p.etat, "FOR_YOU_ANSWERED")
	if at > 0 {
		if _, err := os.Stat(fAns); err != nil {
			p.log(fmt.Sprintf("You answered %d task(s): Claude checks them first.", at))
		}
		_ = publierFichier(fAns, []byte(time.Now().Format("2006-01-02 15:04")+"\n"))
	} else {
		_ = os.Remove(fAns)
	}
	fWait := filepath.Join(p.etat, "WAITING_FOR_YOU")
	if _, err := os.Stat(fWait); err == nil && (pv == 0 || at > 0) {
		_ = os.Remove(fWait)
	}
}

func (p *pilote) attendreUtilisateur() {
	pv, at := p.tachesEtat()
	fWait := filepath.Join(p.etat, "WAITING_FOR_YOU")
	fStop := filepath.Join(p.etat, "STOP")
	if pv == 0 || at > 0 {
		_ = os.Remove(fWait)
		return
	}
	p.log(fmt.Sprintf("Waiting for you: %d task(s) on the For you page. The pilot resumes as soon as you answer one.", pv))
	p.etatPhase = "attente_vous"
	p.statut(fmt.Sprintf("Waiting for you (%d task(s))", pv))
	debutAttente := time.Now().Unix()
	shaDemande := func() string {
		b, err := os.ReadFile(filepath.Join(p.etat, "REQUEST.md"))
		if err != nil {
			return ""
		}
		s := sha256.Sum256(b)
		return hex.EncodeToString(s[:])
	}
	sha := shaDemande()
	n := 0
	for {
		if _, err := os.Stat(fWait); err != nil {
			break
		}
		if _, err := os.Stat(fStop); err == nil {
			break
		}
		p.dormir(10)
		pv, at = p.tachesEtat()
		if at > 0 || pv == 0 {
			break
		}
		if shaDemande() != sha {
			break
		}
		n++
		if n%30 == 0 {
			p.statut(fmt.Sprintf("Waiting for you (%d task(s))", pv))
		}
	}
	_ = os.Remove(fWait)
	p.debut += time.Now().Unix() - debutAttente
	_ = os.WriteFile(filepath.Join(p.prive, "debut"), []byte(strconv.FormatInt(p.debut, 10)+"\n"), 0o644)
	if _, err := os.Stat(fStop); err != nil {
		p.log("Resuming: an answer arrived (or the request changed).")
	}
	p.etatPhase = "entre_cycles"
}

func (p *pilote) cochesPlan() int {
	b, err := os.ReadFile(filepath.Join(p.etat, "PLAN.md"))
	if err != nil {
		return 0
	}
	return len(reJalonCochePilote.FindAll(b, -1))
}

func (p *pilote) planPret() bool {
	b, err := os.ReadFile(filepath.Join(p.etat, "PLAN.md"))
	return err == nil && reJalonToutPilote.Match(b)
}

func (p *pilote) jalonsPlan() int {
	b, err := os.ReadFile(filepath.Join(p.etat, "PLAN.md"))
	if err != nil {
		return 0
	}
	return len(reJalonToutPilote.FindAll(b, -1))
}

func journalQuotaPilote(chemin string) bool {
	objs := lignesJSON(chemin, 0)
	var res map[string]any
	rejected := false
	for _, o := range objs {
		switch o["type"] {
		case "result":
			res = o
		case "rate_limit_event":
			if info := dict(o["rate_limit_info"]); info != nil && info["status"] == "rejected" {
				rejected = true
			}
		}
	}
	if res != nil {
		if isErr, _ := res["is_error"].(bool); !isErr {
			return false
		}
	}
	if rejected {
		return true
	}
	if res != nil {
		if r, ok := texte(res["result"]); ok && reQuotaTextePilote.MatchString(r) {
			return true
		}
	}
	return false
}

func (p *pilote) seuilQuota() string {
	regles := filepath.Join(p.kit, "logs", "quota-arret.json")
	if st, err := os.Stat(regles); err != nil || st.Size() == 0 {
		return ""
	}
	fs, _ := filepath.Glob(filepath.Join(p.etat, "logs", "*.jsonl"))
	type j struct {
		f string
		t time.Time
	}
	var js []j
	for _, f := range fs {
		if st, err := os.Stat(f); err == nil {
			js = append(js, j{f, st.ModTime()})
		}
	}
	sort.Slice(js, func(i, k int) bool { return js[i].t.After(js[k].t) })
	if len(js) == 0 {
		return ""
	}
	if len(js) > 6 {
		js = js[:6]
	}
	var noms []string
	for _, x := range js {
		noms = append(noms, x.f)
	}
	var s string

	s = quotaVerifier(regles, noms)

	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func (p *pilote) appliquerSeuilQuota() {
	l := p.seuilQuota()
	if l == "" {
		return
	}
	parts := strings.SplitN(l, "\t", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	action, reinitStr, message := parts[0], parts[1], parts[2]
	reinit := entier64Pilote(reinitStr, 0)
	fStop := filepath.Join(p.etat, "STOP")
	if action == "pause" && reinit > 0 {
		reprise := time.Unix(reinit+60, 0).Format("2006-01-02 15:04")
		p.etatPhase = "quota"
		p.etatReprise = reprise
		p.log(fmt.Sprintf("Quota threshold reached: %s. Pausing until about %s, then resuming automatically.", message, reprise))
		p.statut(fmt.Sprintf("Quota threshold reached (%s), resuming around %s", message, reprise))
		for time.Now().Unix() < reinit+60 {
			if _, err := os.Stat(fStop); err == nil {
				break
			}
			p.dormir(300)
			if p.seuilQuota() == "" {
				break
			}
		}
		p.etatPhase = ""
		p.etatReprise = ""
		if _, err := os.Stat(fStop); err != nil {
			p.log("Quota threshold: resuming.")
		}
		return
	}
	p.etatPhase = "arrete"
	p.terminer(fmt.Sprintf("Quota threshold reached: %s. The pilot stopped (Start to resume).", message))
}

var joursSemaineQuota = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
	"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

func attenteQuotaCalcul(texte string, maintenant time.Time, quotaWaitMin int) int {
	m := reResetsAtPilote.FindStringSubmatch(texte)
	if m != nil {
		jourStr := strings.ToLower(m[1])
		h, errH := strconv.Atoi(m[2])
		min := 0
		var errM error
		if m[3] != "" {
			min, errM = strconv.Atoi(m[3])
		}
		ampm := strings.ToLower(m[4])
		jourValide := jourStr == ""
		var jourCible time.Weekday
		if jourStr != "" {
			jourCible, jourValide = joursSemaineQuota[jourStr]
		}
		heureValide := errH == nil && errM == nil && min >= 0 && min <= 59
		if ampm != "" {
			if h < 1 || h > 12 {
				heureValide = false
			} else {
				if h == 12 {
					h = 0
				}
				if ampm == "pm" {
					h += 12
				}
			}
		} else if h < 0 || h > 23 {
			heureValide = false
		}
		if jourValide && heureValide {
			cible := time.Date(maintenant.Year(), maintenant.Month(), maintenant.Day(), h, min, 0, 0, maintenant.Location())
			if jourStr != "" {
				ecartJours := (int(jourCible) - int(maintenant.Weekday()) + 7) % 7
				cible = cible.AddDate(0, 0, ecartJours)
			}
			nowS, cibleS := maintenant.Unix(), cible.Unix()
			if cibleS <= nowS {
				if nowS-cibleS < 21600 {
					return 120
				}
				if jourStr != "" {
					cibleS += 7 * 86400
				} else {
					cibleS += 86400
				}
			}
			attente := int(cibleS - nowS + 120)
			if attente > 21600 {
				attente = 21600
			}
			if attente < 60 {
				attente = 60
			}
			return attente
		}
	}
	return quotaWaitMin * 60
}

func (p *pilote) nettoyerWorktrees() {
	ws, _ := filepath.Glob(filepath.Join(p.etat, "work", "*"))
	for _, w := range ws {
		if st, err := os.Stat(w); err == nil && st.IsDir() {
			if gitP(p.projet, nil, "worktree", "remove", "--force", w).Run() != nil {
				_ = os.RemoveAll(w)
			}
		}
	}
	_ = gitP(p.projet, nil, "worktree", "prune").Run()
}

func (p *pilote) nettoyerSortie() {
	if p.battementStop != nil {
		close(p.battementStop)
		p.battementStop = nil
	}
	if p.pontCmd != nil && p.pontCmd.Process != nil {
		_ = p.pontCmd.Process.Kill()
	}
	if p.syncCmd != nil && p.syncCmd.Process != nil {
		_ = p.syncCmd.Process.Kill()
	}
	if p.ecranCmd != nil && p.ecranCmd.Process != nil {
		_ = p.ecranCmd.Process.Kill()
	}
	_ = os.Remove(filepath.Join(p.prive, "pilot.pid"))
	_ = os.Remove(filepath.Join(p.etat, "pilot.pid"))
	_ = os.Remove(filepath.Join(p.etat, "pilot.version"))
	_ = os.Remove(filepath.Join(p.prive, "ecran"))
	_ = os.Remove(filepath.Join(p.prive, "eveil"))
	_ = os.Remove(filepath.Join(p.prive, "arreter"))
	if p.verrouFichier != nil {
		p.verrouFichier.Close()
		p.verrouFichier = nil
	}
}

// codeRepliPilote: the Go pilot's exit after an internal error (a panic): pilot.sh carries on in bash.
const codeRepliPilote = 70

// recupererPanique: deferred at the top of the pilot and of its goroutines.
func (p *pilote) recupererPanique() {
	if r := recover(); r != nil {
		p.planter(r, debug.Stack())
	}
}

// planter: a Go error in the pilot. Its report first (logs/pilotes/…/pilote-go.crash), then everything it
// started is stopped (Claude, the workers, their worktrees, the bridge, the Rojo sync, the screen), the lock
// and pilot.pid are released, and the exit code tells pilot.sh to carry on with the bash pilot.
func (p *pilote) planter(r any, pile []byte) {
	if !p.enArret.CompareAndSwap(false, true) {
		select {}
	}
	f := filepath.Join(p.prive, "pilote-go.crash")
	if o, err := os.OpenFile(f, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(o, "%s Athanor %s, cycle %d\npanic: %v\n\n%s\n", time.Now().Format("2006-01-02 15:04:05"), versionAthanor, p.cycle, r, pile)
		o.Close()
	}
	sur := func(f func()) {
		defer func() { _ = recover() }()
		f()
	}
	sur(func() {
		p.log(fmt.Sprintf("The Go pilot hit an internal error (%v); its report: %s. The bash pilot takes over.", r, f))
	})
	journalArret := filepath.Join(p.prive, "arret.log")
	sur(func() {
		arreterSessionClaude(true, filepath.Join(p.prive, "claude_sid"), filepath.Join(p.prive, "claude_winpid"), journalArret)
	})
	sids, _ := filepath.Glob(filepath.Join(p.prive, "ouvrier_*.sid"))
	for _, s := range sids {
		sur(func() { arreterSessionClaude(true, s, strings.TrimSuffix(s, ".sid")+".winpid", journalArret) })
	}
	sur(p.nettoyerWorktrees)
	sur(p.nettoyerSortie)
	os.Exit(codeRepliPilote)
}

func (p *pilote) quitterSurSignal() {
	if !p.enArret.CompareAndSwap(false, true) {
		select {}
	}
	journalArret := filepath.Join(p.prive, "arret.log")
	arreterSessionClaude(false, filepath.Join(p.prive, "claude_sid"), filepath.Join(p.prive, "claude_winpid"), journalArret)
	_ = os.WriteFile(filepath.Join(p.prive, "claude_fin"), nil, 0o644) // a live display reading the log ends
	sids, _ := filepath.Glob(filepath.Join(p.prive, "ouvrier_*.sid"))
	for _, f := range sids {
		arreterSessionClaude(true, f, strings.TrimSuffix(f, ".sid")+".winpid", journalArret)
	}
	p.nettoyerWorktrees()
	p.log("Pilot stopped.")
	p.etatPhase = "arrete"
	p.statut("Stopped (restart with start)")
	p.nettoyerSortie()
	os.Exit(0)
}

func demandeRempliePilote(etat string) bool {
	b, err := os.ReadFile(filepath.Join(etat, "REQUEST.md"))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSuffix(l, "\r")
		if !reLigneDemande.MatchString(l) && reAlnumPilote.MatchString(l) {
			return true
		}
	}
	return false
}

func (p *pilote) butCadrage(fermeture bool, limiteMin string) string {
	if fermeture {
		return fmt.Sprintf("/goal The framing is closed now: .athanor/PLAN.md is written from .athanor/REQUEST.md and the documents already in .athanor/docs (no more reading of attachments, no sub-agent, no code), %s, and .athanor/LOG.md has a new entry. Or stop after 12 turns. %s", formatJalonPilote, p.phraseLangue)
	}
	tb := ""
	if limiteMin != "" {
		tb = fmt.Sprintf(" The pilot gives this cycle %s minutes: write the plan well before that.", limiteMin)
	}
	return fmt.Sprintf("/goal The framing is done according to the framing steps of your instructions: .athanor/PLAN.md exists, %s; .athanor/docs/REQUIREMENTS.md and .athanor/docs/ARCHITECTURE.md exist; .athanor/LOG.md has a new entry. No code, no installation, no demo content in this cycle.%s Or stop after %d turns. %s", formatJalonPilote, tb, p.maxTurnsPerCycle, p.phraseLangue)
}

func (p *pilote) consignesCadrage(fermeture bool, limiteMin string) string {
	if fermeture {
		return "\n## This cycle: closing the framing\n\nThe framing cycle ended before the plan was written. Write `.athanor/PLAN.md` now, yourself, from `.athanor/REQUEST.md` and what is already in `.athanor/docs/`: each milestone one line starting with `- [ ] M1 : `. Do not read the attachments again, do not call any sub-agent, write no code. The missing details become milestones of the plan.\n"
	}
	guide, _ := os.ReadFile(filepath.Join(p.modeles, "guides", "FRAMING.md"))
	if limiteMin != "" {
		return fmt.Sprintf("\n## This cycle: the framing (time box of %s min)\n\nThis cycle only writes the plan, with the steps below: no code, no installation, no test of a tool, no demo content. The pilot stops it after %s minutes and starts the plan as it stands then: have `.athanor/PLAN.md` written within the first half of that time, refine after.\n\n%s", limiteMin, limiteMin, string(guide))
	}
	return fmt.Sprintf("\n## This cycle: the framing\n\nThis cycle only writes the plan, with the steps below: no code, no installation, no test of a tool, no demo content.\n\n%s", string(guide))
}
