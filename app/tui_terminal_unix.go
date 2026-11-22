//go:build linux || darwin

package main

// The terminal interface on Linux and macOS: the terminal in raw mode on its alternate screen (given back as
// it was on the way out, whatever happens), its size, its keys and mouse.

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const sequenceEntreeTUI = "\x1b[?1049h\x1b[?1000h\x1b[?1006h\x1b[?2004h\x1b[?25l\x1b[H\x1b[2J"
const sequenceSortieTUI = "\x1b[?2004l\x1b[?1006l\x1b[?1000l\x1b[0m\x1b[?25h\x1b[?1049l"

type terminalTUI struct {
	f     *os.File
	avant syscall.Termios
	rendu bool // the terminal was handed back
}

// ioctlTUI: an ioctl on the terminal, through SyscallConn (File.Fd would put the file back in blocking
// mode, and its read could no longer be woken up while a command runs in front of the user).
func ioctlTUI(f *os.File, req uintptr, arg unsafe.Pointer) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var e syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		_, _, e = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); err != nil {
		return err
	}
	if e != 0 {
		return e
	}
	return nil
}

func ioctlTermios(f *os.File, req uintptr, t *syscall.Termios) error {
	return ioctlTUI(f, req, unsafe.Pointer(t))
}

// estTerminal: f is a terminal (it answers the termios request; /dev/null, a pipe or a file do not).
func estTerminal(f *os.File) bool {
	var t syscall.Termios
	return ioctlTermios(f, ioctlLireTermios, &t) == nil
}

func ouvrirTerminalTUI() (*terminalTUI, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	t := &terminalTUI{f: f}
	if err := ioctlTermios(f, ioctlLireTermios, &t.avant); err != nil {
		f.Close()
		return nil, err
	}
	if err := t.brut(); err != nil {
		f.Close()
		return nil, err
	}
	return t, nil
}

// brut: raw mode on the alternate screen (again after a command run in front of the user).
func (t *terminalTUI) brut() error {
	f := t.f
	brut := t.avant
	brut.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	brut.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	brut.Cflag &^= syscall.CSIZE | syscall.PARENB
	brut.Cflag |= syscall.CS8
	brut.Cc[syscall.VMIN] = 1
	brut.Cc[syscall.VTIME] = 0
	if err := ioctlTermios(f, ioctlEcrireTermios, &brut); err != nil {
		return err
	}
	_, _ = f.WriteString(sequenceEntreeTUI)
	t.rendu = false
	return nil
}

// rendre: the terminal as it was (its screen, its echo, its cursor).
func (t *terminalTUI) rendre() {
	if t.rendu {
		return
	}
	t.rendu = true
	_, _ = t.f.WriteString(sequenceSortieTUI)
	_ = ioctlTermios(t.f, ioctlEcrireTermios, &t.avant)
}

