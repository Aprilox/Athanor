//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// langueSystemeOS: macOS' language (its locale), English elsewhere.
func langueSystemeOS() string {
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output(); err == nil && strings.HasPrefix(strings.ToLower(string(out)), "fr") {
			return "fr"
		}
	}
	return "en"
}

// demanderConsentement: a dialog of the system: macOS' own (osascript), on Linux zenity or kdialog, else
// the question in the terminal Athanor was started from. Nobody to ask: no.
func demanderConsentement(titre, texte string) bool {
	oui, non := "Create", "Quit"
	if langueSysteme() == "fr" {
		oui, non = "Créer", "Quitter"
	}
	// the buttons here are Create / Quit (Windows' dialog says Yes / No)
	texte = strings.Replace(strings.Replace(texte, "\n\nNo: ", "\n\n"+non+": ", 1), "\n\nNon : ", "\n\n"+non+" : ", 1)
	// the terminal interface asked for: the question in the terminal, as everything else
	enTerminal := interfaceTerminal() && terminalInteractif()
	if runtime.GOOS == "darwin" && !enTerminal {
		q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
		script := "display dialog " + q(texte) + " with title " + q(titre) + " buttons {" + q(non) + ", " + q(oui) + "} default button " + q(oui) + " with icon note"
		out, err := exec.Command("osascript", "-e", script).Output()
		return err == nil && strings.Contains(string(out), oui)
	}
	if !enTerminal && (os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "") {
		if z, err := exec.LookPath("zenity"); err == nil {
			return exec.Command(z, "--question", "--title="+titre, "--text="+texte, "--ok-label="+oui, "--cancel-label="+non, "--width=520").Run() == nil
		}
		if k, err := exec.LookPath("kdialog"); err == nil {
			return exec.Command(k, "--title", titre, "--yesno", texte).Run() == nil
		}
		// no dialog program: the same question in a page of the browser (served from memory, nothing written)
		if r, ok := consentementNavigateur(titre, texte, oui, non); ok {
			return r
		}
	}
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintf(os.Stderr, "%s\n\n%s\n\n[y/N] ", titre, texte)
		l, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		l = strings.ToLower(strings.TrimSpace(l))
		return l == "y" || l == "yes" || l == "o" || l == "oui"
	}
	fmt.Fprintf(os.Stderr, "%s\n\n%s\n\nNo terminal to answer in: start Athanor again with --oui to accept.\n", titre, texte)
	return false
}

// informer: a message in a dialog of the system (macOS, zenity or kdialog), else in the terminal.
func informer(titre, texte string) {
	if runtime.GOOS == "darwin" {
		q := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
		if exec.Command("osascript", "-e", "display dialog "+q(texte)+" with title "+q(titre)+" buttons {\"OK\"} default button \"OK\" with icon note").Run() == nil {
			return
		}
	} else if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		if z, err := exec.LookPath("zenity"); err == nil && exec.Command(z, "--info", "--title="+titre, "--text="+texte, "--width=520").Run() == nil {
			return
		}
		if k, err := exec.LookPath("kdialog"); err == nil && exec.Command(k, "--title", titre, "--msgbox", texte).Run() == nil {
			return
		}
	}
	fmt.Fprintf(os.Stderr, "%s\n\n%s\n", titre, texte)
}

// consentementNavigateur: the question in a page of the default browser, served from memory on 127.0.0.1
// (a random address, closed after the answer). False, false: no browser could be opened.
func consentementNavigateur(titre, texte, oui, non string) (bool, bool) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, false
	}
	defer l.Close()
	jeton := aleatoire(16)
	reponse := make(chan bool, 1)
	page := `<!doctype html><html><head><meta charset="utf-8"><title>` + html.EscapeString(titre) + `</title><style>body{font-family:system-ui,sans-serif;background:#141b24;color:#e9eef3;display:flex;justify-content:center;padding:48px 16px}main{max-width:560px;background:#1b2430;border:1px solid #2d3a49;border-radius:12px;padding:24px 28px}h1{font-size:20px;margin:0 0 12px}p{white-space:pre-wrap;line-height:1.5;color:#c9d3dd}form{display:flex;gap:10px;justify-content:flex-end;margin-top:18px}button{font:inherit;padding:8px 16px;border-radius:6px;border:1px solid #2d3a49;background:#232e3b;color:#e9eef3;cursor:pointer}button.oui{background:#3fc6a5;border-color:#3fc6a5;color:#0e1a16;font-weight:600}</style></head><body><main><h1>` + html.EscapeString(titre) + `</h1><p>` + html.EscapeString(texte) + `</p><form method="post"><button name="r" value="non">` + html.EscapeString(non) + `</button><button class="oui" name="r" value="oui" autofocus>` + html.EscapeString(oui) + `</button></form></main></body></html>`
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/"+jeton {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			ok := r.PostForm.Get("r") == "oui"
			fin := "Athanor: you can close this page."
			if ok {
				fin = "Athanor opens."
			}
			_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><body style="font-family:system-ui;background:#141b24;color:#e9eef3;padding:48px">` + html.EscapeString(fin) + `</body>`))
			select {
			case reponse <- ok:
			default:
			}
			return
		}
		_, _ = w.Write([]byte(page))
	})}
	go func() { _ = srv.Serve(l) }()
	defer srv.Close()
	url := "http://" + l.Addr().String() + "/" + jeton
	ouvert := false
	for _, o := range []string{"xdg-open", "gio", "sensible-browser", "x-www-browser"} {
		if p, err := exec.LookPath(o); err == nil {
			args := []string{url}
			if o == "gio" {
				args = []string{"open", url}
			}
			if exec.Command(p, args...).Start() == nil {
				ouvert = true
				break
			}
		}
	}
	if !ouvert {
		return false, false
	}
	select {
	case r := <-reponse:
		time.Sleep(300 * time.Millisecond) // the last page is sent before the server closes
		return r, true
	case <-time.After(15 * time.Minute):
		return false, true
	}
}
