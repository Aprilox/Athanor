package main

// The pilot's start and stop (the former commands/start.sh, stop.sh, stop-after-cycle.sh and follow.sh):
//
//   athanor demarrer [--background | --keep | --attach] [PROJECT]
//       starts the pilot on PROJECT (default KIT/projet). --background: in the background, returns at once.
//       --keep (the Windows app, WSL mode): like --background, then stays as long as the pilot runs, so that
//       Windows keeps WSL alive; if it is killed, the pilot goes on. --attach: only keeps an already running
//       pilot alive, never starts one. Without an option: in the foreground, its output also in output.log.
//   athanor arreter [--apres-cycle] [PROJECT]
//       stops the pilot now (the current cycle is interrupted), or at the end of the current cycle.
//   athanor suivre [PROJECT]
//       what the pilot does, live (its status, then its output; Ctrl+C to quit, the pilot goes on).
//
// The pilot runs under a supervisor (demarrer --surveiller): a pilot that hits an internal error of its own
// (a Go panic: exit 70, after it stopped its sessions; a fatal error: exit 2 with a crash report) is
// started again after 30 s, three times an hour at most; the report stays in the pilot's private folder.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func init() {
	enregistrerCommande("demarrer", "demarrer [--background|--keep|--attach] [PROJECT]: starts the pilot on a project (the application, or a terminal)", lancerDemarrer)
	enregistrerCommande("arreter", "arreter [--apres-cycle] [PROJECT]: stops a project's pilot now, or at the end of the current cycle", lancerArreter)
	enregistrerCommande("suivre", "suivre [PROJECT]: what the pilot does on a project, live (Ctrl+C to quit; the pilot goes on)", lancerSuivre)
}