func (t *terminalTUI) taille() (int, int) {
	var ws struct{ lignes, colonnes, x, y uint16 }
	if err := ioctlTUI(t.f, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil || ws.colonnes == 0 {
		return 80, 24
	}
	return int(ws.colonnes), int(ws.lignes)
}

// vraiesCouleurs: the terminal shows 24-bit colours (what it says of itself, or a terminal known to).
func vraiesCouleurs() bool {
	if v := os.Getenv("ATHANOR_COULEURS"); v != "" {
		return v == "24" || v == "truecolor"
	}
	ct := strings.ToLower(os.Getenv("COLORTERM"))
	if ct == "truecolor" || ct == "24bit" {
		return true
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm", "vscode", "ghostty", "Hyper", "Tabby", "rio":
		return true
	}
	term := os.Getenv("TERM")
	return strings.Contains(term, "kitty") || strings.Contains(term, "ghostty") || strings.Contains(term, "alacritty") ||
		strings.Contains(term, "foot") || strings.Contains(term, "direct") || os.Getenv("WT_SESSION") != ""
}

// attendreEntree: the line typed (cooked mode: Enter ends it).
func attendreEntree(f *os.File) {
	// what was typed while the command ran is not this Enter (SyscallConn: Fd would stop the deadlines)
	if rc, err := f.SyscallConn(); err == nil {
		_ = rc.Control(func(fd uintptr) { viderEntree(int(fd)) })
	}
	b := make([]byte, 256)
	for {
		n, err := f.Read(b)
		if err != nil || bytes.ContainsAny(b[:n], "\r\n") {
			if n > 0 && b[n-1] == '\r' {
				dernierCR.Store(time.Now().UnixNano()) // its LF, if it comes apart, is not a key for Athanor
			}
			return
		}
	}
}

// dernierCR: when the last read ended with a CR. Some terminals send Enter as CR LF, and over SSH the LF
// may come in a read of its own: an LF (or NUL) right after a CR is the same Enter, never a second one.
var dernierCR atomic.Int64

// sansLFApresCR: b without the LF that finishes the Enter of the read before.
func sansLFApresCR(b []byte) []byte {
	if len(b) > 0 && (b[0] == '\n' || b[0] == 0) && time.Now().UnixNano()-dernierCR.Load() < int64(time.Second) {
		b = b[1:]
		dernierCR.Store(0) // that Enter is whole: the next LF is a key again
	}
	if len(b) > 0 {
		if b[len(b)-1] == '\r' {
			dernierCR.Store(time.Now().UnixNano())
		} else {
			dernierCR.Store(0)
		}
	}
	return b
}

// lancerTUI: the terminal interface on the server reached through this single-use address. proprio: this
// process serves the application (its messages then go to journal, never onto the screen).
func lancerTUI(lien string, proprio bool, journal string) int {
	c, err := nouveauClientTUI(lien)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Athanor:", err)
		return 1
	}
	term, err := ouvrirTerminalTUI()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Athanor: no terminal here (", err, "): start Athanor with gui for its window.")
		return 1
	}
	// what Athanor and its programs print would break the screen: into the log file meanwhile
	if proprio && journal != "" {
		if f, err := os.OpenFile(journal, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintf(f, "\n=== %s Athanor %s, terminal interface ===\n", time.Now().Format("2006-01-02 15:04:05"), versionAthanor)
			_ = dupliquerSortie(int(f.Fd()), 1)
			_ = dupliquerSortie(int(f.Fd()), 2)
		}
	}
	avantSortie = func() {
		term.rendre()
		fmt.Fprintln(term.f, "Athanor is being updated: start it again (athanor tui) once it is done.")
	}
	defer term.rendre()
	defer func() {
		if r := recover(); r != nil {
			term.rendre()
			panic(r)
		}
	}()

	app := nouvelleAppTUI(c, proprio)
	app.l, app.h = term.taille()
	vraies := vraiesCouleurs()

	// the size
	tailles := make(chan [2]int, 4)
	sigTaille := make(chan os.Signal, 4)
	signal.Notify(sigTaille, syscall.SIGWINCH)
	defer signal.Stop(sigTaille)
	go func() {
		for range sigTaille {
			l, h := term.taille()
			select {
			case tailles <- [2]int{l, h}:
			default:
			}
		}
	}()
	// a stop asked from outside (the terminal closed, kill): the terminal given back first
	sigFin := make(chan os.Signal, 2)
	signal.Notify(sigFin, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	defer signal.Stop(sigFin)

	// the keys: read in the background; a lone Escape is told from a sequence by a short wait
	evs := make(chan evenementTUI, 64)
	octets := make(chan []byte, 16)
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := term.f.Read(b)
			if n > 0 {
				octets <- append([]byte(nil), b[:n]...)
			}
			if err != nil {
				close(octets)
				return
			}
		}
	}()
	go func() {
		var tampon []byte
		attente := time.NewTimer(time.Hour)
		attente.Stop()
		for {
			select {
			case b, ok := <-octets:
				if !ok {
					close(evs)
					return
				}
				tampon = append(tampon, sansLFApresCR(b)...)
				var l []evenementTUI
				l, tampon = analyserTouches(tampon, false)
				for _, e := range l {
					evs <- e
				}
				if len(tampon) > 0 {
					attente.Reset(40 * time.Millisecond)
				}
			case <-attente.C:
				l, _ := analyserTouches(tampon, true)
				tampon = nil
				for _, e := range l {
					evs <- e
				}
			case <-sigFin:
				app.majs <- func(a *appTUI) { a.quitter = true }
			}
		}
	}()

	var prec *ecranT
	dessiner := func() {
		e := nouvelEcran(app.l, app.h)
		app.dessiner(e)
		_, _ = term.f.Write(e.rendu(prec, vraies))
		prec = e
	}
	app.boucle(evs, tailles, dessiner)
	term.rendre()
	return 0
}
