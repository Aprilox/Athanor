package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	reEspacesPilote      = regexp.MustCompile(`\s+`)
	reVaguePlanPilote    = regexp.MustCompile(`(?im)^## +Wave|Depends on:`)
	reParalleleMaxPilote = regexp.MustCompile(`^([2-9]|10)$`)
)

func courtPilote(v any, n int) string {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case nil:
		s = "null"
	case json.RawMessage:
		s = string(x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			s = fmt.Sprint(x)
		} else {
			s = string(b)
		}
	}
	s = reEspacesPilote.ReplaceAllString(s, " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// lignesEnDirectPilote: pilot.sh's afficher_en_direct (turns one JSON line of Claude's stream into readable lines).
func lignesEnDirectPilote(ligne string) []string {
	ligne = strings.TrimSpace(ligne)
	if ligne == "" || ligne[0] != '{' {
		return nil
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(ligne))
	dec.UseNumber()
	if dec.Decode(&obj) != nil || obj == nil {
		return nil
	}
	pref := "  "
	if p := obj["parent_tool_use_id"]; p != nil && p != false {
		pref = "      ↳ "
	}
	var out []string
	switch obj["type"] {
	case "assistant":
		msg := dict(obj["message"])
		if msg == nil {
			return nil
		}
		// the inputs as Claude wrote them (jq's tostring keeps the keys in their order; a Go map sorts them)
		var bruts struct {
			Message struct {
				Content []struct {
					Input json.RawMessage `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		_ = json.Unmarshal([]byte(ligne), &bruts)
		for k, raw := range liste(msg["content"]) {
			c := dict(raw)
			if c == nil {
				continue
			}
			var entreeBrute any = c["input"]
			if k < len(bruts.Message.Content) && len(bruts.Message.Content[k].Input) > 0 {
				var b bytes.Buffer
				if json.Compact(&b, bruts.Message.Content[k].Input) == nil && b.Len() > 0 && b.Bytes()[0] == '{' {
					entreeBrute = json.RawMessage(b.Bytes())
				}
			}
			switch c["type"] {
			case "text":
				if t, ok := c["text"].(string); ok && len([]rune(t)) > 0 {
					out = append(out, pref+"💬 "+courtPilote(t, 300))
				}
			case "tool_use":
				nom, _ := c["name"].(string)
				inp := dict(c["input"])
				switch nom {
				case "Bash":
					var cmd any
					if inp != nil {
						cmd = inp["command"]
					}
					out = append(out, pref+"▶ "+courtPilote(cmd, 200))
				case "Edit", "Write", "MultiEdit":
					fp := ""
					if inp != nil && inp["file_path"] != nil && inp["file_path"] != false {
						fp = fmt.Sprint(inp["file_path"])
					}
					out = append(out, pref+"✎ "+nom+" "+fp)
				case "Read":
					fp := ""
					if inp != nil && inp["file_path"] != nil && inp["file_path"] != false {
						fp = fmt.Sprint(inp["file_path"])
					}
					out = append(out, pref+"👁 Read "+fp)
				case "Agent", "Task":
					st := "?"
					var desc any = ""
					if inp != nil {
						if inp["subagent_type"] != nil && inp["subagent_type"] != false {
							st = fmt.Sprint(inp["subagent_type"])
						}
						if inp["description"] != nil && inp["description"] != false {
							desc = inp["description"]
						}
					}
					out = append(out, pref+"👥 Sub-agent "+st+": "+courtPilote(desc, 120))
				default:
					out = append(out, pref+"• "+nom+" "+courtPilote(entreeBrute, 120))
				}
			}
		}
	case "result":
		var r any = ""
		if obj["result"] != nil && obj["result"] != false {
			r = obj["result"]
		} else if obj["subtype"] != nil && obj["subtype"] != false {
			r = obj["subtype"]
		}
		out = append(out, "  ■ End of cycle: "+courtPilote(r, 300))
	}
	return out
}

func checkReussiPilote(etat, journal string, debut int64) bool {
	if b, err := os.ReadFile(journal); err == nil && bytes.Contains(b, []byte("CHECK: PASSED")) {
		return true
	}
	seuil := time.Unix(debut, 0)
	var meilleur string
	var meilleurTemps time.Time
	for _, racine := range []string{filepath.Join(etat, ".verif"), filepath.Join(etat, "check-logs")} {
		_ = filepath.WalkDir(racine, func(chemin string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(racine, chemin)
			prof := 0
			if rel != "." && rel != "" {
				prof = len(strings.Split(filepath.ToSlash(rel), "/"))
			}
			if d.IsDir() {
				if prof >= 2 {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			st, err := d.Info()
			if err != nil || st.Size() >= 20<<20 || st.ModTime().Before(seuil) {
				return nil
			}
			if meilleur == "" || st.ModTime().After(meilleurTemps) {
				meilleur = chemin
				meilleurTemps = st.ModTime()
			}
			return nil
		})
	}
	if meilleur == "" {
		return false
	}
	b, err := os.ReadFile(meilleur)
	if err != nil {
		return false
	}
	derniere := ""
	for _, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "CHECK: ") {
			derniere = l
		}
	}
	return strings.Contains(derniere, "CHECK: PASSED")
}

func produitChangePilote(projet, avant, apres string) bool {
	if avant == apres {
		return false
	}
	if avant == "aucun" {
		return true
	}
	return gitP(projet, nil, "diff", "--quiet", avant, apres).Run() != nil
}

func estScriptShellPilote(chemin string) bool {
	f, err := os.Open(chemin)
	if err != nil {
		return false
	}
	defer f.Close()
	var b [2]byte
	n, _ := f.Read(b[:])
	return n == 2 && b[0] == '#' && b[1] == '!'
}

func cheminWindowsPilote(p string) string {
	p = cheminNatifPilote(p)
	if runtime.GOOS == "windows" {
		return filepath.FromSlash(p)
	}
	return p
}

func cheminMixtePilote(p string) string {
	return filepath.ToSlash(cheminWindowsPilote(p))
}

func (p *pilote) argsNatifs(args []string, prefixe string) []string {
	if !p.natif {
		return args
	}
	var out []string
	k := 0
	for i := 0; i < len(args); {
		o := args[i]
		v := ""
		if i+1 < len(args) {
			v = args[i+1]
		}
		switch o {
		case "--settings", "--mcp-config", "--plugin-dir", "--append-system-prompt-file":
			out = append(out, o, cheminMixtePilote(v))
			i += 2
		case "--append-system-prompt":
			if p.appendFichierOK {
				k++
				f := fmt.Sprintf("%s.consignes%d.md", prefixe, k)
				_ = os.WriteFile(f, []byte(v), 0o644)
				out = append(out, "--append-system-prompt-file", cheminMixtePilote(f))
			} else {
				out = append(out, o, v)
			}
			i += 2
		case "--agents":
			if p.agentsFichierOK {
				f := prefixe + ".agents.json"
				_ = os.WriteFile(f, []byte(v), 0o644)
				out = append(out, "--agents", cheminMixtePilote(f))
			} else {
				out = append(out, o, v)
			}
			i += 2
		default:
			out = append(out, o)
			i++
		}
	}
	return out
}

func (p *pilote) ciblesClaude() []string {
	if p.natif && runtime.GOOS == "windows" && estScriptShellPilote(p.claudeBin) {
		if b := trouverBashNatifPilote(); b != "" {
			return []string{b, filepath.ToSlash(p.claudeBin)}
		}
	}
	if p.natif {
		return []string{cheminWindowsPilote(p.claudeBin)}
	}
	return []string{p.claudeBin}
}

func (p *pilote) progClaude(fWin string) []string {
	cibles := p.ciblesClaude()
	if p.natif {
		exeAthanor := filepath.Join(p.kit, "Athanor.exe")
		if st, err := os.Stat(exeAthanor); err == nil && !st.IsDir() {
			prog := []string{exeAthanor, "--isole", "--fin-tout"}
			if fWin != "" {
				_ = os.Remove(fWin)
				prog = append(prog, "--pid", cheminWindowsPilote(fWin))
			}
			if _, err := os.Stat(filepath.Join(p.kit, "logs", "bureau-isole")); err != nil {
				prog = append(prog, "--ici")
			}
			prog = append(prog, "--")
			prog = append(prog, cibles...)
			return prog
		}
		return cibles
	}
	if x := os.Getenv("ATHANOR_X11_DIR"); x != "" && dossierExistePilote(x) {
		if bwrap, err := exec.LookPath("bwrap"); err == nil {
			a := []string{bwrap, "--dev-bind", "/", "/", "--bind", x, "/tmp/.X11-unix"}
			if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
				l, _ := filepath.Glob(r + "/wayland-*")
				for _, s := range append(l, r+"/bus") {
					if st, err := os.Stat(s); err == nil && st.Mode()&os.ModeSocket != 0 {
						a = append(a, "--ro-bind", "/dev/null", s)
					}
				}
			}
			a = append(a, "--")
			return append(a, cibles...)
		}
	}
	return cibles
}

func (p *pilote) envClaude() []string {
	var out []string
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if strings.EqualFold(k, "PATH") || strings.EqualFold(k, "MSYS2_ARG_CONV_EXCL") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "PATH="+p.cheminClaude)
	if p.natif {
		out = append(out, "MSYS2_ARG_CONV_EXCL=*")
	}
	return out
}

func (p *pilote) gitHead(dossier string) string {
	out, err := gitP(dossier, nil, "rev-parse", "HEAD").Output()
	if err != nil {
		return "aucun"
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "aucun"
	}
	return s
}

func (p *pilote) relaisContexte(journal string) int {
	if p.outilsPython {
		out, _ := exec.Command("python3", "-I", filepath.Join(p.systeme, "relais.py"), "contexte", journal).Output()
		return entierPilote(string(out), 0)
	}
	return int(relaisContexte(journal))
}

func (p *pilote) relaisSur(journal string) bool {
	if p.outilsPython {
		return exec.Command("python3", "-I", filepath.Join(p.systeme, "relais.py"), "sur", journal).Run() == nil
	}
	return relaisSur(journal)
}

func (p *pilote) relaisResume(journal, handoff string) string {
	if p.outilsPython {
		out, _ := exec.Command("python3", "-I", filepath.Join(p.systeme, "relais.py"), "resume", journal, handoff).Output()
		return string(out)
	}
	return relaisResume(journal, handoff)
}

func (p *pilote) lancerClaudeSurveille(journal string, args []string, cadrage bool, limiteCycleMin int, relaisOK bool) int {
	fSid := filepath.Join(p.prive, "claude_sid")
	fCode := filepath.Join(p.prive, "claude_code")
	fWin := filepath.Join(p.prive, "claude_winpid")
	journalArret := filepath.Join(p.prive, "arret.log")
	args = p.argsNatifs(args, filepath.Join(p.prive, "claude"))
	prog := p.progClaude(fWin)
	for _, f := range []string{fSid, fCode, filepath.Join(p.prive, "cycle_arrete"), filepath.Join(p.prive, "cycle_clos"), filepath.Join(p.prive, "cycle_relais")} {
		_ = os.Remove(f)
	}
	tete := p.gitHead(p.projet)
	cochesDebut := p.cochesPlan()
	_ = os.WriteFile(journal, nil, 0o644)

	// Native Windows: Claude writes its stream straight into its log file, never into a pipe (a pipe can
	// stall Claude's write of an image's long line for good); the live display reads the file as it grows.
	direct := p.natif || os.Getenv("JOURNAL_DIRECT") == "1"
	fFin := filepath.Join(p.prive, "claude_fin")
	_ = os.Remove(fFin)
	var pr, pw, jDirect *os.File
	var err error
	if direct {
		if jDirect, err = os.OpenFile(journal, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644); err != nil {
			return 1
		}
	} else if pr, pw, err = os.Pipe(); err != nil {
		return 1
	}
	cmd := exec.Command(prog[0], append(prog[1:], args...)...)
	cmd.Dir = p.projet
	if direct {
		cmd.Stdout, cmd.Stderr = jDirect, jDirect
	} else {
		cmd.Stdout, cmd.Stderr = pw, pw
	}
	cmd.Env = p.envClaude()
	preparerSessionClaude(cmd)
	if err := cmd.Start(); err != nil {
		if direct {
			jDirect.Close()
		} else {
			pw.Close()
			pr.Close()
		}
		assainirAtelier(p.etat)
		return 127
	}
	if direct {
		jDirect.Close()
	} else {
		pw.Close()
	}
	pid := cmd.Process.Pid
	_ = os.WriteFile(fSid, []byte(strconv.Itoa(pid)+"\n"), 0o644)
	if p.natif {
		if _, err := os.Stat(fWin); err != nil {
			_ = os.WriteFile(fWin, []byte(strconv.Itoa(pid)+"\n"), 0o644)
		}
	}

	attendu := make(chan struct{})
	go func() {
		defer p.recupererPanique()
		defer close(attendu)
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() >= 0 {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		_ = os.WriteFile(fCode, []byte(strconv.Itoa(code)+"\n"), 0o644)
	}()

	fluxDone := make(chan struct{})
	affichageFini := make(chan struct{})
	if direct {
		go func() { <-attendu; close(fluxDone) }()
		go func() {
			defer p.recupererPanique()
			defer close(affichageFini)
			suivreJournal(journal, fFin, os.Stdout, 250*time.Millisecond, 6*time.Hour)
		}()
	} else {
		close(affichageFini)
	}
	lu := make(chan struct{})
	if !direct {
		// the stream ends once Claude's output is closed AND Claude's exit code is written: the end of the
		// output alone can come first (the code would be read missing, the cycle taken for an error)
		go func() { <-lu; <-attendu; close(fluxDone) }()
	}
	go func() {
		defer p.recupererPanique()
		if direct {
			return
		}
		defer close(lu)
		defer pr.Close()
		jf, _ := os.OpenFile(journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if jf != nil {
			defer jf.Close()
		}
		br := bufio.NewReaderSize(pr, 64<<10)
		for {
			b, err := br.ReadBytes('\n')
			if len(b) > 0 {
				if jf != nil {
					_, _ = jf.Write(b)
				}
				for _, l := range lignesEnDirectPilote(string(b)) {
					fmt.Fprintln(os.Stdout, l)
				}
			}
			if err != nil {
				break
			}
		}
	}()

	debut := time.Now().Unix()
	var jalonVu, finVu, relaisVu, dernierCtx, dernierQuota, dernierVerrou int64
	motif := ""
	intervalle := time.Duration(max(1, p.watchInterval)) * time.Second

boucleSurveillance:
	for {
		select {
		case <-fluxDone:
			break boucleSurveillance
		case <-p.arretCh:
			p.quitterSurSignal()
		case <-time.After(intervalle):
		}
		select {
		case <-fluxDone:
			break boucleSurveillance
		default:
		}
		maintenant := time.Now().Unix()
		derniere := debut
		if st, err := os.Stat(journal); err == nil {
			derniere = st.ModTime().Unix()
		}
		if st, err := os.Stat(fCode); err == nil && st.Size() > 0 {
			if finVu == 0 {
				finVu = maintenant
			}
		}
		limiteMin := p.cycleMaxMin
		if limiteCycleMin > 0 {
			limiteMin = limiteCycleMin
		}
		if finVu != 0 && maintenant-finVu >= 30 {
			arreterSessionClaude(true, fSid, fWin, journalArret)
			break
		} else if gel := min(p.idleMaxMin, 10); !p.outilsPython && maintenant-derniere >= int64(gel*p.secondsPerMinute) && imageGeleeOK(journal) {
			// Claude Code silent right after receiving an image: it froze on it (large images, athanor images);
			// 10 minutes are enough to tell, a session that works writes something long before
			kb, _ := imageGelee(journal)
			motif = fmt.Sprintf("no activity for %d min, right after an image of %d KB", gel, kb)
		} else if maintenant-derniere >= int64(p.idleMaxMin*p.secondsPerMinute) {
			motif = fmt.Sprintf("no activity for %d min", p.idleMaxMin)
		} else if maintenant-debut >= int64(limiteMin*p.secondsPerMinute) {
			if limiteCycleMin > 0 {
				motif = fmt.Sprintf("framing time box of %d min spent", limiteCycleMin)
			} else {
				motif = fmt.Sprintf("cycle longer than %d min", p.cycleMaxMin)
			}
		} else if p.closeAfterMilestone == "1" {
			if jalonVu == 0 {
				h := p.gitHead(p.projet)
				c := p.cochesPlan()
				if h != tete && c > cochesDebut {
					jalonVu = maintenant
				}
			}
			if jalonVu != 0 {
				ecart := maintenant - jalonVu
				verifie := true
				if fichierRegulierPilote(filepath.Join(p.etat, "check.sh")) && produitChangePilote(p.projet, tete, p.gitHead(p.projet)) && !checkReussiPilote(p.etat, journal, debut) {
					verifie = false
				}
				propre := func() bool {
					return gitP(p.projet, nil, "diff", "--quiet", "--no-ext-diff", "--no-textconv", "HEAD").Run() == nil
				}
				if ecart >= 1200 || (verifie && (ecart >= 600 || (ecart >= 120 && propre()))) {
					motif = "milestone delivered and committed"
					_ = os.WriteFile(filepath.Join(p.prive, "cycle_clos"), nil, 0o644)
				}
			}
		}
		if motif == "" && p.relayContextK > 0 && relaisOK && !cadrage && jalonVu == 0 && maintenant-dernierCtx >= 60 {
			dernierCtx = maintenant
			ctx := p.relaisContexte(journal)
			if ctx >= p.relayContextK*1000 {
				if relaisVu == 0 {
					relaisVu = maintenant
				}
				if p.relaisSur(journal) || maintenant-relaisVu >= 600 {
					res := p.relaisResume(journal, filepath.Join(p.etat, "HANDOFF.md"))
					_ = os.WriteFile(filepath.Join(p.prive, "relais.md"), []byte(res), 0o644)
					_ = os.WriteFile(filepath.Join(p.prive, "relais_k"), []byte(strconv.Itoa(ctx/1000)+"\n"), 0o644)
					_ = os.WriteFile(filepath.Join(p.prive, "cycle_relais"), nil, 0o644)
					motif = fmt.Sprintf("context full (%dk tokens): a fresh session takes over", ctx/1000)
				}
			}
		}
		if (p.robloxMCP || p.unrealMCP || p.blenderMCP || p.mcpProgrammes) && maintenant-dernierVerrou >= 300 {
			dernierVerrou = maintenant
			if processusTourne("LogonUI.exe") {
				if !p.verrouVu {
					p.log("Windows session locked: the programs your MCP servers drive (Roblox Studio, Unreal, Blender…) draw nothing until you unlock it (captures, play tests). Claude goes on with what needs no image.")
				}
				p.verrouVu = true
			} else if p.verrouVu {
				p.verrouVu = false
				p.log("Windows session unlocked: the programs draw again.")
			}
		}
		reglesQuota := filepath.Join(p.kit, "logs", "quota-arret.json")
		if motif == "" && tailleFichierPilote(reglesQuota) > 0 && maintenant-dernierQuota >= 60 {
			dernierQuota = maintenant
			lq := p.seuilQuota()
			if strings.HasPrefix(lq, "maintenant\t") {
				idx := strings.LastIndexByte(lq, '\t')
				motif = "quota threshold reached: " + lq[idx+1:]
			}
		}
		if motif != "" {
			_ = os.WriteFile(filepath.Join(p.prive, "cycle_arrete"), []byte(motif+"\n"), 0o644)
			arreterSessionClaude(false, fSid, fWin, journalArret)
			select {
			case <-fluxDone:
			case <-p.arretCh:
				p.quitterSurSignal()
			case <-time.After(10 * time.Second):
			}
			arreterSessionClaude(true, fSid, fWin, journalArret)
			break
		}
	}

	attente := 0
	for {
		select {
		case <-fluxDone:
			goto fluxTermine
		case <-p.arretCh:
			p.quitterSurSignal()
		case <-time.After(5 * time.Second):
			attente += 5
			if attente%60 == 0 {
				if attente == 60 {
					m := ""
					if motif != "" {
						m = " (" + motif + ")"
					}
					p.log(fmt.Sprintf("Cycle %d: Claude is still running after the stop%s; the pilot keeps stopping it (taskkill output: arret.log in the pilot's folder).", p.cycle, m))
				}
				arreterSessionClaude(true, fSid, fWin, journalArret)
			}
		}
	}
fluxTermine:
	_ = os.WriteFile(fFin, nil, 0o644) // the live display shows the end of the log, then stops
	select {
	case <-affichageFini:
	case <-time.After(3 * time.Second):
	}
	if attente >= 60 {
		p.log(fmt.Sprintf("Cycle %d: Claude stopped after %d min of attempts.", p.cycle, attente/60))
	}
	arreterSessionClaude(true, fSid, fWin, journalArret)
	_ = os.Remove(fWin)
	assainirAtelier(p.etat)
	if b, err := os.ReadFile(fCode); err == nil && strings.TrimSpace(string(b)) != "" {
		return entierPilote(strings.TrimSpace(string(b)), 1)
	}
	return 124
}

func fichierRegulierPilote(chemin string) bool {
	st, err := os.Stat(chemin)
	return err == nil && st.Mode().IsRegular()
}

func dossierExistePilote(chemin string) bool {
	st, err := os.Stat(chemin)
	return err == nil && st.IsDir()
}

func tailleFichierPilote(chemin string) int64 {
	st, err := os.Stat(chemin)
	if err != nil {
		return 0
	}
	return st.Size()
}

func (p *pilote) rangerSolutions(fichier, quoi string) {
	if p.outilsPython {
		_ = exec.Command("bash", "-c", `. "$1/safety.sh"; KIT="$2"; ATHANOR_OUTILS=python; sed -n '/^SOLUTIONS_PY=/,/^fusionner_solutions/p' "$1/pilot.sh" > "$3/sol.sh"; . "$3/sol.sh"; ranger_solutions "$4" "$5"`, "_", p.systeme, p.kit, p.prive, fichier, quoi).Run()
		return
	}
	_ = lancerSolutions([]string{"ranger", fichier, quoi})
}

func (p *pilote) preparerEquipe() {
	dossierAgents := filepath.Join(p.prive, "agents")
	if p.outilsPython {
		out, _ := exec.Command("python3", filepath.Join(p.systeme, "equipe.py"), "importer", p.kit, p.projet).Output()
		for _, l := range strings.Split(string(out), "\n") {
			if l = strings.TrimSpace(l); l != "" {
				p.log(l)
			}
		}
		cmd := exec.Command("python3", filepath.Join(p.systeme, "equipe.py"), "json", p.kit, p.projet, dossierAgents)
		cmd.Env = append(os.Environ(), "SUBAGENT_MODEL="+p.subagentModel, "CHEAP_MODEL="+p.cheapModel)
		jb, _ := cmd.Output()
		p.agentsJSON = strings.TrimSpace(string(jb))
		if p.agentsJSON == "" {
			p.agentsJSON = "{}"
		}
		_ = exec.Command("python3", filepath.Join(p.systeme, "equipe.py"), "fiche", p.kit, p.projet, dossierAgents).Run()
		return
	}
	for _, l := range equipeImporter(p.kit, p.projet) {
		if l != "" {
			p.log(l)
		}
	}
	_ = os.Setenv("SUBAGENT_MODEL", p.subagentModel)
	_ = os.Setenv("CHEAP_MODEL", p.cheapModel)
	p.agentsJSON = strings.TrimSpace(equipeJSON(p.kit, p.projet, dossierAgents, false))
	if p.agentsJSON == "" {
		p.agentsJSON = "{}"
	}
	_ = equipeFiche(p.kit, p.projet, dossierAgents)
}

func (p *pilote) unrealDepuisMCP() {
	p.unrealMCP = false
	b, err := os.ReadFile(filepath.Join(p.prive, "mcp.json"))
	if err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if u, ok := texte(dict(dict(m["mcpServers"])["unreal"])["url"]); ok && u != "" {
				p.unrealMCP = true
				p.unrealURL = u
				if u != p.unrealPrecedent {
					p.log("Unreal MCP: " + u)
				}
				p.unrealPrecedent = u
			}
		}
	}
	if p.unrealMCP {
		_ = os.Setenv("UNREAL_MCP", "1")
		_ = os.Setenv("UNREAL_MCP_URL", p.unrealURL)
	} else {
		_ = os.Setenv("UNREAL_MCP", "0")
	}
}

func (p *pilote) robloxDepuisMCP() {
	p.robloxMCP = false
	b, err := os.ReadFile(filepath.Join(p.prive, "mcp.json"))
	if err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			for k, raw := range dict(m["mcpServers"]) {
				if strings.Contains(strings.ToLower(k), "roblox") {
					p.robloxMCP = true
					break
				}
				var as []string
				for _, a := range liste(dict(raw)["args"]) {
					as = append(as, fmt.Sprint(a))
				}
				if regexp.MustCompile(`(?i)Roblox\\mcp\.bat`).MatchString(strings.Join(as, " ")) {
					p.robloxMCP = true
					break
				}
			}
		}
	}
	if p.robloxMCP {
		_ = os.Setenv("ROBLOX_MCP", "1")
	} else {
		_ = os.Setenv("ROBLOX_MCP", "0")
	}
}

func (p *pilote) blenderDepuisMCP() {
	p.blenderMCP = false
	p.mcpProgrammes = false
	fOuvrir := filepath.Join(p.prive, "ouvrir.json")
	if !p.outilsPython && tailleFichierPilote(fOuvrir) > 0 {
		p.mcpProgrammes = true
	} else {
		_ = os.Remove(fOuvrir)
	}
	geres := map[string]bool{}
	if b, err := os.ReadFile(fOuvrir); err == nil {
		var l []map[string]any
		if json.Unmarshal(b, &l) == nil {
			for _, o := range l {
				if id, ok := o["id"].(string); ok {
					geres[id] = true
				}
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(p.prive, "mcp.json")); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			for k := range dict(m["mcpServers"]) {
				if strings.Contains(strings.ToLower(k), "blender") && !geres[k] {
					p.blenderMCP = true
					break
				}
			}
		}
	}
	if p.blenderMCP {
		_ = os.Setenv("BLENDER_MCP", "1")
	} else {
		_ = os.Setenv("BLENDER_MCP", "0")
	}
	if p.mcpProgrammes {
		_ = os.Setenv("MCP_PROGRAMMES", "1")
	} else {
		_ = os.Setenv("MCP_PROGRAMMES", "0")
	}
}

func (p *pilote) demarrerEveil() {
	eveil := filepath.Join(p.prive, "eveil")
	_ = os.Remove(eveil)
	if p.keepAwake == "0" {
		return
	}
	_ = os.WriteFile(eveil, []byte("system\n"), 0o644)
	exeAthanor := filepath.Join(p.kit, "Athanor.exe")
	if fichierRegulierPilote(exeAthanor) && (p.natif || interopWindows()) {
		argEveil := eveil
		if !p.natif {
			if w := wslpathW(eveil); w != "" {
				argEveil = w
			}
		}
		cmd := exec.Command(exeAthanor, "--eveil", argEveil)
		if !p.natif && dossierExistePilote("/mnt/c") {
			cmd.Dir = "/mnt/c"
		}
		cacherFenetre(cmd)
		_ = cmd.Start()
		if cmd.Process != nil {
			go func() { _ = cmd.Wait() }()
		}
		return
	}
	if _, err := exec.LookPath("systemd-inhibit"); err == nil {
		cmd := exec.Command("systemd-inhibit", "--what=sleep:idle", "--who=Athanor", "--why=Athanor: a pilot is running", "--mode=block", "tail", fmt.Sprintf("--pid=%d", os.Getpid()), "-f", "/dev/null")
		_ = cmd.Start()
		if cmd.Process != nil {
			go func() { _ = cmd.Wait() }()
		}
		return
	}
	_ = os.Remove(eveil)
}

func (p *pilote) modeEveil() {
	eveil := filepath.Join(p.prive, "eveil")
	if !fichierRegulierPilote(eveil) {
		return
	}
	m := "system"
	if p.keepAwake == "screen" || (p.keepAwake == "1" && (p.robloxMCP || p.unrealMCP || p.blenderMCP || p.mcpProgrammes)) {
		m = "screen"
	}
	if lireTexteCourt(eveil) != m {
		_ = os.WriteFile(eveil, []byte(m+"\n"), 0o644)
	}
}

func (p *pilote) preparerExtensions() {
	var r string
	if p.outilsPython {
		out, _ := exec.Command("python3", filepath.Join(p.systeme, "extensions.py"), "preparer", p.kit, p.projet, p.prive).Output()
		r = strings.TrimSpace(string(out))
	} else {
		choix := choixDuProjetKit(p.kit, p.projet)
		_ = os.MkdirAll(p.prive, 0o755)
		skills := construirePlugin(p.kit, choix, p.prive)
		plugins := construirePlugins(p.kit, choix, p.prive)
		mcp, notes := construireMCP(p.kit, choix, filepath.Join(p.prive, "mcp.json"), p.projet, "")
		notes = append(notes, construireOuvertures(p.kit, mcp, filepath.Join(p.prive, "ouvrir.json"), p.projet)...)
		aucun := func(l []string) string {
			if len(l) == 0 {
				return "none"
			}
			return strings.Join(l, ", ")
		}
		r = "skills: " + aucun(skills) + " · plugins: " + aucun(plugins) + " · MCP: " + aucun(mcp)
		if len(notes) > 0 {
			r += " · " + strings.Join(notes, "; ")
		}
	}
	p.pluginAthanor = ""
	p.pluginsDirs = nil
	if fichierRegulierPilote(filepath.Join(p.prive, "plugin", ".claude-plugin", "plugin.json")) {
		if p.pluginDirOK {
			p.pluginAthanor = filepath.Join(p.prive, "plugin")
		} else {
			r += " (skills ignored: this Claude Code version has no --plugin-dir, update it with repair)"
		}
	}
	if entrees, err := os.ReadDir(filepath.Join(p.prive, "plugins")); err == nil {
		for _, e := range entrees {
			d := filepath.Join(p.prive, "plugins", e.Name())
			if !estLien(d) && fichierRegulierPilote(filepath.Join(d, ".claude-plugin", "plugin.json")) && p.pluginDirOK {
				p.pluginsDirs = append(p.pluginsDirs, d)
			}
		}
	}
	if r != "" && r != p.extensionsAvant {
		p.log("Skills & MCP: " + r)
	}
	p.extensionsAvant = r
	p.unrealDepuisMCP()
	p.robloxDepuisMCP()
	p.blenderDepuisMCP()
	p.modeEveil()
}

var ecranVariablesPilote = []string{
	"DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS",
	"XDG_SESSION_TYPE", "GDK_BACKEND", "QT_QPA_PLATFORM", "SDL_VIDEODRIVER", "ELECTRON_OZONE_PLATFORM_HINT",
}

func (p *pilote) arreterEcran() {
	if p.ecranCmd != nil && p.ecranCmd.Process != nil {
		_ = p.ecranCmd.Process.Kill()
		p.ecranCmd = nil
	}
	_ = os.Remove(filepath.Join(p.prive, "ecran"))
	if os.Getenv("ATHANOR_X11_DIR") != "" {
		_ = os.Unsetenv("ATHANOR_X11_DIR")
		for _, v := range ecranVariablesPilote {
			if val, ok := p.ecranAvant[v]; ok {
				_ = os.Setenv(v, val)
			} else {
				_ = os.Unsetenv(v)
			}
		}
	}
}

func (p *pilote) preparerEcran() {
	if p.natif || !fichierRegulierPilote(filepath.Join(p.kit, "logs", "bureau-isole")) || p.ecranImposs {
		p.arreterEcran()
		return
	}
	if _, err := exec.LookPath("Xvfb"); err != nil {
		if !p.ecranSignale {
			p.log("Claude's screen: Xvfb is missing (Setup & health -> Install everything): Linux windows open on your screen.")
			p.ecranSignale = true
		}
		p.arreterEcran()
		return
	}
	if _, err := exec.LookPath("bwrap"); err != nil {
		if !p.ecranSignale {
			p.log("Claude's screen: Xvfb is missing (Setup & health -> Install everything): Linux windows open on your screen.")
			p.ecranSignale = true
		}
		p.arreterEcran()
		return
	}
	vivant := p.ecranCmd != nil && p.ecranCmd.Process != nil && processusVivantPilote(p.ecranCmd.Process.Pid)
	if !vivant {
		p.arreterEcran()
		tmp := os.Getenv("TMPDIR")
		if tmp == "" {
			tmp = "/tmp"
		}
		dir := filepath.Join(tmp, fmt.Sprintf("athanor-ecran-%d-%s", os.Getuid(), filepath.Base(p.prive)))
		_ = os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			p.log("Claude's screen: cannot create " + dir + ".")
			return
		}
		n := 90
		for n = 90; n <= 189; n++ {
			if _, err := os.Lstat(fmt.Sprintf("/tmp/.X%d-lock", n)); os.IsNotExist(err) {
				break
			}
		}
		fLog, _ := os.OpenFile(filepath.Join(p.prive, "ecran.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		cmd := exec.Command("bwrap", "--dev-bind", "/", "/", "--bind", dir, "/tmp/.X11-unix", "--die-with-parent", "--",
			"Xvfb", fmt.Sprintf(":%d", n), "-screen", "0", "1920x1080x24", "-nolisten", "tcp", "-nolisten", "local", "-nolock")
		if fLog != nil {
			cmd.Stdout = fLog
			cmd.Stderr = fLog
		}
		if err := cmd.Start(); err != nil {
			if fLog != nil {
				fLog.Close()
			}
			p.log(fmt.Sprintf("Claude's screen: Xvfb did not start (see %s/ecran.log): Linux windows open on your screen.", p.prive))
			p.ecranImposs = true
			return
		}
		if fLog != nil {
			fLog.Close()
		}
		p.ecranCmd = cmd
		go func() { _ = cmd.Wait() }()
		pret := false
		for i := 0; i < 50; i++ {
			if st, err := os.Stat(filepath.Join(dir, fmt.Sprintf("X%d", n))); err == nil && st.Mode()&os.ModeSocket != 0 {
				pret = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !pret {
			p.log(fmt.Sprintf("Claude's screen: Xvfb did not start (see %s/ecran.log): Linux windows open on your screen.", p.prive))
			_ = cmd.Process.Kill()
			p.ecranCmd = nil
			p.ecranImposs = true
			return
		}
		testCmd := exec.Command("bwrap", "--dev-bind", "/", "/", "--bind", dir, "/tmp/.X11-unix", "--",
			"bwrap", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc", "--unshare-net", "--", "true")
		if testCmd.Run() != nil {
			p.log("Claude's screen: the system sandbox cannot run inside it on this system: Linux windows open on your screen.")
			_ = cmd.Process.Kill()
			p.ecranCmd = nil
			p.ecranImposs = true
			return
		}
		_ = os.WriteFile(filepath.Join(p.prive, "ecran"), []byte(fmt.Sprintf("%d %s\n", n, dir)), 0o644)
		p.log(fmt.Sprintf("Claude's screen ready: Linux windows open there (:%d), never on your screen.", n))
	}
	b, err := os.ReadFile(filepath.Join(p.prive, "ecran"))
	if err != nil {
		return
	}
	champs := strings.Fields(string(b))
	if len(champs) < 2 {
		return
	}
	n, dir := champs[0], champs[1]
	_ = os.Setenv("ATHANOR_X11_DIR", dir)
	_ = os.Setenv("DISPLAY", ":"+n)
	_ = os.Setenv("XDG_SESSION_TYPE", "x11")
	_ = os.Setenv("GDK_BACKEND", "x11")
	_ = os.Setenv("QT_QPA_PLATFORM", "xcb")
	_ = os.Setenv("SDL_VIDEODRIVER", "x11")
	_ = os.Setenv("ELECTRON_OZONE_PLATFORM_HINT", "x11")
	for _, v := range []string{"WAYLAND_DISPLAY", "XAUTHORITY", "DBUS_SESSION_BUS_ADDRESS"} {
		_ = os.Unsetenv(v)
	}
}

func (p *pilote) robloxProjetRojo() string {
	def := filepath.Join(p.projet, "default.project.json")
	if fichierRegulierPilote(def) {
		return def
	}
	var trouve string
	_ = filepath.WalkDir(p.projet, func(chemin string, d fs.DirEntry, err error) error {
		if err != nil || trouve != "" {
			return nil
		}
		rel, _ := filepath.Rel(p.projet, chemin)
		prof := 0
		if rel != "." && rel != "" {
			prof = len(strings.Split(filepath.ToSlash(rel), "/"))
		}
		if d.IsDir() {
			if chemin != p.projet && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "build" || d.Name() == "Packages" || prof >= 2) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() && strings.HasSuffix(d.Name(), ".project.json") {
			trouve = chemin
		}
		return nil
	})
	return trouve
}

func (p *pilote) robloxPlaceStudio(rojoF string) string {
	nom := ""
	if b, err := os.ReadFile(rojoF); err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			if s, ok := m["name"].(string); ok {
				var sb strings.Builder
				for _, r := range s {
					if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
						sb.WriteRune(r)
						if sb.Len() >= 60 {
							break
						}
					}
				}
				nom = sb.String()
			}
		}
	}
	if nom == "" {
		nom = "place"
	}
	return filepath.Join(p.projet, ".athanor", "studio", nom+".rbxl")
}

func (p *pilote) robloxSyncDemarrer() {
	if p.syncCmd != nil && p.syncCmd.Process != nil && processusVivantPilote(p.syncCmd.Process.Pid) {
		return
	}
	fLog, _ := os.OpenFile(filepath.Join(p.prive, "roblox-sync.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	var cmd *exec.Cmd
	if p.outilsPython {
		cmd = exec.Command("python3", filepath.Join(p.systeme, "roblox-sync.py"), "serve", p.projet, filepath.Join(p.prive, "pilot.pid"))
	} else if exe, err := os.Executable(); err == nil {
		cmd = exec.Command(exe, "roblox-sync", "serve", p.projet, filepath.Join(p.prive, "pilot.pid"))
	}
	if cmd != nil {
		if fLog != nil {
			cmd.Stdout = fLog
			cmd.Stderr = fLog
		}
		cacherFenetre(cmd)
		if cmd.Start() == nil {
			p.syncCmd = cmd
			go func() { _ = cmd.Wait() }()
		}
	}
	if fLog != nil {
		fLog.Close()
	}
	if !p.syncAnnonce {
		p.syncAnnonce = true
		p.log("Roblox Studio: Rojo project, the scripts reach the open Studio live (.athanor/studio/SYNC.luau), without restarting it.")
	}
}

func (p *pilote) preparerCycle() {
	exe, errExe := os.Executable()
	if errExe == nil && !p.outilsPython {
		cmd := exec.Command(exe, "archive")
		cmd.Env = append(os.Environ(), "PROJECT="+p.projet)
		cacherFenetre(cmd)
		if out, err := cmd.CombinedOutput(); err == nil && bytes.Contains(out, []byte(" archived")) {
			p.log("Archiving: " + strings.ReplaceAll(strings.TrimSpace(string(out)), "\n", " "))
		}
		cmdMap := exec.Command(exe, "map")
		cmdMap.Env = append(os.Environ(), "PROJECT="+p.projet)
		cacherFenetre(cmdMap)
		_ = cmdMap.Run()
	} else if athanorBin, err := exec.LookPath("athanor"); err == nil {
		cmd := exec.Command(athanorBin, "archive")
		cmd.Env = append(os.Environ(), "PROJECT="+p.projet)
		cacherFenetre(cmd)
		if out, err := cmd.CombinedOutput(); err == nil && bytes.Contains(out, []byte(" archived")) {
			p.log("Archiving: " + strings.ReplaceAll(strings.TrimSpace(string(out)), "\n", " "))
		}
		cmdMap := exec.Command(athanorBin, "map")
		cmdMap.Env = append(os.Environ(), "PROJECT="+p.projet)
		cacherFenetre(cmdMap)
		_ = cmdMap.Run()
	}

	if p.natif || interopWindows() {
		sch := "schtasks.exe"
		if runtime.GOOS == "windows" {
			sch = "schtasks"
		}
		c := exec.Command(sch, "/run", "/tn", "Athanor - installations")
		if runtime.GOOS != "windows" && dossierExistePilote("/mnt/c") {
			c.Dir = "/mnt/c"
		}
		c.Env = append(os.Environ(), "MSYS2_ARG_CONV_EXCL=*")
		cacherFenetre(c)
		_ = c.Run()
	}

	if p.unrealMCP {
		uproject := fichierDuProjet(p.projet, []string{"*.uproject"})
		if uproject != "" {
			url := p.unrealURL
			if url == "" {
				url = "http://127.0.0.1:8000/mcp"
			}
			cl := &http.Client{Timeout: 3 * time.Second}
			resp, err := cl.Get(url)
			if resp != nil {
				resp.Body.Close()
			}
			if err != nil {
				p.log(fmt.Sprintf("Opening the Unreal editor (%s) for the MCP server…", filepath.Base(uproject)))
				c := p.commandePasserelle("unreal-editor", "--wait-mcp", uproject)
				fLog, _ := os.OpenFile(filepath.Join(p.prive, "pilot.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if fLog != nil {
					c.Stdout = fLog
					c.Stderr = fLog
				}
				if c.Run() != nil {
					p.log("Unreal MCP unavailable for this cycle.")
				}
				if fLog != nil {
					fLog.Close()
				}
			}
		}
	}

	if p.robloxMCP {
		place := ""
		if rojoF := p.robloxProjetRojo(); rojoF != "" {
			p.robloxSyncDemarrer()
			place = p.robloxPlaceStudio(rojoF)
			if !processusTourne("RobloxStudioBeta.exe") {
				c := exec.Command("bash", "-c", `. "$1/roblox.sh"; roblox_construire "$2" "$3"`, "_", p.systeme, filepath.ToSlash(rojoF), filepath.ToSlash(place))
				fLog, _ := os.OpenFile(filepath.Join(p.prive, "pilot.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
				if fLog != nil {
					c.Stderr = fLog
				}
				cacherFenetre(c)
				if c.Run() != nil {
					p.log("Roblox Studio: rojo build failed (see the pilot's log), Studio opens on the place built last.")
					if !fichierRegulierPilote(place) {
						place = ""
					}
				}
				if fLog != nil {
					fLog.Close()
				}
			}
		}
		if place == "" {
			place = fichierDuProjet(p.projet, []string{"*.rbxl", "*.rbxlx"})
		}
		if place != "" {
			c := p.commandePasserelle("roblox-studio", "--wait", place)
			out, _ := c.CombinedOutput()
			s := string(out)
			switch {
			case strings.Contains(s, "already open"):
			case strings.Contains(s, "started"):
				p.log(fmt.Sprintf("Roblox Studio opened on %s for its MCP server.", filepath.Base(place)))
			default:
				lignes := strings.Split(strings.TrimSpace(s), "\n")
				derniere := lignes[len(lignes)-1]
				p.log("Roblox Studio unavailable for this cycle: " + tronquerRunes(derniere, 200))
			}
		} else if !p.robloxSansPlace {
			p.robloxSansPlace = true
			p.log("Roblox Studio MCP: no .rbxl/.rbxlx place in the project, the pilot does not open Studio (open it yourself, or let Claude save the place into the project).")
		}
	}

	if p.blenderMCP {
		blend := fichierDuProjet(p.projet, []string{"*.blend"})
		argsB := []string{"--open"}
		if blend != "" {
			argsB = append(argsB, blend)
		}
		c := p.commandePasserelle("blender", argsB...)
		out, _ := c.CombinedOutput()
		s := string(out)
		switch {
		case strings.Contains(s, "already open"):
			p.blenderEchec = ""
		case strings.Contains(s, "started"):
			sur := ""
			if blend != "" {
				sur = " on " + filepath.Base(blend)
			}
			p.log("Blender opened" + sur + " for its MCP server.")
			p.blenderEchec = ""
		default:
			lignes := strings.Split(strings.TrimSpace(s), "\n")
			derniere := tronquerRunes(lignes[len(lignes)-1], 200)
			if derniere != p.blenderEchec {
				p.log("Blender unavailable for this cycle: " + derniere)
			}
			p.blenderEchec = derniere
			return
		}
	}

	if p.mcpProgrammes {
		lignes := ouvrirServeurs(p.kit, p.projet, filepath.Join(p.prive, "ouvrir.json"), time.Sleep)
		var tout strings.Builder
		for _, l := range lignes {
			if l == "" {
				continue
			}
			tout.WriteString(l + "\n")
			if !strings.Contains(p.ouvrirAvant, l) {
				p.log(l)
			}
		}
		p.ouvrirAvant = tout.String()
	}
}

func (p *pilote) commandePasserelle(nom string, args ...string) *exec.Cmd {
	script := filepath.Join(p.systeme, "bin", nom)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		b := trouverBashNatifPilote()
		a := append([]string{filepath.ToSlash(script)}, args...)
		cmd = exec.Command(b, a...)
	} else {
		cmd = exec.Command(script, args...)
	}
	cmd.Env = append(os.Environ(), "PROJECT="+p.projet)
	cacherFenetre(cmd)
	return cmd
}

func (p *pilote) commandeBash(script string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		b := trouverBashNatifPilote()
		a := append([]string{filepath.ToSlash(script)}, args...)
		cmd = exec.Command(b, a...)
	} else {
		cmd = exec.Command("bash", append([]string{script}, args...)...)
	}
	cacherFenetre(cmd)
	return cmd
}

func (p *pilote) ecrireReglages() bool {
	if p.outilsPython {
		return exec.Command("python3", filepath.Join(p.systeme, "settings.py"), p.kit, p.reglages).Run() == nil
	}
	return construireReglages(p.kit, p.reglages) == nil
}

func (p *pilote) appliquerIdentite() {
	var l string
	if !p.natif && !p.outilsPython {
		var buf bytes.Buffer
		// identite-pilote writes "nom\temail\tsans\n"
		cmd := p.commandeBash(filepath.Join(p.systeme, "github.sh"), "identite-pilote", p.projet)
		cmd.Stdout = &buf
		_ = cmd.Run()
		l = strings.TrimRight(buf.String(), "\r\n")
	} else {
		var buf bytes.Buffer
		cmd := p.commandeBash(filepath.Join(p.systeme, "github.sh"), "identite-pilote", filepath.ToSlash(p.projet))
		cmd.Stdout = &buf
		_ = cmd.Run()
		l = strings.TrimRight(buf.String(), "\r\n")
	}
	parts := strings.SplitN(l, "\t", 3)
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	nom, email, sans := parts[0], parts[1], parts[2]
	if nom != "" && email != "" {
		for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
			_ = os.Setenv(k, nom)
		}
		for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
			_ = os.Setenv(k, email)
		}
		_ = gitP(p.projet, nil, "config", "user.name", nom).Run()
		_ = gitP(p.projet, nil, "config", "user.email", email).Run()
	} else {
		for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
			_ = os.Unsetenv(k)
		}
		if gitP(p.projet, nil, "config", "user.name").Run() != nil {
			_ = gitP(p.projet, nil, "config", "user.name", "Athanor").Run()
		}
		if gitP(p.projet, nil, "config", "user.email").Run() != nil {
			_ = gitP(p.projet, nil, "config", "user.email", "athanor@localhost").Run()
		}
	}
	if sans == "1" {
		_ = os.Setenv("ATHANOR_SANS_COAUTEUR", "1")
	} else {
		_ = os.Unsetenv("ATHANOR_SANS_COAUTEUR")
	}
	sansArg := sans
	if sansArg == "" {
		sansArg = "0"
	}
	if !p.outilsPython {
		_ = crochetCommit(p.projet, sansArg == "1")
	} else {
		_ = p.commandeBash(filepath.Join(p.systeme, "github.sh"), "crochet", filepath.ToSlash(p.projet), sansArg).Run()
	}
	if l != p.identitePrecedente {
		if p.reglages != "" && fichierRegulierPilote(p.reglages) {
			p.ecrireReglages()
		}
		if nom != "" {
			co := ""
			if sans == "1" {
				co = ", without Claude as co-author"
			}
			p.log(fmt.Sprintf("Commits by %s <%s>%s.", nom, email, co))
		}
		p.identitePrecedente = l
	}
}

func (p *pilote) githubPousser() int {
	sh := filepath.Join(p.systeme, "github.sh")
	if !fichierRegulierPilote(sh) {
		return 3
	}
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		b := trouverBashNatifPilote()
		if b == "" {
			return 3
		}
		cmd = exec.CommandContext(ctx, b, filepath.ToSlash(sh), "pousser", filepath.ToSlash(p.projet), "--auto")
	} else {
		cmd = exec.CommandContext(ctx, "bash", sh, "pousser", p.projet, "--auto")
	}
	cacherFenetre(cmd)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	if code != 3 {
		lignes := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
		derniere := lignes[len(lignes)-1]
		p.log(tronquerRunes(derniere, 300))
	}
	return code
}

func (p *pilote) lireModeParallele() {
	p.parallel = false
	p.parallelMax = 3
	b, err := os.ReadFile(filepath.Join(p.etat, ".env"))
	if err != nil {
		return
	}
	parStr, maxStr := "0", "3"
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSuffix(l, "\r")
		switch {
		case strings.HasPrefix(l, "PARALLEL="):
			parStr = l[len("PARALLEL="):]
		case strings.HasPrefix(l, "PARALLELE="):
			parStr = l[len("PARALLELE="):]
		case strings.HasPrefix(l, "PARALLEL_MAX="):
			maxStr = l[len("PARALLEL_MAX="):]
		case strings.HasPrefix(l, "PARALLELE_MAX="):
			maxStr = l[len("PARALLELE_MAX="):]
		}
	}
	if parStr == "1" {
		p.parallel = true
	}
	if reParalleleMaxPilote.MatchString(maxStr) {
		p.parallelMax, _ = strconv.Atoi(maxStr)
	}
	if p.parallel {
		out, _ := gitP(p.projet, nil, "ls-files", "--", ".athanor").Output()
		if strings.TrimSpace(string(out)) != "" {
			p.parallel = false
			if !p.paralleleRefusSignale {
				p.log("Parallel mode ignored: .athanor/ is tracked by git. Run 'git rm -r --cached .athanor' in the project, then commit, to enable it.")
				p.paralleleRefusSignale = true
			}
		}
	}
}

func (p *pilote) absorberFileParallele() {
	fs, _ := filepath.Glob(filepath.Join(p.etat, "queue", "*.md"))
	sort.Strings(fs)
	n := 0
	for _, f := range fs {
		if estLien(f) || !fichierRegulierPilote(f) {
			continue
		}
		nom := strings.TrimSuffix(filepath.Base(f), ".md")
		faite := filepath.Join(p.etat, "done", fmt.Sprintf("%s-%s.md", time.Now().Format("20060102-150405"), nom))
		if os.MkdirAll(filepath.Join(p.etat, "done"), 0o755) != nil || os.Rename(f, faite) != nil {
			continue
		}
		b, _ := os.ReadFile(faite)
		ajout := fmt.Sprintf("\n\n## Additional request (%s, %s)\n\n%s\n",
			time.Now().Format("2006-01-02 15:04"), nom, strings.ReplaceAll(string(b), "\r", ""))
		rf, err := os.OpenFile(filepath.Join(p.etat, "REQUEST.md"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err == nil {
			_, _ = rf.WriteString(ajout)
			rf.Close()
		}
		n++
	}
	if n > 0 {
		p.log(fmt.Sprintf("Parallel mode: %d queued request(s) joined the current plan.", n))
	}
}

func (p *pilote) planADesVagues() bool {
	b, err := os.ReadFile(filepath.Join(p.etat, "PLAN.md"))
	return err == nil && reVaguePlanPilote.Match(b)
}

func (p *pilote) jalonsPretsParallele() [][4]string {
	lignes, err := listerPlan(filepath.Join(p.etat, "PLAN.md"))
	if err != nil {
		return nil
	}
	faits := map[string]bool{}
	var tous [][4]string
	for _, l := range lignes {
		parts := strings.SplitN(l, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		if parts[1] == "1" {
			faits[parts[0]] = true
		}
		tous = append(tous, [4]string{parts[0], parts[1], parts[2], parts[3]})
	}
	var prets [][4]string
	for _, j := range tous {
		if j[1] != "0" {
			continue
		}
		ok := true
		for _, d := range strings.Split(j[2], ",") {
			if d != "" && !faits[d] {
				ok = false
				break
			}
		}
		if ok {
			prets = append(prets, j)
		}
	}
	return prets
}

func (p *pilote) quotaRestantParallele() string {
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
	if len(js) > 6 {
		js = js[:6]
	}
	var ev map[string]any
	for _, x := range js {
		for _, o := range lignesJSON(x.f, 0) {
			if o["type"] == "rate_limit_event" {
				ev = o
			}
		}
		if ev != nil {
			break
		}
	}
	if ev == nil {
		return ""
	}
	w := dict(dict(ev["rate_limit_info"])["unifiedWindows"])
	if w == nil {
		return ""
	}
	now := float64(time.Now().Unix())
	verif := func(cle string, seuil float64, label string) string {
		win := dict(w[cle])
		if win == nil {
			return ""
		}
		u, _ := nombreJSON(win["utilization"])
		r, _ := nombreJSON(win["resetsAt"])
		if u >= seuil && r > now {
			return fmt.Sprintf("%s quota at %d%%", label, int(math.Floor(u*100)))
		}
		return ""
	}
	if s := verif("five_hour", 0.75, "5-hour"); s != "" {
		return s
	}
	return verif("seven_day", 0.85, "weekly")
}

func (p *pilote) raisonSerieParallele() string {
	prets := p.jalonsPretsParallele()
	n := len(prets)
	if n == 0 {
		return "the other milestones wait for the next one"
	}
	if n < 3 {
		pl := ""
		if n > 1 {
			pl = "s"
		}
		return fmt.Sprintf("only %d milestone%s can start now", n, pl)
	}
	var textes []string
	for _, j := range prets {
		textes = append(textes, j[3])
	}
	if documentationSeule(strings.Join(textes, "\n")) {
		return "the milestones ready are documentation only"
	}
	return p.quotaRestantParallele()
}

func (p *pilote) branchesPartielles() []string {
	lignes, _ := listerPlan(filepath.Join(p.etat, "PLAN.md"))
	etatJalon := map[string]string{}
	for _, l := range lignes {
		parts := strings.SplitN(l, "\t", 4)
		if len(parts) >= 2 {
			etatJalon[parts[0]] = parts[1]
		}
	}
	out, err := gitP(p.projet, nil, "for-each-ref", "--format=%(refname:short)", "refs/heads/auto/*").Output()
	if err != nil {
		return nil
	}
	var res []string
	for i, br := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if i >= 50 || br == "" {
			break
		}
		id := strings.TrimPrefix(br, "auto/")
		switch etatJalon[id] {
		case "1":
			if gitP(p.projet, nil, "branch", "-D", br).Run() == nil {
				p.log(fmt.Sprintf("Branch %s deleted: its milestone is done.", br))
			}
		case "0":
			cntOut, _ := gitP(p.projet, nil, "rev-list", "--count", "HEAD.."+br).Output()
			if entierPilote(string(cntOut), 0) > 0 {
				res = append(res, id)
			}
		}
	}
	return res
}

const consignesParallelePilote = `
## Parallel mode (active for this project)

Workers build milestones **at the same time**, each in its own git worktree and branch ` + "`auto/<milestone>`" + `; the pilot merges each one as soon as it finishes and immediately starts the next milestone whose dependencies are met. Your job as the lead is therefore a **precise, fine-grained plan** with explicit dependencies: the better it is cut, the more work runs in parallel and the simpler the merges.

Plan rules (` + "`.athanor/PLAN.md`" + `):
- One milestone = one worker session = a small, self-contained piece of work (one page, one module, one feature with its tests), doable alone by someone who only knows the documents, the skeleton and the milestone text. Prefer many small milestones (10 to 25) to a few big ones.
- Every milestone line ends with ` + "`Files: <files it creates or modifies>. Depends on: <milestone ids, or none>. Acceptance: <verifiable criteria>. Read: <the document sections the worker needs>.`" + ` Example: ` + "`- [ ] M7 : Contact form with client-side validation — Files: contact.html, css/contact.css, js/contact.js, tests/e2e/test_contact.py. Depends on: M1. Acceptance: empty submit shows 5 errors; valid submit shows \"Thank you\"; check.sh quick contact passes. Read: DESIGN.md §Contact.`" + ` A worker reads only its line and what ` + "`Read`" + ` names: everything it needs is there.
- **M1 is always the foundation**: the runnable skeleton, ` + "`check.sh`" + `, and the **contracts** everything else relies on (data structures, interfaces, conventions, shared stylesheet, header/footer, test helpers). Everything shared is built before it is parallelised; milestones that only depend on M1 can all run at once.
- Two milestones that modify the same file must depend on one another (one after the other). A shared file discovered later is changed minimally and flagged in the worker's report.
- Milestones that test, review or polish the work of others (cross-cutting tests, tester/critic passes, polish, documentation, delivery) depend on everything they cover; the final delivery milestone depends on all the others.
- ` + "`## Wave N`" + ` headings are still accepted (a milestone without ` + "`Depends on:`" + ` depends on every milestone of the earlier waves), but explicit dependencies are preferred: they let the pilot start work earlier.
- **Additional requests** (sections "Additional request" of REQUEST.md, flagged by ` + "`.athanor/REQUEST_CHANGED`" + `): turn each into milestones with their dependencies and append them to the plan; keep a single final delivery milestone at the very end that depends on everything.
If the plan already exists without ` + "`Depends on:`" + ` nor waves, add the dependencies without changing the milestones' content (commit), and stop there for this cycle.
`

func trouverBashNatifPilote() string {
	existe := func(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }
	if p := os.Getenv("CLAUDE_CODE_GIT_BASH_PATH"); p != "" && existe(p) {
		return p
	}
	var candidats []string
	for _, v := range []string{"ProgramW6432", "ProgramFiles", "ProgramFiles(x86)"} {
		if d := os.Getenv(v); d != "" {
			candidats = append(candidats, filepath.Join(d, "Git", "bin", "bash.exe"), filepath.Join(d, "Git", "usr", "bin", "bash.exe"))
		}
	}
	if d := os.Getenv("LocalAppData"); d != "" {
		candidats = append(candidats, filepath.Join(d, "Programs", "Git", "bin", "bash.exe"))
	}
	if g, err := exec.LookPath("git.exe"); err == nil {
		racine := filepath.Dir(filepath.Dir(g))
		candidats = append(candidats, filepath.Join(racine, "bin", "bash.exe"))
	}
	if b, err := exec.LookPath("bash.exe"); err == nil {
		candidats = append(candidats, b)
	}
	for _, c := range candidats {
		if existe(c) {
			return c
		}
	}
	return ""
}

func cheminNatifPilote(s string) string {
	if runtime.GOOS != "windows" || s == "" {
		return s
	}
	if len(s) >= 2 && s[0] == '/' && estLettre(s[1]) && (len(s) == 2 || s[2] == '/') {
		reste := ""
		if len(s) > 2 {
			reste = filepath.FromSlash(s[2:])
		} else {
			reste = `\`
		}
		return strings.ToUpper(s[1:2]) + ":" + reste
	}
	if strings.HasPrefix(s, "/") {
		if _, err := os.Stat(s); err != nil {
			if b := trouverBashNatifPilote(); b != "" {
				cyg := filepath.Join(filepath.Dir(b), "cygpath.exe")
				if !fichierRegulierPilote(cyg) {
					cyg = filepath.Join(filepath.Dir(filepath.Dir(b)), "usr", "bin", "cygpath.exe")
				}
				if fichierRegulierPilote(cyg) {
					if out, err := exec.Command(cyg, "-w", s).Output(); err == nil {
						if w := strings.TrimSpace(string(out)); w != "" {
							return w
						}
					}
				}
			}
		}
	}
	return s
}

func migrerAtelierPilote(projet string) {
	a := filepath.Join(projet, ".autopilote")
	n := filepath.Join(projet, ".athanor")
	if st, err := os.Lstat(a); err != nil || !st.IsDir() {
		return
	}
	if st, err := os.Stat(filepath.Join(a, "pilot.pid")); err == nil && time.Since(st.ModTime()) < 90*time.Second {
		return
	}
	if st, err := os.Lstat(n); err == nil {
		if !st.IsDir() || estLien(filepath.Join(n, "REQUEST.md")) || fichierRegulierPilote(filepath.Join(n, "REQUEST.md")) ||
			estLien(filepath.Join(n, "PLAN.md")) || fichierRegulierPilote(filepath.Join(n, "PLAN.md")) {
			return
		}
		entrees, _ := os.ReadDir(n)
		for _, e := range entrees {
			src := filepath.Join(n, e.Name())
			dst := filepath.Join(a, e.Name())
			if !estLien(src) {
				if _, err := os.Lstat(dst); os.IsNotExist(err) {
					_ = os.Rename(src, dst)
				}
			}
		}
		_ = os.RemoveAll(n)
	}
	if os.Rename(a, n) != nil {
		return
	}
	excl := filepath.Join(projet, ".git", "info", "exclude")
	if !estLien(excl) && fichierRegulierPilote(excl) {
		if b, err := os.ReadFile(excl); err == nil {
			nb := regexp.MustCompile(`(?m)^\.autopilote/$`).ReplaceAll(b, []byte(".athanor/"))
			_ = os.WriteFile(excl, nb, 0o644)
		}
	}
	if dossierExistePilote(filepath.Join(n, "work")) {
		ws, _ := filepath.Glob(filepath.Join(n, "work", "*"))
		if len(ws) > 0 {
			_ = gitP(projet, nil, append([]string{"worktree", "repair"}, ws...)...).Run()
		}
		_ = gitP(projet, nil, "worktree", "prune").Run()
	}
}

func versionPlusRecenteOuEgale(v, minV string) bool {
	p1 := strings.Split(v, ".")
	p2 := strings.Split(minV, ".")
	for i := 0; i < 3; i++ {
		var a, b int
		if i < len(p1) {
			a, _ = strconv.Atoi(p1[i])
		}
		if i < len(p2) {
			b, _ = strconv.Atoi(p2[i])
		}
		if a != b {
			return a > b
		}
	}
	return true
}

func trouverClaudePilote(natif bool) string {
	if c := os.Getenv("CLAUDE_BIN"); c != "" {
		return cheminNatifPilote(c)
	}
	if c, err := exec.LookPath("claude"); err == nil {
		return c
	}
	if natif {
		if c, err := exec.LookPath("claude.exe"); err == nil {
			return c
		}
	}
	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home != "" {
		for _, nom := range []string{"claude", "claude.exe"} {
			cand := filepath.Join(home, ".local", "bin", nom)
			if fichierRegulierPilote(cand) {
				return cand
			}
		}
	}
	return ""
}

func lancerPilote(args []string) int {
	ignorerSIGPIPE()
	natif := runtime.GOOS == "windows" || os.Getenv("ATHANOR_NATIF") == "1"
	kit := os.Getenv("KIT")
	if kit != "" {
		kit = cheminNatifPilote(kit)
	} else if exe, err := os.Executable(); err == nil {
		kit = filepath.Dir(exe)
		if !fichierRegulierPilote(filepath.Join(kit, "system", "pilot.sh")) {
			if wd, err := os.Getwd(); err == nil && fichierRegulierPilote(filepath.Join(wd, "system", "pilot.sh")) {
				kit = wd
			}
		}
	}
	kit = abspathOS(kit)
	systeme := filepath.Join(kit, "system")
	tools := filepath.Join(kit, "tools")

	projArg := ""
	if len(args) > 0 && args[0] != "" {
		projArg = args[0]
	} else if envP := os.Getenv("PROJECT"); envP != "" {
		projArg = envP
	} else {
		projArg = filepath.Join(kit, "projet")
	}
	projArg = cheminNatifPilote(projArg)
	_ = os.MkdirAll(projArg, 0o755)
	projet := abspathOS(projArg)
	etat := filepath.Join(projet, ".athanor")

	migrerAtelierPilote(projet)
	_ = os.Remove(filepath.Join(etat, "verification.json"))
	prive := priveDePilote(kit, projet, natif)
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN"} {
		_ = os.Unsetenv(k)
	}
	_ = os.MkdirAll(prive, 0o755)
	reglages := filepath.Join(prive, "claude_settings.json")
	modeles := filepath.Join(prive, "template")
	_ = os.RemoveAll(modeles)
	copierDossierModeles(filepath.Join(kit, "template"), modeles)
	localTpl := filepath.Join(kit, "local", "template")
	if !estLien(localTpl) && dossierExistePilote(localTpl) {
		copierDossierModeles(localTpl, modeles)
	}
	assainirAtelier(etat)

	if b, err := os.ReadFile(filepath.Join(kit, ".env")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSuffix(l, "\r")
			if m := reCleEnvPilote.FindStringSubmatch(l); m != nil {
				if _, ok := os.LookupEnv(m[1]); !ok {
					_ = os.Setenv(m[1], m[2])
				}
			}
		}
	}
	for _, paire := range [][2]string{
		{"MODELE", "MODEL"}, {"MODELE_SECOURS", "FALLBACK_MODEL"}, {"MODELE_SOUS_AGENTS", "SUBAGENT_MODEL"},
		{"MODELE_TACHES_SIMPLES", "CHEAP_MODEL"}, {"LANGUE", "LANGUAGE"}, {"DUREE_MAX_HEURES", "MAX_HOURS"},
		{"PAUSE_QUOTA_MIN", "QUOTA_WAIT_MIN"}, {"INACTIVITE_MAX_MIN", "IDLE_MAX_MIN"}, {"CLORE_APRES_JALON", "CLOSE_AFTER_MILESTONE"},
	} {
		if v, ok := os.LookupEnv(paire[0]); ok {
			if _, okNew := os.LookupEnv(paire[1]); !okNew {
				_ = os.Setenv(paire[1], v)
			}
		}
	}
	_ = os.Setenv("TOOLS", tools)
	_ = os.Setenv("PROJECT", projet)
	_ = os.Setenv("KIT", kit)

	home := os.Getenv("HOME")
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	sep := string(os.PathListSeparator)
	pathActuel := os.Getenv("PATH")
	nouveauPath := filepath.Join(systeme, "bin") + sep + filepath.Join(home, ".local", "bin") + sep + pathActuel
	if strings.HasPrefix(pathActuel, filepath.Join(systeme, "bin")+sep) {
		nouveauPath = pathActuel // started by pilot.sh, which put them there already
	}
	_ = os.Setenv("PATH", nouveauPath)
	claudeBin := trouverClaudePilote(natif)
	cheminClaude := nouveauPath + sep + filepath.Join(tools, "bin")
	if natif {
		var parts []string
		natifDir := filepath.Clean(filepath.Join(systeme, "natif"))
		for _, d := range filepath.SplitList(cheminClaude) {
			if !strings.EqualFold(filepath.Clean(d), natifDir) && filepath.ToSlash(d) != filepath.ToSlash(natifDir) {
				parts = append(parts, d)
			}
		}
		cheminClaude = strings.Join(parts, sep)
		if os.Getenv("CLAUDE_CODE_GIT_BASH_PATH") == "" {
			if b := trouverBashNatifPilote(); b != "" {
				_ = os.Setenv("CLAUDE_CODE_GIT_BASH_PATH", b)
			}
		}
	}
	_ = os.MkdirAll(filepath.Join(etat, "logs"), 0o755)
	_ = os.MkdirAll(filepath.Join(tools, "bin"), 0o755)
	_ = os.MkdirAll(filepath.Join(tools, "cache"), 0o755)

	defEnv := func(cle, def string) string {
		if v := os.Getenv(cle); v != "" {
			return v
		}
		return def
	}
	defEnvVide := func(cle, def string) string {
		if v, ok := os.LookupEnv(cle); ok {
			return v
		}
		return def
	}
	keepAwake := defEnv("KEEP_AWAKE", "1")
	switch keepAwake {
	case "0", "1", "screen":
	default:
		fmt.Fprintf(os.Stderr, "Invalid setting KEEP_AWAKE (%q): using default 1.\n", keepAwake)
		keepAwake = "1"
	}
	entierReglage := func(cle string, def int) int {
		v := defEnv(cle, strconv.Itoa(def))
		if !reEntierPilote.MatchString(v) {
			fmt.Fprintf(os.Stderr, "Invalid setting %s (%q): using default %d.\n", cle, v, def)
			_ = os.Setenv(cle, strconv.Itoa(def))
			return def
		}
		n, _ := strconv.Atoi(v)
		return n
	}

	p := &pilote{
		kit:                      kit,
		systeme:                  systeme,
		tools:                    tools,
		projet:                   projet,
		etat:                     etat,
		prive:                    prive,
		reglages:                 reglages,
		modeles:                  modeles,
		natif:                    natif,
		outilsPython:             os.Getenv("ATHANOR_OUTILS") == "python",
		claudeBin:                claudeBin,
		cheminClaude:             cheminClaude,
		maxHours:                 entierReglage("MAX_HOURS", 168),
		quotaWaitMin:             entierReglage("QUOTA_WAIT_MIN", 20),
		errorWaitMin:             entierReglage("ERROR_WAIT_MIN", 2),
		errorWaitMaxMin:          entierReglage("ERROR_WAIT_MAX_MIN", 60),
		loginWaitMin:             entierReglage("LOGIN_WAIT_MIN", 30),
		maxCyclesWithoutProgress: entierReglage("MAX_CYCLES_WITHOUT_PROGRESS", 3),
		maxTurnsPerCycle:         entierReglage("MAX_TURNS_PER_CYCLE", 30),
		idleMaxMin:               entierReglage("IDLE_MAX_MIN", 30),
		cycleMaxMin:              entierReglage("CYCLE_MAX_MIN", 180),
		watchInterval:            entierReglage("WATCH_INTERVAL", 30),
		secondsPerMinute:         entierReglage("SECONDS_PER_MINUTE", 60),
		framingMaxMin:            entierReglage("FRAMING_MAX_MIN", 25),
		relayContextK:            entierReglage("RELAY_CONTEXT_K", 150),
		closeAfterMilestone:      defEnv("CLOSE_AFTER_MILESTONE", "1"),
		stayAliveAtEnd:           defEnv("STAY_ALIVE_AT_END", "0"),
		keepAwake:                keepAwake,
		model:                    os.Getenv("MODEL"),
		fallbackModel:            os.Getenv("FALLBACK_MODEL"),
		effort:                   os.Getenv("EFFORT"),
		subagentModel:            os.Getenv("SUBAGENT_MODEL"),
		framingModel:             os.Getenv("FRAMING_MODEL"),
		workerModel:              defEnvVide("WORKER_MODEL", "sonnet"),
		cheapModel:               defEnvVide("CHEAP_MODEL", "haiku"),
		language:                 defEnv("LANGUAGE", "en"),
		ecranAvant:               map[string]string{},
		arretCh:                  make(chan struct{}),
		battementStop:            make(chan struct{}),
	}
	for _, v := range ecranVariablesPilote {
		if val, ok := os.LookupEnv(v); ok {
			p.ecranAvant[v] = val
		}
	}
	_ = os.Setenv("CLAUDE_CODE_DISABLE_BACKGROUND_TASKS", defEnv("CLAUDE_CODE_DISABLE_BACKGROUND_TASKS", "1"))
	_ = os.Setenv("ENABLE_CLAUDEAI_MCP_SERVERS", "false")
	_ = os.Setenv("BASH_MAX_OUTPUT_LENGTH", defEnv("BASH_MAX_OUTPUT_LENGTH", "20000"))
	if p.effort != "" {
		_ = os.Setenv("CLAUDE_CODE_EFFORT_LEVEL", p.effort)
	}
	p.configClaude = os.Getenv("CLAUDE_CONFIG_DIR")
	if p.configClaude == "" {
		p.configClaude = filepath.Join(home, ".claude")
	}

	p.nettoyerWorktrees()

	if os.Chdir(p.projet) != nil {
		fmt.Fprintf(os.Stderr, "Folder %s not accessible\n", p.projet)
		return 1
	}
	if p.natif {
		ps := filepath.ToSlash(p.projet)
		okDrive := (len(ps) >= 2 && ps[1] == ':') || (len(ps) >= 2 && ps[0] == '/' && estLettre(ps[1]) && (len(ps) == 2 || ps[2] == '/'))
		if !okDrive {
			fmt.Printf("Native Windows mode: the project must be in a folder of a Windows drive (C:\\..., D:\\...), not %s. Move it, or use the WSL mode.\n", p.projet)
			return 1
		}
	}
	for _, d := range []string{filepath.Join(p.etat, "logs"), filepath.Join(p.etat, "queue"), filepath.Join(p.etat, "done")} {
		_ = os.MkdirAll(d, 0o755)
	}

	vf, ok := verrouPilote(filepath.Join(p.prive, "verrou"))
	if !ok {
		fmt.Println("The pilot is already running on this project.")
		return 0
	}
	p.verrouFichier = vf
	defer p.nettoyerSortie()
	// a Go error from here on: the sessions stopped, the report kept, and pilot.sh carries on in bash
	defer p.recupererPanique()
	if f, err := os.OpenFile(filepath.Join(p.prive, "pilote-go.crash"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{}) // the fatal errors no recover catches (pilot.sh removes it empty)
		f.Close()
	}

	_ = os.Remove(filepath.Join(p.prive, "arreter"))
	pidBytes := []byte(strconv.Itoa(os.Getpid()) + "\n")
	_ = os.WriteFile(filepath.Join(p.prive, "pilot.pid"), pidBytes, 0o644)
	_ = publierFichier(filepath.Join(p.etat, "pilot.pid"), pidBytes)
	_ = os.Setenv("JOURNAL_ARRET", filepath.Join(p.prive, "arret.log"))

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, signauxArretPilote()...)
	go func() {
		select {
		case <-sigCh:
			p.arretOnce.Do(func() { close(p.arretCh) })
			p.quitterSurSignal()
		case <-p.arretCh:
			p.quitterSurSignal()
		}
	}()

	go func() {
		defer p.recupererPanique()
		tBattement := time.NewTicker(20 * time.Second)
		tArret := time.NewTicker(200 * time.Millisecond)
		defer tBattement.Stop()
		defer tArret.Stop()
		eveil := filepath.Join(p.prive, "eveil")
		fArret := filepath.Join(p.prive, "arreter")
		for {
			select {
			case <-p.battementStop:
				return
			case <-tArret.C:
				if _, err := os.Stat(fArret); err == nil {
					_ = os.Remove(fArret)
					p.arretOnce.Do(func() { close(p.arretCh) })
					return
				}
			case <-tBattement.C:
				if !fichierRegulierPilote(filepath.Join(p.prive, "pilot.pid")) {
					return
				}
				_ = publierFichier(filepath.Join(p.etat, "pilot.pid"), pidBytes)
				if fichierRegulierPilote(eveil) {
					now := time.Now()
					_ = os.Chtimes(eveil, now, now)
				}
			}
		}
	}()

	p.demarrerEveil()

	if p.claudeBin == "" || !fichierRegulierPilote(p.claudeBin) {
		p.log("Claude Code not found: run install.sh first.")
		return 1
	}
	for _, f := range []string{"pilot.log", "stats.tsv"} {
		dst := filepath.Join(p.prive, f)
		src := filepath.Join(p.etat, f)
		if !fichierRegulierPilote(dst) && !estLien(src) && fichierRegulierPilote(src) {
			if b, err := os.ReadFile(src); err == nil {
				_ = os.WriteFile(dst, b, 0o644)
			}
		}
	}

	p.log("=== Athanor starting ===")
	if runtime.GOOS == "windows" && !consolePartagee {
		p.log("Windows: no console to share with the programs the pilot starts (git, the tools): each may flash a window of its own.")
	}
	if p.outilsPython {
		p.log("Helpers: Python scripts.")
	} else {
		nomExe := "athanor"
		if exe, err := os.Executable(); err == nil {
			nomExe = filepath.Base(exe)
		}
		p.log(fmt.Sprintf("Helpers: Go (%s %s).", nomExe, versionAthanor))
	}

	dejaSignale := false
	for !demandeRempliePilote(p.etat) {
		if !dejaSignale {
			p.log("No request: write it in the application (or in .athanor/REQUEST.md), Claude will start on its own.")
			p.statut("Waiting for your request in REQUEST.md")
			dejaSignale = true
		}
		p.dormir(60)
	}

	p.consignes = filepath.Join(p.prive, "CLAUDE.md")
	var cb bytes.Buffer
	if b, err := os.ReadFile(filepath.Join(p.modeles, "CLAUDE.md")); err == nil {
		cb.Write(b)
	}
	if strings.EqualFold(p.subagentModel, "auto") {
		if b, err := os.ReadFile(filepath.Join(p.modeles, "guides", "SUBAGENT_MODELS.md")); err == nil {
			cb.Write(b)
		}
	}
	if p.natif {
		if b, err := os.ReadFile(filepath.Join(p.modeles, "guides", "NATIVE.md")); err == nil {
			cb.Write(b)
		}
	}
	enteteLangue := "\n## Language\n\n"
	switch p.language {
	case "fr":
		p.phraseLangue = "Écris tous tes messages en français (ils s'affichent dans le journal de l'application), ainsi que les documents, les fichiers .athanor et les consignes des sous-agents."
		enteteLangue = "\n## Langue\n\n"
	case "es":
		p.phraseLangue = "Escribe todos tus mensajes en español (se muestran en el registro de la aplicación), así como los documentos, los archivos .athanor y las instrucciones de los subagentes."
		enteteLangue = "\n## Idioma\n\n"
	case "de":
		p.phraseLangue = "Schreibe alle deine Nachrichten auf Deutsch (sie erscheinen im Protokoll der Anwendung), ebenso die Dokumente, die .athanor-Dateien und die Anweisungen an die Sub-Agenten."
		enteteLangue = "\n## Sprache\n\n"
	case "it":
		p.phraseLangue = "Scrivi tutti i tuoi messaggi in italiano (compaiono nel registro dell'applicazione), così come i documenti, i file .athanor e le istruzioni dei sub-agenti."
		enteteLangue = "\n## Lingua\n\n"
	case "pt":
		p.phraseLangue = "Escreve todas as tuas mensagens em português (aparecem no registo da aplicação), assim como os documentos, os ficheiros .athanor e as instruções dos subagentes."
		enteteLangue = "\n## Idioma\n\n"
	default:
		p.phraseLangue = "Write all your messages in English (they show in the application's log), as well as the documents, the .athanor files and the sub-agents' briefs."
	}
	cb.WriteString(enteteLangue + p.phraseLangue + "\n")
	_ = os.WriteFile(p.consignes, cb.Bytes(), 0o644)

	if b, err := os.ReadFile(filepath.Join(p.modeles, "PC_TOOLS_GUIDE.md")); err == nil {
		_ = os.WriteFile(filepath.Join(p.etat, "PC_TOOLS_GUIDE.md"), b, 0o644)
	}
	_ = os.MkdirAll(filepath.Join(p.etat, "guides"), 0o755)
	if gs, err := filepath.Glob(filepath.Join(p.modeles, "guides", "*.md")); err == nil {
		for _, g := range gs {
			if b, err := os.ReadFile(g); err == nil {
				_ = os.WriteFile(filepath.Join(p.etat, "guides", filepath.Base(g)), b, 0o644)
			}
		}
	}
	for _, f := range []string{"CLAUDE.md", "CLAUDE.cycle.md", "mcp.json", "claude_settings.json"} {
		_ = os.RemoveAll(filepath.Join(p.etat, f))
	}
	dossierAgents := filepath.Join(p.prive, "agents")
	_ = os.RemoveAll(dossierAgents)
	_ = os.MkdirAll(dossierAgents, 0o755)
	if as, err := filepath.Glob(filepath.Join(p.modeles, ".claude", "agents", "*.md")); err == nil {
		for _, a := range as {
			if b, err := os.ReadFile(a); err == nil {
				_ = os.WriteFile(filepath.Join(dossierAgents, filepath.Base(a)), b, 0o644)
			}
		}
	}
	for _, f := range []string{"architect", "creator", "developer", "tester", "critic", "executor", "scout"} {
		_ = os.Remove(filepath.Join(p.etat, "agents", f+".md"))
	}
	p.preparerEquipe()

	cibles := p.ciblesClaude()
	cmdHelp := exec.Command(cibles[0], append(cibles[1:], "--help")...)
	cacherFenetre(cmdHelp)
	aideClaudeBytes, _ := cmdHelp.Output()
	aideClaude := string(aideClaudeBytes)
	if strings.Contains(aideClaude, "--strict-mcp-config") {
		p.argsSurs = append(p.argsSurs, "--strict-mcp-config")
	}
	if strings.Contains(aideClaude, "--setting-sources") {
		p.argsSurs = append(p.argsSurs, "--setting-sources", "user")
	}
	if strings.Contains(aideClaude, "--append-system-prompt-file") {
		p.appendFichierOK = true
	}
	cmdVer := exec.Command(cibles[0], append(cibles[1:], "--version")...)
	cacherFenetre(cmdVer)
	verBytes, _ := cmdVer.Output()
	vClaude := reVersionClaude.FindString(string(verBytes))
	if vClaude != "" && versionPlusRecenteOuEgale(vClaude, "2.1.281") {
		p.agentsFichierOK = true
		p.appendFichierOK = true
	}
	if p.natif && !p.appendFichierOK {
		vc := vClaude
		if vc == "" {
			vc = "?"
		}
		p.log(fmt.Sprintf("Native Windows mode: Claude Code %s cannot read its instructions from a file; update it (Setup & health -> Install everything), long instructions may not fit on a Windows command line.", vc))
	}
	if strings.Contains(aideClaude, "--agents") {
		p.agentsParJSON = true
	} else {
		p.agentsParJSON = false
		_ = os.MkdirAll(filepath.Join(p.projet, ".claude", "agents"), 0o755)
		if as, err := filepath.Glob(filepath.Join(dossierAgents, "*.md")); err == nil {
			for _, a := range as {
				if b, err := os.ReadFile(a); err == nil {
					_ = os.WriteFile(filepath.Join(p.projet, ".claude", "agents", filepath.Base(a)), b, 0o644)
				}
			}
		}
	}
	if strings.Contains(aideClaude, "--plugin-dir") {
		p.pluginDirOK = true
	}
	p.synchroniserDemande()

	if p.outilsPython {
		_ = p.commandeBash(filepath.Join(p.systeme, "detect-tools.sh")).Run()
	} else {
		_ = lancerDetecter([]string{p.kit})
	}
	_ = os.Setenv("ATHANOR_PLUGIN", filepath.Join(p.prive, "plugin"))
	_ = os.Setenv("ATHANOR_PLUGINS", filepath.Join(p.prive, "plugins"))
	if !p.ecrireReglages() {
		p.log("Cannot write Claude's settings.")
		return 1
	}

	p.preparerExtensions()
	p.preparerEcran()

	if !p.natif {
		fPont, _ := os.OpenFile(filepath.Join(p.prive, "pont.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		var cmdPont *exec.Cmd
		if p.outilsPython {
			cmdPont = exec.Command("python3", filepath.Join(p.systeme, "bridge.py"), p.kit, p.projet)
		} else if exe, err := os.Executable(); err == nil {
			cmdPont = exec.Command(exe, "pont", p.kit, p.projet)
		}
		if cmdPont != nil {
			if fPont != nil {
				cmdPont.Stdout = fPont
				cmdPont.Stderr = fPont
			}
			if cmdPont.Start() == nil {
				p.pontCmd = cmdPont
				go func() { _ = cmdPont.Wait() }()
			}
		}
		if fPont != nil {
			fPont.Close()
		}
	}

	if !dossierExistePilote(filepath.Join(p.projet, ".git")) {
		_ = gitP(p.projet, nil, "init", "-q", "-b", "main").Run()
	}
	if p.natif {
		_ = gitP(p.projet, nil, "config", "core.autocrlf", "false").Run()
		_ = gitP(p.projet, nil, "config", "core.fileMode", "false").Run()
	}
	exclPath := filepath.Join(p.projet, ".git", "info", "exclude")
	_ = os.MkdirAll(filepath.Dir(exclPath), 0o755)
	exclBytes, _ := os.ReadFile(exclPath)
	exclLignes := map[string]bool{}
	for _, l := range strings.Split(string(exclBytes), "\n") {
		exclLignes[strings.TrimSuffix(l, "\r")] = true
	}
	motifsSecrets := []string{".env", ".env.*", "!.env.example", "!.env.sample", "!.env.template", "!.env.dist"}
	tousExcl := append([]string{".athanor/", ".claude/", "/.bashrc", "/.bash_profile", "/.profile", "/.zshrc", "/.zprofile", "/.gitconfig", "/.gitmodules", "/.ripgreprc", "/.idea", "/.vscode", "/.mcp.json"}, motifsSecrets...)
	var ajoutExcl strings.Builder
	if len(exclBytes) > 0 && exclBytes[len(exclBytes)-1] != '\n' {
		ajoutExcl.WriteByte('\n')
	}
	for _, m := range tousExcl {
		if !exclLignes[m] {
			ajoutExcl.WriteString(m + "\n")
			exclLignes[m] = true
		}
	}
	if ajoutExcl.Len() > 0 {
		if ef, err := os.OpenFile(exclPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_, _ = ef.WriteString(ajoutExcl.String())
			ef.Close()
		}
	}

	gi := filepath.Join(p.projet, ".gitignore")
	if !estLien(gi) {
		st, err := os.Stat(gi)
		if os.IsNotExist(err) || (err == nil && st.Mode().IsRegular()) {
			giBytes, _ := os.ReadFile(gi)
			if !reGitignoreEnv.Match(giBytes) {
				var gb strings.Builder
				if len(giBytes) > 0 {
					if giBytes[len(giBytes)-1] != '\n' {
						gb.WriteByte('\n')
					}
					gb.WriteByte('\n')
				}
				gb.WriteString("# Local secrets (added by Athanor)\n")
				for _, m := range motifsSecrets {
					gb.WriteString(m + "\n")
				}
				if gf, err := os.OpenFile(gi, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
					_, errW := gf.WriteString(gb.String())
					gf.Close()
					if errW == nil {
						p.log("Local secrets (.env) added to the project's .gitignore.")
					}
				}
			}
		}
	}

	p.appliquerIdentite()

	if !fichierRegulierPilote(filepath.Join(p.prive, "rangement")) {
		var aRanger strings.Builder
		for _, f := range []string{"docs/REQUIREMENTS.md", "docs/DESIGN.md", "docs/ARCHITECTURE.md", "docs/DECISIONS.md"} {
			fp := filepath.Join(p.projet, filepath.FromSlash(f))
			if !estLien(fp) && fichierRegulierPilote(fp) {
				aRanger.WriteString("- `" + f + "`\n")
			}
		}
		if !estLien(gi) && fichierRegulierPilote(gi) {
			if giBytes, err := os.ReadFile(gi); err == nil && reGitignoreAtelier.Match(giBytes) {
				aRanger.WriteString("- the `.athanor/` or `.claude/` line of `.gitignore`\n")
			}
		}
		if aRanger.Len() > 0 {
			if publierFichier(filepath.Join(p.etat, "TIDY"), []byte("# Files of the piloting to take out of the project\n\n"+aRanger.String())) == nil {
				p.log("Files of the piloting found in the project: Claude moves them into the workshop first (.athanor/TIDY).")
			}
		}
		_ = os.WriteFile(filepath.Join(p.prive, "rangement"), []byte(time.Now().Format("2006-01-02 15:04")+"\n"), 0o644)
	}

	p.retirerSecretsSuivis()

	if gitP(p.projet, nil, "rev-parse", "HEAD").Run() != nil {
		if gitP(p.projet, nil, "add", "-A").Run() != nil || gitP(p.projet, nil, "commit", "-qm", "Initial commit").Run() != nil {
			_ = gitP(p.projet, nil, "commit", "-q", "--allow-empty", "-m", "Initial commit").Run()
		}
	}

	if !p.outilsPython {
		lignes, _ := reprendreProjet(p.projet, p.prive)
		for _, l := range lignes {
			if l != "" {
				p.log(l)
			}
		}
	}

	if !fichierRegulierPilote(filepath.Join(p.prive, "debut")) {
		d := entier64Pilote(lireTexteCourt(filepath.Join(p.etat, "debut")), time.Now().Unix())
		_ = os.WriteFile(filepath.Join(p.prive, "debut"), []byte(strconv.FormatInt(d, 10)+"\n"), 0o644)
	}
	p.debut = entier64Pilote(lireTexteCourt(filepath.Join(p.prive, "debut")), time.Now().Unix())

	but := fmt.Sprintf("/goal The next unticked milestone of .athanor/PLAN.md is completed and verified according to your instructions: bash .athanor/check.sh has been run and its output, showing success, is displayed in this conversation; .athanor/PLAN.md ticks this milestone; .athanor/LOG.md has a new entry; git log shows the commit. If .athanor/PLAN.md does not exist yet, this milestone is the framing phase described in your instructions, with no check.sh required. If all milestones are already ticked, the goal is reached when .athanor/FINAL_REPORT.md exists and contains the line STATUS: DONE. A milestone that waits for a task of the user (athanor todo list) is passed over for the next one; if .athanor/FOR_YOU_ANSWERED exists, every answered task is handled first (athanor todo ok, missing or resolved); if every unticked milestone waits for the user, the goal is reached once athanor todo wait has run. Or stop after %d turns. %s", p.maxTurnsPerCycle, p.phraseLangue)

	p.cycle = entierPilote(lireTexteCourt(filepath.Join(p.prive, "cycle")), 0)
	if p.cycle == 0 {
		if b, err := os.ReadFile(filepath.Join(p.prive, "stats.tsv")); err == nil && len(b) > 0 {
			for _, l := range strings.Split(string(b), "\n") {
				parts := strings.Split(l, "\t")
				if len(parts) >= 2 {
					cStr, _, _ := strings.Cut(parts[1], "·")
					if n := entierPilote(cStr, 0); n > p.cycle {
						p.cycle = n
					}
				}
			}
		}
	}
	erreurs := 0
	sansProgres := 0
	arrets := 0                                         // cycles in a row stopped by the watchdog
	_ = os.Remove(filepath.Join(p.prive, "images_gel")) // whether large images freeze Claude is seen again from this start

	for {
		p.compterFileAttente()
		if fichierRegulierPilote(filepath.Join(p.etat, "STOP")) {
			p.etatPhase = "arrete"
			p.terminer("Stop requested (STOP file). Delete it, then restart with start.")
		}
		ecouleH := (time.Now().Unix() - p.debut) / 3600
		if ecouleH >= int64(p.maxHours) {
			p.terminer(fmt.Sprintf("Maximum duration reached (%d h). The work in progress is in the project folder.", p.maxHours))
		}
		if p.estTermine() {
			_ = os.WriteFile(filepath.Join(p.etat, "done", ".last-delivery"), []byte(time.Now().Format("20060102-150405")+"\n"), 0o644)
			codeGH := p.githubPousser()
			if codeGH == 4 && fichierRegulierPilote(filepath.Join(p.etat, "SECRETS_FOUND.md")) {
				fRep := filepath.Join(p.etat, "FINAL_REPORT.md")
				if b, err := os.ReadFile(fRep); err == nil {
					nb := reStatusDonePilote.ReplaceAll(b, []byte("STATUS: REOPENED (a secret must leave the commits before the GitHub push)"))
					_ = os.WriteFile(fRep, nb, 0o644)
				}
				p.log("Delivery reopened: a secret must leave the commits before the GitHub push (.athanor/SECRETS_FOUND.md).")
			} else if !p.prochaineDemande() {
				_ = os.Remove(filepath.Join(p.etat, "auto"))
				p.terminer("DONE: the deliverable and FINAL_REPORT.md are in the project folder.")
			}
		}

		if !p.connexionPresente() {
			p.log("NO CONNECTION to Claude: put your token in .env (run configure), then restart.")
			p.statut("Waiting for connection (token missing in .env)")
			p.dormir(p.loginWaitMin * 60)
			continue
		}

		p.appliquerSeuilQuota()
		if fichierRegulierPilote(filepath.Join(p.etat, "STOP")) {
			continue
		}
		p.synchroniserDemande()
		p.signalerReponses()
		p.rangerSolutions(filepath.Join(p.etat, "SOLUTIONS.md"), "this project")
		p.rangerSolutions(filepath.Join(p.tools, "SOLUTIONS.md"), "this PC and its tools, every project")
		p.preparerEcran()
		p.preparerExtensions()
		p.preparerCycle()
		p.appliquerIdentite()
		p.preparerEquipe()

		p.cycle++
		_ = os.WriteFile(filepath.Join(p.prive, "cycle"), []byte(strconv.Itoa(p.cycle)+"\n"), 0o644)
		p.fichierLog = filepath.Join(p.etat, "logs", fmt.Sprintf("cycle-%s-%04d.jsonl", time.Now().Format("20060102-150405"), p.cycle))
		avant := p.gitHead(p.projet)
		cochesCycleDebut := p.cochesPlan()

		p.modeleCycle = p.model
		fSecours := filepath.Join(p.prive, "secours_jusqua")
		if p.fallbackModel != "" && fichierRegulierPilote(fSecours) {
			if time.Now().Unix() < entier64Pilote(lireTexteCourt(fSecours), 0) {
				p.modeleCycle = p.fallbackModel
			} else {
				_ = os.Remove(fSecours)
				mNom := p.model
				if mNom == "" {
					mNom = "default model"
				}
				p.log(fmt.Sprintf("Main model quota reopened: back to %s.", mNom))
			}
		}

		cadrage := false
		cadrageFin := false
		limiteCycleMin := 0
		limiteStr := ""
		if !p.planPret() {
			cadrage = true
			cadrageFin = fichierRegulierPilote(filepath.Join(p.prive, "cadrage_fin"))
			if p.framingModel != "" && p.modeleCycle == p.model {
				p.modeleCycle = p.framingModel
			}
			if p.framingMaxMin > 0 && entierPilote(lireTexteCourt(filepath.Join(p.prive, "cadrage_depasse")), 0) < 2 {
				if cadrageFin && p.framingMaxMin > 15 {
					limiteCycleMin = 15
				} else {
					limiteCycleMin = p.framingMaxMin
				}
				limiteStr = strconv.Itoa(limiteCycleMin)
			}
		}

		avecModele := ""
		if p.modeleCycle != "" {
			avecModele = " with " + p.modeleCycle
		}
		noteCadrage := ""
		if cadrage {
			noteCadrage = ", framing"
			if limiteStr != "" {
				noteCadrage += fmt.Sprintf(" (time box %s min)", limiteStr)
			}
		}
		p.log(fmt.Sprintf("Cycle %d: Claude is working%s%s (log: .athanor/logs/%s)", p.cycle, avecModele, noteCadrage, filepath.Base(p.fichierLog)))
		p.etatPhase = "travail"
		p.etatReprise = ""
		debutCycleTime := time.Now()
		p.etatDebutCycle = debutCycleTime.Format("2006-01-02T15:04:05")
		p.statut(fmt.Sprintf("Claude is working (cycle %d)", p.cycle))

		p.lireModeParallele()
		if p.parallel {
			p.absorberFileParallele()
			p.synchroniserDemande()
		}
		mode := "serie"
		butCycle := but
		consignesCycle := p.consignes
		planSeul := false

		if fichierRegulierPilote(filepath.Join(p.etat, "FOR_YOU_ANSWERED")) && fichierRegulierPilote(filepath.Join(p.modeles, "guides", "FOR_YOU.md")) {
			b1, _ := os.ReadFile(consignesCycle)
			b2, _ := os.ReadFile(filepath.Join(p.modeles, "guides", "FOR_YOU.md"))
			consignesCycle = filepath.Join(p.prive, "CLAUDE.reponses.md")
			_ = os.WriteFile(consignesCycle, append(append(b1, '\n', '\n'), b2...), 0o644)
		}
		if os.Getenv("ATHANOR_SANS_COAUTEUR") == "1" {
			b1, _ := os.ReadFile(p.consignes)
			ajout := "\n## Commit messages\n\nNever add a `Co-Authored-By` line, a \"Generated with\" line or any mention of Claude or of an AI tool to a commit message: the commits are signed by the user alone.\n"
			consignesCycle = filepath.Join(p.prive, "CLAUDE.auteur.md")
			_ = os.WriteFile(consignesCycle, append(b1, []byte(ajout)...), 0o644)
		}
		if p.parallel {
			b1, _ := os.ReadFile(consignesCycle)
			consignesCycle = filepath.Join(p.prive, "CLAUDE.cycle.md")
			_ = os.WriteFile(consignesCycle, append(b1, []byte(consignesParallelePilote)...), 0o644)
			if fichierRegulierPilote(filepath.Join(p.etat, "PLAN.md")) && fichierRegulierPilote(filepath.Join(p.etat, "REQUEST_CHANGED")) && p.planADesVagues() {
				butCycle = fmt.Sprintf("/goal The change of .athanor/REQUEST.md (flagged by .athanor/REQUEST_CHANGED) is folded into .athanor/PLAN.md as waves according to your parallel-mode instructions, the change is noted in .athanor/LOG.md, .athanor/REQUEST_CHANGED is deleted and the plan is committed, without writing any code. Or stop after 15 turns. %s", p.phraseLangue)
				planSeul = true
				p.log("Parallel mode: the request changed, the lead updates the plan first.")
			} else if fichierRegulierPilote(filepath.Join(p.etat, "PLAN.md")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "RECHECK")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "RESUME.md")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "REQUEST_CHANGED")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "REVERIFY")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "SECRETS_FOUND.md")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "SECRETS_PUBLISHED.md")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "TIDY")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "FOR_YOU_ANSWERED")) &&
				!fichierRegulierPilote(filepath.Join(p.etat, "SAVE_CHECK")) &&
				tailleFichierPilote(filepath.Join(p.prive, "relais.md")) == 0 {
				if p.planADesVagues() {
					raisonSerie := p.raisonSerieParallele()
					if raisonSerie == "" {
						mode = "parallele"
					} else if raisonSerie != lireTexteCourt(filepath.Join(p.prive, "raison_serie")) {
						p.log(fmt.Sprintf("Parallel mode: the lead works alone this cycle (%s).", raisonSerie))
					}
					_ = os.WriteFile(filepath.Join(p.prive, "raison_serie"), []byte(raisonSerie), 0o644)
				} else {
					butCycle = fmt.Sprintf("/goal .athanor/PLAN.md is reorganised into waves (## Wave 1, ## Wave 2…) according to your parallel-mode instructions, without writing any code. Or stop after 15 turns. %s", p.phraseLangue)
					p.log("Parallel mode: the plan has no waves yet, the lead reorganises it first.")
				}
			}
		}
		if cadrage {
			mode = "serie"
			butCycle = p.butCadrage(cadrageFin, limiteStr)
			b1, _ := os.ReadFile(consignesCycle)
			consignesCycle = filepath.Join(p.prive, "CLAUDE.cadrage.md")
			_ = os.WriteFile(consignesCycle, append(b1, []byte(p.consignesCadrage(cadrageFin, limiteStr))...), 0o644)
		}
		if mode == "parallele" && time.Now().Unix() < entier64Pilote(lireTexteCourt(filepath.Join(p.prive, "serie_jusqua")), 0) {
			mode = "serie"
		} else if mode == "parallele" && fichierRegulierPilote(filepath.Join(p.prive, "sonde_quota")) {
			mode = "serie"
			p.log("After the quota stop: one milestone by the lead first, to check that the quota is back before starting workers.")
		}

		p.quotaOuvriers = false
		p.modeleVerif = ""
		code := 0
		if mode == "parallele" {
			code = p.cycleParallele(strings.TrimSuffix(p.fichierLog, ".jsonl"), p.modeleCycle)
			if code == 3 {
				mode = "serie"
			}
		}
		if mode == "serie" && !cadrage {
			var partiels []string
			for _, br := range p.branchesPartielles() {
				if len(partiels) >= 20 {
					break
				}
				partiels = append(partiels, "`auto/"+br+"`")
			}
			if len(partiels) > 0 {
				b1, _ := os.ReadFile(consignesCycle)
				ajout := fmt.Sprintf("\n\n## Partial work left by workers\n\nThese branches hold work on a milestone started by a parallel worker that was stopped: %s. When you take one of these milestones, merge its branch first (`git merge --no-ff auto/<milestone>`) and carry on from there, never redo it; once the milestone is ticked, delete the branch (`git branch -D auto/<milestone>`).\n", strings.Join(partiels, ", "))
				consignesCycle = filepath.Join(p.prive, "CLAUDE.serie.md")
				_ = os.WriteFile(consignesCycle, append(b1, []byte(ajout)...), 0o644)
			}
		}
		if mode == "serie" && !cadrage && tailleFichierPilote(filepath.Join(p.prive, "relais.md")) > 0 {
			b1, _ := os.ReadFile(consignesCycle)
			b2, _ := os.ReadFile(filepath.Join(p.prive, "relais.md"))
			consignesCycle = filepath.Join(p.prive, "CLAUDE.relais.md")
			_ = os.WriteFile(consignesCycle, append(append(b1, '\n', '\n'), b2...), 0o644)
			_ = os.Remove(filepath.Join(p.prive, "relais.md"))
		}
		if mode == "serie" && fichierRegulierPilote(filepath.Join(p.prive, "mcp.json")) && tailleFichierPilote(filepath.Join(p.prive, "mcp.json.sauvegarde.md")) > 0 {
			b1, _ := os.ReadFile(consignesCycle)
			b2, _ := os.ReadFile(filepath.Join(p.prive, "mcp.json.sauvegarde.md"))
			consignesCycle = filepath.Join(p.prive, "CLAUDE.mcp.md")
			_ = os.WriteFile(consignesCycle, append(append(b1, '\n', '\n'), b2...), 0o644)
		}
		// large images froze a session (athanor images): Claude keeps its images small meanwhile
		if mode == "serie" && !p.outilsPython && tailleFichierPilote(filepath.Join(p.prive, "images_gel")) > 0 {
			b1, _ := os.ReadFile(consignesCycle)
			consignesCycle = filepath.Join(p.prive, "CLAUDE.images.md")
			_ = os.WriteFile(consignesCycle, append(b1, consigneImages(entierPilote(lireTexteCourt(filepath.Join(p.prive, "images_gel")), 0))...), 0o644)
		}
		if mode == "serie" {
			texteConsignes, _ := os.ReadFile(consignesCycle)
			cArgs := []string{"-p", butCycle, "--permission-mode", "bypassPermissions", "--settings", p.reglages, "--permission-prompts", "none", "--output-format", "stream-json", "--verbose"}
			cArgs = append(cArgs, p.argsSurs...)
			cArgs = append(cArgs, "--append-system-prompt", strings.TrimRight(string(texteConsignes), "\r\n"))
			if p.agentsParJSON {
				cArgs = append(cArgs, "--agents", p.agentsJSON)
			}
			if fichierRegulierPilote(filepath.Join(p.prive, "mcp.json")) {
				cArgs = append(cArgs, "--mcp-config", filepath.Join(p.prive, "mcp.json"))
			}
			if p.pluginAthanor != "" {
				cArgs = append(cArgs, "--plugin-dir", p.pluginAthanor)
			}
			for _, dp := range p.pluginsDirs {
				cArgs = append(cArgs, "--plugin-dir", dp)
			}
			if p.modeleCycle != "" {
				cArgs = append(cArgs, "--model", p.modeleCycle)
			}
			if p.fallbackModel != "" && p.modeleCycle != p.fallbackModel {
				cArgs = append(cArgs, "--fallback-model", p.fallbackModel)
			}
			if p.effort != "" {
				cArgs = append(cArgs, "--effort", p.effort)
			}
			code = p.lancerClaudeSurveille(p.fichierLog, cArgs, cadrage, limiteCycleMin, true)
		}
		p.etatPhase = "entre_cycles"
		if planSeul {
			_ = os.Remove(filepath.Join(p.etat, "REQUEST_CHANGED"))
		}

		apres := p.gitHead(p.projet)
		texteResultat, enErreur, repriseQuota, typeLimite, finLog, serveursMCP := analyserJournalCycle(p.fichierLog)
		motifArret := lireTexteCourt(filepath.Join(p.prive, "cycle_arrete"))
		_ = os.Remove(filepath.Join(p.prive, "cycle_arrete"))
		clos := fichierRegulierPilote(filepath.Join(p.prive, "cycle_clos"))
		_ = os.Remove(filepath.Join(p.prive, "cycle_clos"))
		relais := fichierRegulierPilote(filepath.Join(p.prive, "cycle_relais"))
		_ = os.Remove(filepath.Join(p.prive, "cycle_relais"))

		var etatCycle string
		switch {
		case reHitLimitPilote.MatchString(texteResultat):
			etatCycle = "quota"
		case repriseQuota != "" && (code != 0 || enErreur):
			etatCycle = "quota"
		case clos:
			etatCycle = "ok"
		case relais:
			etatCycle = "relais"
		case motifArret != "":
			etatCycle = "bloque"
		case code == 0 && !enErreur:
			etatCycle = "ok"
		case reQuotaTextePilote.MatchString(texteResultat + "\n" + finLog):
			etatCycle = "quota"
		case reConnTextePilote.MatchString(texteResultat + "\n" + finLog):
			etatCycle = "connexion"
		default:
			etatCycle = "erreur"
		}

		issueCadrage := ""
		if cadrage && etatCycle != "quota" && etatCycle != "connexion" {
			if p.planPret() {
				issueCadrage = "fait"
				etatCycle = "ok"
			} else {
				issueCadrage = "a_conclure"
				etatCycle = "bloque"
				if fichierRegulierPilote(filepath.Join(p.etat, "PLAN.md")) {
					p.log(`Framing: .athanor/PLAN.md has no milestone line the pilot can read (each one must start with "- [ ] M1 : ").`)
				}
			}
		}

		modStats := p.modeleCycle
		if p.modeleVerif != "" {
			modStats = p.modeleVerif
		}
		ancMod := p.modeleCycle
		p.modeleCycle = modStats
		p.enregistrerStats(p.fichierLog, etatCycle, strconv.Itoa(p.cycle), p.projet)
		p.modeleCycle = ancMod
		// a session that froze right after a large image: the next cycles ask Claude to keep its images
		// small, until an image as large goes through again
		if !p.outilsPython {
			fGel := filepath.Join(p.prive, "images_gel")
			if kb, ok := imageGelee(p.fichierLog); ok && motifArret != "" {
				if avantKB := entierPilote(lireTexteCourt(fGel), 0); avantKB == 0 {
					_ = os.WriteFile(fGel, []byte(strconv.Itoa(kb)+"\n"), 0o644)
					p.log(fmt.Sprintf("Claude froze right after an image of %d KB: the next cycles ask it to keep its images small.", kb))
				} else if kb < avantKB {
					_ = os.WriteFile(fGel, []byte(strconv.Itoa(kb)+"\n"), 0o644)
				}
			} else if tailleFichierPilote(fGel) > 0 && plusGrandeImagePassee(p.fichierLog) >= max(1, entierPilote(lireTexteCourt(fGel), 1)) {
				_ = os.Remove(fGel)
				p.log("Large images go through again: the cycles no longer ask Claude to shrink them.")
			}
		}

		cochesFin := p.cochesPlan()
		if cochesFin > cochesCycleDebut {
			_ = os.Remove(filepath.Join(p.etat, "HANDOFF.md"))
			if len(serveursMCP) > 0 && !produitChangePilote(p.projet, avant, apres) {
				_ = os.WriteFile(filepath.Join(p.etat, "SAVE_CHECK"), []byte(strings.Join(serveursMCP, "\n")+"\n"), 0o644)
				p.log(fmt.Sprintf("The milestone used MCP servers (%s) but its commit changed no file: Claude first saves their work into the project on the next cycle.", strings.Join(serveursMCP, " ")))
			}
		}
		if produitChangePilote(p.projet, avant, apres) && cochesFin > cochesCycleDebut &&
			fichierRegulierPilote(filepath.Join(p.etat, "check.sh")) && !checkReussiPilote(p.etat, p.fichierLog, debutCycleTime.Unix()) {
			_ = os.WriteFile(filepath.Join(p.etat, "RECHECK"), []byte(time.Now().Format("2006-01-02 15:04")+"\n"), 0o644)
			p.log("Warning: milestone ticked without a visible successful full check; Claude will recheck first on the next cycle.")
		}

		attenteVous := false
		if etatCycle == "ok" && fichierRegulierPilote(filepath.Join(p.etat, "WAITING_FOR_YOU")) {
			pv, at := p.tachesEtat()
			if pv > 0 && at == 0 {
				attenteVous = true
			} else {
				_ = os.Remove(filepath.Join(p.etat, "WAITING_FOR_YOU"))
			}
		}
		if etatCycle != "ok" {
			_ = os.Remove(filepath.Join(p.etat, "WAITING_FOR_YOU"))
		}
		if etatCycle != "quota" && etatCycle != "connexion" {
			_ = os.Remove(filepath.Join(p.prive, "sonde_quota"))
		}

		if issueCadrage == "fait" {
			erreurs = 0
			sansProgres = 0
			_ = os.Remove(filepath.Join(p.prive, "cadrage_fin"))
			_ = os.Remove(filepath.Join(p.etat, "ALERT.md"))
			if strings.Contains(motifArret, "framing time box") {
				dep := entierPilote(lireTexteCourt(filepath.Join(p.prive, "cadrage_depasse")), 0) + 1
				_ = os.WriteFile(filepath.Join(p.prive, "cadrage_depasse"), []byte(strconv.Itoa(dep)+"\n"), 0o644)
				p.log(fmt.Sprintf("Framing time box reached: the plan as it stands goes ahead (%d milestones).", p.jalonsPlan()))
			} else {
				_ = os.Remove(filepath.Join(p.prive, "cadrage_depasse"))
				p.log(fmt.Sprintf("Framing done in %d min: %d milestones in the plan.", (time.Now().Unix()-debutCycleTime.Unix())/60, p.jalonsPlan()))
			}
			etatCycle = "cadre"
		} else if issueCadrage == "a_conclure" {
			erreurs = 0
			dep := entierPilote(lireTexteCourt(filepath.Join(p.prive, "cadrage_depasse")), 0) + 1
			_ = os.WriteFile(filepath.Join(p.prive, "cadrage_depasse"), []byte(strconv.Itoa(dep)+"\n"), 0o644)
			if cadrageFin {
				_ = os.Remove(filepath.Join(p.prive, "cadrage_fin"))
				p.log("The closing cycle wrote no plan either: the next framing runs without a time box.")
			} else {
				_ = os.WriteFile(filepath.Join(p.prive, "cadrage_fin"), nil, 0o644)
				raison := motifArret
				if raison == "" {
					raison = "the session stopped"
				}
				p.log(fmt.Sprintf("Framing ended with no plan yet (%s): a short cycle writes it now from the documents already there.", raison))
			}
			sansProgres++
			if sansProgres >= p.maxCyclesWithoutProgress {
				alerte := fmt.Sprintf("# Alert: no plan\n%s: %d framing cycles in a row without a plan.\nLook at .athanor/LOG.md and logs/. You can refine your request.\n", time.Now().Format("2006-01-02 15:04"), sansProgres)
				_ = os.WriteFile(filepath.Join(p.etat, "ALERT.md"), []byte(alerte), 0o644)
				p.log(fmt.Sprintf("ALERT: %d framing cycles without a plan, 30 min pause (see .athanor/ALERT.md).", sansProgres))
				p.etatPhase = "erreur"
				p.statut(fmt.Sprintf("Paused after %d framing cycles without a plan", sansProgres))
				sansProgres = 0
				p.dormir(1800)
			}
			etatCycle = "cadre"
		}

		if etatCycle != "bloque" {
			arrets = 0
		}
		switch etatCycle {
		case "cadre":
		case "ok":
			erreurs = 0
			if avant == apres && attenteVous {
				sansProgres = 0
			} else if avant == apres {
				sansProgres++
				p.log(fmt.Sprintf("Cycle %d finished without a new commit (%d/%d).", p.cycle, sansProgres, p.maxCyclesWithoutProgress))
				if sansProgres >= p.maxCyclesWithoutProgress {
					alerte := fmt.Sprintf("# Alert: no progress\n%s: %d cycles in a row without a commit.\nLook at .athanor/LOG.md, BLOCKERS.md and logs/. You can refine your request.\n", time.Now().Format("2006-01-02 15:04"), sansProgres)
					_ = os.WriteFile(filepath.Join(p.etat, "ALERT.md"), []byte(alerte), 0o644)
					p.log(fmt.Sprintf("ALERT: %d cycles without progress, 30 min pause (see .athanor/ALERT.md).", sansProgres))
					p.etatPhase = "erreur"
					p.statut(fmt.Sprintf("Paused after %d cycles without progress", sansProgres))
					sansProgres = 0
					p.dormir(1800)
				}
			} else {
				sansProgres = 0
				_ = os.Remove(filepath.Join(p.etat, "ALERT.md"))
				if exe, err := os.Executable(); err == nil && !p.outilsPython {
					c := exec.Command(exe, "screenshots", "--accept")
					c.Env = append(os.Environ(), "PROJECT="+p.projet)
					cacherFenetre(c)
					_ = c.Run()
				}
				sujet, _ := gitP(p.projet, nil, "log", "-1", "--pretty=%s").Output()
				p.log(fmt.Sprintf("Cycle %d succeeded: %s", p.cycle, strings.TrimSpace(string(sujet))))
				if clos {
					p.log("(cycle closed by the pilot after the milestone: the next one starts with a fresh context)")
				}
				p.githubPousser()
			}
		case "quota":
			var attente int
			if reEntierPilote.MatchString(repriseQuota) {
				rq := entier64Pilote(repriseQuota, 0)
				attente = int(rq - time.Now().Unix() + 120)
				if attente > 21600 {
					attente = 21600
				}
				if attente < 60 {
					attente = 60
				}
			} else {
				attente = attenteQuotaCalcul(texteResultat+" "+finLog, time.Now(), p.quotaWaitMin)
			}
			reprise := time.Unix(time.Now().Unix()+int64(attente), 0).Format("2006-01-02 15:04")
			p.etatPhase = "quota"
			p.etatReprise = reprise
			_ = os.Remove(filepath.Join(p.prive, "sonde_quota"))
			estCompte := typeLimite == "five_hour" || typeLimite == "seven_day"
			if p.quotaOuvriers && !estCompte && p.workerModel != "" && p.workerModel != p.modeleCycle {
				_ = os.WriteFile(filepath.Join(p.prive, "serie_jusqua"), []byte(strconv.FormatInt(time.Now().Unix()+int64(attente), 10)+"\n"), 0o644)
				p.log(fmt.Sprintf("Quota of the workers' model %s reached: the lead goes on alone, without workers, until about %s.", p.workerModel, reprise))
				p.statut("Workers paused until " + reprise)
				p.dormir(60)
			} else if p.fallbackModel != "" && p.modeleCycle != p.fallbackModel && estCompte {
				nomLim := "weekly limit"
				if typeLimite == "five_hour" {
					nomLim = "5-hour limit"
				}
				p.log(fmt.Sprintf("Quota of the account reached (%s): the fallback model shares it, pausing until about %s, then resuming automatically.", nomLim, reprise))
				p.statut("Quota reached, resuming around " + reprise)
				_ = os.WriteFile(filepath.Join(p.prive, "sonde_quota"), nil, 0o644)
				p.dormir(attente)
			} else if p.fallbackModel != "" && p.modeleCycle != p.fallbackModel {
				_ = os.WriteFile(filepath.Join(p.prive, "secours_jusqua"), []byte(strconv.FormatInt(time.Now().Unix()+int64(attente), 10)+"\n"), 0o644)
				_ = os.WriteFile(filepath.Join(p.prive, "sonde_quota"), nil, 0o644)
				mNom := p.modeleCycle
				if mNom == "" {
					mNom = "default model"
				}
				p.log(fmt.Sprintf("Quota of %s reached: switching to %s until about %s.", mNom, p.fallbackModel, reprise))
				p.statut(fmt.Sprintf("Fallback model %s until %s", p.fallbackModel, reprise))
				p.dormir(60)
			} else {
				p.log(fmt.Sprintf("Quota reached: pausing until about %s, then resuming automatically.", reprise))
				p.statut("Quota reached, resuming around " + reprise)
				_ = os.WriteFile(filepath.Join(p.prive, "sonde_quota"), nil, 0o644)
				p.dormir(attente)
			}
		case "relais":
			erreurs = 0
			p.log(fmt.Sprintf("Cycle %d: context full (%sk tokens). A fresh session takes over the milestone now, from the relay notes.", p.cycle, lireTexteCourt(filepath.Join(p.prive, "relais_k"))))
		case "connexion":
			p.etatPhase = "connexion"
			p.log(fmt.Sprintf("Connection problem (token expired or invalid?). Retrying in %d min.", p.loginWaitMin))
			p.statut("Connection problem: check the token in .env")
			p.dormir(p.loginWaitMin * 60)
		case "bloque":
			erreurs++
			arrets++
			if arrets >= p.maxCyclesWithoutProgress {
				// the same stop again and again: said once, with a long pause, rather than a restart every few minutes
				_ = os.WriteFile(filepath.Join(p.etat, "ALERT.md"), []byte(fmt.Sprintf("# Alert: cycles stopped by the watchdog\n%s: %d cycles in a row stopped by the watchdog (the last one: %s).\nThe last lines of each cycle log (.athanor/logs/) show where Claude stopped. You can refine your request.\n", time.Now().Format("2006-01-02 15:04"), arrets, motifArret)), 0o644)
				p.log(fmt.Sprintf("ALERT: %d cycles in a row stopped by the watchdog (%s), 30 min pause (see .athanor/ALERT.md).", arrets, motifArret))
				p.etatPhase = "erreur"
				p.statut(fmt.Sprintf("Paused after %d cycles stopped by the watchdog", arrets))
				arrets = 0
				p.dormir(1800)
			} else {
				p.log(fmt.Sprintf("Cycle %d stopped by the watchdog (%s). The work already done stays in the folder; resuming in %d min.", p.cycle, motifArret, p.errorWaitMin))
				p.etatPhase = "erreur"
				p.statut(fmt.Sprintf("Cycle stopped (%s), resuming in %d min", motifArret, p.errorWaitMin))
				p.dormir(p.errorWaitMin * 60)
			}
		case "erreur":
			p.etatPhase = "erreur"
			erreurs++
			pause := p.errorWaitMin
			for i := 1; i < erreurs && pause < p.errorWaitMaxMin; i++ {
				pause *= 2
			}
			if pause > p.errorWaitMaxMin {
				pause = p.errorWaitMaxMin
			}
			p.log(fmt.Sprintf("Error on cycle %d (code %d, %d in a row). Retrying in %d min.", p.cycle, code, erreurs, pause))
			p.statut(fmt.Sprintf("Error, retrying in %d min", pause))
			p.dormir(pause * 60)
		}
		if attenteVous {
			p.attendreUtilisateur()
		}
	}
}

var (
	reEnvSuiviPilote   = regexp.MustCompile(`(^|/)\.env(\.[^/]+)?$`)
	reEnvExemplePilote = regexp.MustCompile(`(^|/)\.env\.(example|sample|template|dist)$`)
	reTypeBruitLog     = regexp.MustCompile(`"type":"(rate_limit_event|tool_progress)"`)
)

func (p *pilote) retirerSecretsSuivis() {
	out, err := gitP(p.projet, nil, "ls-files", "-z").Output()
	if err != nil || len(out) == 0 {
		return
	}
	var candidats []byte
	for _, f := range bytes.Split(out, []byte{0}) {
		s := string(f)
		if s != "" && reEnvSuiviPilote.MatchString(s) && !reEnvExemplePilote.MatchString(s) {
			candidats = append(candidats, f...)
			candidats = append(candidats, 0)
		}
	}
	if len(candidats) == 0 {
		return
	}
	cmdIgnore := gitP(p.projet, nil, "check-ignore", "-z", "--stdin", "--no-index")
	cmdIgnore.Stdin = bytes.NewReader(candidats)
	ignOut, _ := cmdIgnore.Output()
	var suivis []string
	for _, f := range bytes.Split(ignOut, []byte{0}) {
		if len(f) > 0 {
			suivis = append(suivis, string(f))
		}
	}
	if len(suivis) == 0 {
		return
	}
	var listeLignes []string
	for i, s := range suivis {
		if i >= 20 {
			break
		}
		listeLignes = append(listeLignes, "- `"+s+"`")
	}
	liste := strings.Join(listeLignes, "\n")
	envLit := []string{"GIT_LITERAL_PATHSPECS=1"}
	rmArgs := append([]string{"rm", "-q", "--cached", "--"}, suivis...)
	if gitP(p.projet, envLit, rmArgs...).Run() == nil {
		if ancienOut, err := gitP(p.projet, nil, "rev-parse", "-q", "--verify", "HEAD^{commit}").Output(); err == nil {
			ancien := strings.TrimSpace(string(ancienOut))
			idx := filepath.Join(p.prive, fmt.Sprintf("index.secrets.%d", os.Getpid()))
			_ = os.Remove(idx)
			envIdx := []string{"GIT_INDEX_FILE=" + idx, "GIT_LITERAL_PATHSPECS=1"}
			okCommit := false
			if gitP(p.projet, envIdx, "read-tree", ancien).Run() == nil &&
				gitP(p.projet, envIdx, rmArgs...).Run() == nil {
				if arbreOut, err := gitP(p.projet, envIdx, "write-tree").Output(); err == nil {
					arbre := strings.TrimSpace(string(arbreOut))
					if nouvOut, err := gitP(p.projet, nil, "commit-tree", arbre, "-p", ancien, "-m", "Stop tracking local secret files").Output(); err == nil {
						nouveau := strings.TrimSpace(string(nouvOut))
						if gitP(p.projet, nil, "update-ref", "-m", "athanor: stop tracking local secret files", "HEAD", nouveau, ancien).Run() == nil {
							okCommit = true
						}
					}
				}
			}
			_ = os.Remove(idx)
			if !okCommit {
				p.log("Local secret files: taken out of git, but the commit failed (Claude commits it with its next change).")
			}
		}
		p.log("Local secret files taken out of git (still on disk, now ignored): " + tronquerRunes(strings.Join(suivis, " ")+" ", 300))
		remArgs := append([]string{"log", "--remotes", "-1", "--format=%H", "--"}, suivis...)
		remOut, _ := gitP(p.projet, envLit, remArgs...).Output()
		publie := strings.TrimSpace(string(remOut)) != ""
		if !publie {
			cmdPub := p.commandeBash(filepath.Join(p.systeme, "github.sh"), append([]string{"publie", filepath.ToSlash(p.projet)}, suivis...)...)
			publie = cmdPub.Run() == nil
		}
		if publie {
			msg := fmt.Sprintf("# Secret files that were published\n\nThese files hold local secrets and were in commits already pushed (to GitHub or another remote):\n\n%s\n\nThe pilot took them out of git (they are still on disk, and now ignored), but their values stay readable in the published history: only changing them protects the project.\n\n1. Add one task for the user (`athanor todo add`): regenerate (revoke, then replace) each key, token or password these files hold, at the service that issued it, then put the new value back in the file. Name the files and the services or variable names concerned, never a value.\n2. Note it in `.athanor/LOG.md`, then delete this file.\n", liste)
			if publierFichier(filepath.Join(p.etat, "SECRETS_PUBLISHED.md"), []byte(msg)) == nil {
				p.log("These secret files were already pushed: Claude asks you to change their values (For you page).")
			}
		}
	} else {
		p.log("Local secret files followed by git could not be taken out of it: " + tronquerRunes(strings.Join(suivis, " ")+" ", 300))
	}
}

func analyserJournalCycle(chemin string) (texteResultat string, enErreur bool, repriseQuota string, typeLimite string, finLog string, serveursMCP []string) {
	b, err := os.ReadFile(chemin)
	if err != nil {
		return "", true, "", "", "", nil
	}
	var lignesPropres []string
	for _, l := range strings.Split(strings.TrimRight(string(b), "\r\n"), "\n") {
		if !reTypeBruitLog.MatchString(l) {
			lignesPropres = append(lignesPropres, l)
		}
	}
	if len(lignesPropres) > 15 {
		lignesPropres = lignesPropres[len(lignesPropres)-15:]
	}
	finLog = strings.Join(lignesPropres, "\n")

	var res map[string]any
	mcpSet := map[string]bool{}
	for _, o := range lignesJSON(chemin, 0) {
		switch o["type"] {
		case "result":
			res = o
		case "rate_limit_event":
			if info := dict(o["rate_limit_info"]); info != nil && info["status"] == "rejected" {
				if r := info["resetsAt"]; r != nil && r != false {
					repriseQuota = fmt.Sprint(r)
				}
				if t, ok := info["rateLimitType"].(string); ok {
					typeLimite = t
				}
			}
		case "assistant":
			if msg := dict(o["message"]); msg != nil {
				for _, raw := range liste(msg["content"]) {
					c := dict(raw)
					if c != nil && c["type"] == "tool_use" {
						if nom, ok := c["name"].(string); ok && strings.HasPrefix(nom, "mcp__") {
							parts := strings.Split(nom, "__")
							if len(parts) >= 2 && parts[1] != "" {
								mcpSet[parts[1]] = true
							}
						}
					}
				}
			}
		}
	}
	if res == nil {
		enErreur = true
	} else {
		if r, ok := res["result"].(string); ok {
			texteResultat = r
		}
		if e, ok := res["is_error"].(bool); ok && e {
			enErreur = true
		}
	}
	for s := range mcpSet {
		serveursMCP = append(serveursMCP, s)
	}
	sort.Strings(serveursMCP)
	if len(serveursMCP) > 10 {
		serveursMCP = serveursMCP[:10]
	}
	return
}