// kitDuProgramme: the tool folder: KIT when given, else the executable's folder (else the current one).
func kitDuProgramme() string {
	if k := os.Getenv("KIT"); k != "" {
		return abspathOS(cheminNatifPilote(k))
	}
	if exe, err := os.Executable(); err == nil {
		if d := filepath.Dir(exe); estDossierKit(d) {
			return d
		}
	}
	if wd, err := os.Getwd(); err == nil && estDossierKit(wd) {
		return wd
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// projetArgument: the project folder given (a Windows path in WSL is converted), else KIT/projet.
func projetArgument(kit, arg string) string {
	if arg == "" {
		arg = os.Getenv("PROJECT")
	}
	if arg == "" {
		return filepath.Join(kit, "projet")
	}
	return abspathOS(cheminNatifPilote(arg))
}

// piloteVivant: the pid of the project's live pilot: the one of its private folder, else the one announced in
// the workshop (a pilot started with another path or from another copy of the tool), but only if that
// process really is a pilot. 0 when none runs.
func piloteVivant(kit, projet string) int {
	prive := priveDePilote(kit, projet, estWindows)
	if p := pidSurPilote(lireTexteCourt(filepath.Join(prive, "pilot.pid"))); p > 1 && processusVivantPilote(p) {
		return p
	}
	if p := pidSurPilote(lireTexteCourt(filepath.Join(projet, ".athanor", "pilot.pid"))); p > 1 && processusVivantPilote(p) {
		l := ligneDeCommande(p)
		if strings.Contains(l, "system/pilot.sh") || (strings.Contains(strings.ToLower(l), "athanor") && strings.Contains(l, "pilot")) {
			return p
		}
	}
	return 0
}

func lancerDemarrer(args []string) int {
	fond, garderVivant, attacher, surveiller := false, false, false, false
	arg := ""
	for _, a := range args {
		switch a {
		case "--background":
			fond = true
		case "--keep":
			fond, garderVivant = true, true
		case "--attach":
			garderVivant, attacher = true, true
		case "--surveiller":
			surveiller = true
		default:
			arg = a
		}
	}
	kit := kitDuProgramme()
	projet := projetArgument(kit, arg)
	if surveiller {
		return surveillerPilote(kit, projet, os.Stdout)
	}
	if st, err := os.Stat(projet); err == nil && st.IsDir() {
		migrerAtelierPilote(projet)
	}
	if err := os.MkdirAll(filepath.Join(projet, ".athanor"), 0o755); err != nil {
		fmt.Println("Project folder not accessible: " + projet)
		return 1
	}
	prive := priveDePilote(kit, projet, estWindows)
	_ = os.MkdirAll(prive, 0o755)
	sortie := filepath.Join(prive, "output.log")

	// waits for the pilot to end (keeps WSL alive); a single keeper per project
	garder := func() {
		if !garderVivant {
			return
		}
		v, ok := verrouPilote(filepath.Join(prive, "keeper.lock"))
		if !ok {
			return
		}
		defer v.Close()
		time.Sleep(5 * time.Second)
		for piloteVivant(kit, projet) > 0 {
			time.Sleep(30 * time.Second)
		}
	}
	if pid := piloteVivant(kit, projet); pid > 0 {
		fmt.Printf("The pilot is already running on this project (PID %d).\n", pid)
		garder()
		return 0
	}
	if attacher {
		return 0 // nothing to keep: --attach never starts a pilot
	}
	_ = os.Remove(filepath.Join(prive, "pilot.pid")) // stale (a pilot killed before it could clean up)
	if !fichierRegulierPilote(filepath.Join(kit, ".env")) {
		fmt.Println("No .env: set the token in the app (Settings).")
		return 1
	}
	if stop := filepath.Join(projet, ".athanor", "STOP"); existeLanceur(stop) {
		if os.Remove(stop) == nil {
			fmt.Println("(previous end-of-cycle stop cancelled: the pilot restarts)")
		}
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("Athanor's executable cannot be found: " + err.Error())
		return 1
	}
	if fond {
		// at the end, but not O_APPEND: on Windows that handle, inherited by the pilot's programs, is
		// append-only and some of them cannot write to it (Git Bash's)
		f, err := os.OpenFile(sortie, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Println("Cannot write the pilot's output: " + err.Error())
			return 1
		}
		_, _ = f.Seek(0, io.SeekEnd)
		c := commande(exe, "demarrer", "--surveiller", projet)
		c.Dir = projet
		c.Env = append(os.Environ(), "KIT="+kit)
		c.Stdout, c.Stderr = f, f
		if nul, err := os.Open(os.DevNull); err == nil {
			c.Stdin = nul
		}
		detacherProcessus(c)
		err = c.Start()
		f.Close()
		if err != nil {
			fmt.Println("Cannot start the pilot: " + err.Error())
			return 1
		}
		_ = c.Process.Release()
		fmt.Println("Pilot started in the background on " + projet)
		garder()
		return 0
	}
	// in the foreground: the output on the screen and in output.log
	var ecran io.Writer = os.Stdout
	if f, err := os.OpenFile(sortie, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		defer f.Close()
		ecran = io.MultiWriter(os.Stdout, f)
	}
	return surveillerPilote(kit, projet, ecran)
}

// surveillerPilote: the pilot as a child; started again after an internal error of its own.
func surveillerPilote(kit, projet string, ecran io.Writer) int {
	prive := priveDePilote(kit, projet, estWindows)
	crash := filepath.Join(prive, "pilote-go.crash")
	exe, err := os.Executable()
	if err != nil {
		fmt.Println("Athanor's executable cannot be found: " + err.Error())
		return 1
	}
	var reprises []time.Time
	for {
		// the report of an earlier error kept aside (pilote-go.crash.1): this run's is a new file
		if st, err := os.Stat(crash); err == nil && st.Size() > 0 {
			_ = os.Rename(crash, crash+".1")
		} else {
			_ = os.Remove(crash)
		}
		c := commande(exe, "pilote", projet)
		c.Dir = projet
		c.Env = append(os.Environ(), "KIT="+kit)
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, ecran, ecran
		err := c.Run()
		code := 0
		if err != nil {
			code = 1
			if c.ProcessState != nil {
				code = c.ProcessState.ExitCode()
			}
		}
		if st, e := os.Stat(crash); e == nil && st.Size() == 0 {
			_ = os.Remove(crash)
		}
		interne := code == 70 || (code == 2 && existeLanceur(crash))
		if !interne {
			return code
		}
		// a fatal error stops the pilot at once: its Claude sessions (the lead's, the workers') may still
		// run, outside its control: they are stopped before anything else starts
		journalArret := filepath.Join(prive, "arret.log")
		arreterSessionClaude(true, filepath.Join(prive, "claude_sid"), filepath.Join(prive, "claude_winpid"), journalArret)
		sids, _ := filepath.Glob(filepath.Join(prive, "ouvrier_*.sid"))
		for _, f := range sids {
			arreterSessionClaude(true, f, strings.TrimSuffix(f, ".sid")+".winpid", journalArret)
		}
		_ = os.Remove(filepath.Join(prive, "pilot.pid"))
		maintenant := time.Now()
		recentes := reprises[:0]
		for _, t := range reprises {
			if maintenant.Sub(t) < time.Hour {
				recentes = append(recentes, t)
			}
		}
		reprises = recentes
		if len(reprises) >= 3 {
			fmt.Fprintf(ecran, "[%s] The pilot hit an internal error again (report: %s): it stays stopped (3 restarts in an hour). Restart it from the app.\n", maintenant.Format("2006-01-02 15:04:05"), crash)
			return code
		}
		reprises = append(reprises, maintenant)
		fmt.Fprintf(ecran, "[%s] The pilot hit an internal error (code %d, report: %s): it starts again in 30 s.\n", maintenant.Format("2006-01-02 15:04:05"), code, crash)
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			if existeLanceur(filepath.Join(prive, "arreter")) || existeLanceur(filepath.Join(projet, ".athanor", "STOP")) {
				_ = os.Remove(filepath.Join(prive, "arreter"))
				fmt.Fprintln(ecran, "Pilot stopped.")
				return 0
			}
		}
	}
}

func lancerArreter(args []string) int {
	apresCycle := false
	arg := ""
	for _, a := range args {
		if a == "--apres-cycle" {
			apresCycle = true
		} else {
			arg = a
		}
	}
	kit := kitDuProgramme()
	projet := projetArgument(kit, arg)
	if st, err := os.Stat(projet); err != nil || !st.IsDir() {
		if apresCycle {
			fmt.Fprintf(os.Stderr, "Project folder not found: %s. Nothing was changed.\n", projet)
		} else {
			fmt.Println("Project folder not found.")
		}
		return 1
	}
	if apresCycle {
		etat := filepath.Join(projet, ".athanor")
		if estLien(etat) {
			_ = os.Remove(etat)
		}
		if err := os.MkdirAll(etat, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot write the stop request: "+err.Error())
			return 1
		}
		stop := filepath.Join(etat, "STOP")
		_ = os.Remove(stop)
		if err := os.WriteFile(stop, nil, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "Cannot write the stop request: "+err.Error())
			return 1
		}
		// a pilot of an older version still works in .autopilote/ (previous name): it reads its STOP there
		if a := filepath.Join(projet, ".autopilote"); dossierExistePilote(a) && !estLien(a) {
			_ = os.WriteFile(filepath.Join(a, "STOP"), nil, 0o644)
		}
		fmt.Println("OK: the pilot will stop at the end of the current cycle. To resume later: start.")
		return 0
	}
	pid := piloteVivant(kit, projet)
	if pid == 0 {
		fmt.Println("The pilot is not running on this project.")
		return 0
	}
	// the pilot sees this file within a second (every system), and a signal where there are signals
	_ = os.WriteFile(filepath.Join(priveDePilote(kit, projet, estWindows), "arreter"), nil, 0o644)
	signalerArret(pid)
	for i := 0; i < 15 && processusVivantPilote(pid); i++ {
		time.Sleep(time.Second)
	}
	if processusVivantPilote(pid) {
		terminerProcessus(pid)
		time.Sleep(time.Second)
	}
	fmt.Println("Pilot stopped.")
	return 0
}

func lancerSuivre(args []string) int {
	arg := ""
	if len(args) > 0 {
		arg = args[0]
	}
	kit := kitDuProgramme()
	projet := projetArgument(kit, arg)
	if st, err := os.Stat(projet); err != nil || !st.IsDir() {
		fmt.Println("Project folder not found.")
		return 1
	}
	prive := priveDePilote(kit, projet, estWindows)
	_ = os.MkdirAll(prive, 0o755)
	sortie := filepath.Join(prive, "output.log")
	if f, err := os.OpenFile(sortie, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.Close()
	}
	if b, err := os.ReadFile(filepath.Join(projet, ".athanor", "STATUS.txt")); err == nil {
		os.Stdout.Write(b)
	}
	fmt.Println("----")
	// tail -n 40 -F
	f, err := os.Open(sortie)
	if err != nil {
		return 1
	}
	lignes := []string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		lignes = append(lignes, sc.Text())
		if len(lignes) > 40 {
			lignes = lignes[1:]
		}
	}
	pos, _ := f.Seek(0, io.SeekCurrent)
	f.Close()
	for _, l := range lignes {
		fmt.Println(l)
	}
	for {
		time.Sleep(time.Second)
		st, err := os.Stat(sortie)
		if err != nil {
			continue
		}
		if st.Size() < pos {
			pos = 0 // rewritten
		}
		if st.Size() == pos {
			continue
		}
		f, err := os.Open(sortie)
		if err != nil {
			continue
		}
		_, _ = f.Seek(pos, io.SeekStart)
		n, _ := io.Copy(os.Stdout, f)
		pos += n
		f.Close()
	}
}

func existeLanceur(p string) bool { _, err := os.Lstat(p); return err == nil }
