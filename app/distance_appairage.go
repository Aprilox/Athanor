package main

// Pairing in two steps (distance.go). The QR code, or its link, only opens a request: the device must
// then type 6 digits that Athanor's window shows under the mouse only, renewed every 20 seconds. A photo
// or a screenshot of the QR code is not enough: one must see the computer's screen at that moment. Three
// wrong codes end the request (scan again), and every wrong code counts towards the pause of pairing.

import (
	"crypto/rand"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"strings"
	"time"
)

const (
	cookieAppairage = "athanor_appairage"
	dureeChiffres   = 20 * time.Second
	graceChiffres   = 10 * time.Second // the code that just changed is still accepted this long (typed at the change)
	essaisChiffres  = 3
	dureeDemande    = 5 * time.Minute
	demandesMax     = 10
)

// demandeAppairage: a device that opened the pairing link and has to type the computer's code.
type demandeAppairage struct {
	fin    time.Time
	essais int
}

func sixChiffres() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return aleatoire(3)[:6]
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// codeChiffres: the 6 digits the window shows now, and their end (new ones every 20 seconds).
func (s *Serveur) codeChiffres() (string, time.Time) {
	d := s.dist()
	d.mu.Lock()
	defer d.mu.Unlock()
	if maintenant := time.Now(); d.chiffres == "" || !maintenant.Before(d.chiffresFin) {
		d.chiffresAvant, d.chiffresAvantFin = d.chiffres, d.chiffresFin
		d.chiffres, d.chiffresFin = sixChiffres(), maintenant.Add(dureeChiffres)
	}
	return d.chiffres, d.chiffresFin
}

// chiffresValides: the digits typed are the window's (or the ones that changed a few seconds ago).
// d.mu held.
func (d *distance) chiffresValides(c string) bool {
	maintenant := time.Now()
	if len(c) != 6 {
		return false
	}
	if d.chiffres != "" && maintenant.Before(d.chiffresFin) && egal(c, d.chiffres) {
		return true
	}
	return d.chiffresAvant != "" && maintenant.Before(d.chiffresAvantFin.Add(graceChiffres)) && egal(c, d.chiffresAvant)
}

// ouvrirDemande: the QR code's code was right: the device now has to type the computer's digits.
func (s *Serveur) ouvrirDemande(w http.ResponseWriter) {
	d := s.dist()
	d.mu.Lock()
	for k, x := range d.demandes {
		if time.Now().After(x.fin) {
			delete(d.demandes, k)
		}
	}
	if len(d.demandes) >= demandesMax { // the oldest one goes
		var vieille string
		for k, x := range d.demandes {
			if vieille == "" || x.fin.Before(d.demandes[vieille].fin) {
				vieille = k
			}
		}
		delete(d.demandes, vieille)
	}
	id := aleatoire(24)
	d.demandes[id] = &demandeAppairage{fin: time.Now().Add(dureeDemande)}
	d.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieAppairage, Value: id, Path: routeAppairer, MaxAge: int(dureeDemande.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	s.pageChiffres(w, http.StatusOK, "", 0)
}

// appairer: GET with the QR code's code opens a request; POST with the computer's digits ends it.
func (s *Serveur) appairer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		if !s.consommerCode(r.URL.Query().Get("c")) {
			time.Sleep(400 * time.Millisecond)
			s.pageDistance(w, http.StatusForbidden, "expire")
			return
		}
		s.ouvrirDemande(w)
		return
	}
	// a form of this page only (the browser names where it comes from)
	if o := r.Header.Get("Origin"); o != "" && !strings.EqualFold(o, "https://"+s.hoteTunnel()) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	c, err := r.Cookie(cookieAppairage)
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	_ = r.ParseForm()
	chiffres := strings.Map(func(x rune) rune {
		if x >= '0' && x <= '9' {
			return x
		}
		return -1
	}, r.PostFormValue("chiffres"))
	d := s.dist()
	d.mu.Lock()
	var dem *demandeAppairage
	if err == nil {
		dem = d.demandes[c.Value]
	}
	if dem == nil || time.Now().After(dem.fin) {
		if err == nil {
			delete(d.demandes, c.Value)
		}
		d.mu.Unlock()
		s.pageDistance(w, http.StatusForbidden, "expire")
		return
	}
	if d.chiffresValides(chiffres) {
		delete(d.demandes, c.Value)
		d.mu.Unlock()
		http.SetCookie(w, &http.Cookie{Name: cookieAppairage, Value: "", Path: routeAppairer, MaxAge: -1})
		s.creerAppareil(w, r)
		return
	}
	dem.essais++
	d.echecs = append(d.echecs, time.Now())
	reste := essaisChiffres - dem.essais
	if reste <= 0 {
		delete(d.demandes, c.Value)
	}
	d.mu.Unlock()
	time.Sleep(400 * time.Millisecond)
	if reste <= 0 {
		s.pageDistance(w, http.StatusForbidden, "essais")
		return
	}
	s.pageChiffres(w, http.StatusUnauthorized, "faux", reste)
}

// textesChiffres: the page that asks for the computer's digits, in Athanor's language.
var textesChiffres = map[string][6]string{
	// title, explanation, button, wrong code ({0}: attempts left), too many wrong codes, label
	"en": {"Code of the computer", "On your computer, in Athanor › Remote, move the mouse over the code to show it, then type it here. It changes every 20 seconds.", "Pair this device", "Wrong code: %d attempt(s) left.", "Too many wrong codes. On your computer, show the QR code again and scan it.", "6-digit code"},
	"fr": {"Code de l'ordinateur", "Sur votre ordinateur, dans Athanor › À distance, passez la souris sur le code pour l'afficher, puis tapez-le ici. Il change toutes les 20 secondes.", "Appairer cet appareil", "Code faux : encore %d essai(s).", "Trop de codes faux. Sur votre ordinateur, affichez à nouveau le QR code et scannez-le.", "Code à 6 chiffres"},
	"es": {"Código del ordenador", "En su ordenador, en Athanor › A distancia, pase el ratón por el código para verlo y escríbalo aquí. Cambia cada 20 segundos.", "Vincular este dispositivo", "Código incorrecto: quedan %d intento(s).", "Demasiados códigos incorrectos. En su ordenador, vuelva a mostrar el código QR y escanéelo.", "Código de 6 cifras"},
	"de": {"Code des Computers", "Fahren Sie auf Ihrem Computer in Athanor › Fernzugriff mit der Maus über den Code, um ihn zu sehen, und geben Sie ihn hier ein. Er ändert sich alle 20 Sekunden.", "Dieses Gerät koppeln", "Falscher Code: noch %d Versuch(e).", "Zu viele falsche Codes. Zeigen Sie den QR-Code auf Ihrem Computer erneut an und scannen Sie ihn.", "6-stelliger Code"},
	"it": {"Codice del computer", "Sul computer, in Athanor › A distanza, passa il mouse sul codice per vederlo, poi scrivilo qui. Cambia ogni 20 secondi.", "Associa questo dispositivo", "Codice errato: ancora %d tentativo/i.", "Troppi codici errati. Sul computer mostra di nuovo il codice QR e scansionalo.", "Codice a 6 cifre"},
	"pt": {"Código do computador", "No computador, no Athanor › À distância, passe o rato sobre o código para o ver e escreva-o aqui. Muda a cada 20 segundos.", "Associar este dispositivo", "Código errado: restam %d tentativa(s).", "Demasiados códigos errados. No computador, volte a mostrar o código QR e leia-o.", "Código de 6 dígitos"},
}

func (s *Serveur) pageChiffres(w http.ResponseWriter, code int, erreur string, reste int) {
	s.mu.Lock()
	langue := s.config.Langue
	s.mu.Unlock()
	t, ok := textesChiffres[langue]
	if !ok {
		langue, t = "en", textesChiffres["en"]
	}
	msg := ""
	if erreur == "faux" {
		msg = `<p style="color:#ff8a80;margin:0 0 12px">` + html.EscapeString(fmt.Sprintf(t[3], reste)) + `</p>`
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, `<!doctype html><html lang="%s"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Athanor</title>
<body style="margin:0;font:17px/1.5 system-ui,sans-serif;background:#14181f;color:#e6e8eb;display:flex;min-height:100vh;align-items:center;justify-content:center">
<form method="post" action="%s" style="max-width:420px;width:100%%;padding:28px;box-sizing:border-box"><div style="font-size:40px">🔐</div><h1 style="font-size:22px;margin:8px 0 12px">%s</h1>
<p style="color:#9aa4b1;margin:0 0 18px">%s</p>%s
<label style="display:block;font-size:14px;color:#9aa4b1;margin-bottom:6px" for="chiffres">%s</label>
<input id="chiffres" name="chiffres" inputmode="numeric" pattern="[0-9 ]*" autocomplete="one-time-code" maxlength="7" required autofocus
 style="box-sizing:border-box;width:100%%;font:600 30px/1.2 ui-monospace,monospace;letter-spacing:.3em;text-align:center;padding:12px;border-radius:10px;border:1px solid #2d3a49;background:#0e131a;color:#e6e8eb">
<button style="margin-top:16px;width:100%%;padding:14px;border:0;border-radius:10px;background:#3fc6a5;color:#08221b;font:600 17px system-ui,sans-serif">%s</button></form>`,
		html.EscapeString(langue), routeAppairer, html.EscapeString(t[0]), html.EscapeString(t[1]), msg, html.EscapeString(t[5]), html.EscapeString(t[2]))
}

// texteNouvelAppareil: the system notification of the computer when a device is paired.
func texteNouvelAppareil(langue, nom string) (string, string) {
	switch langue {
	case "fr":
		return "Athanor : nouvel appareil appairé", nom + " peut maintenant piloter Athanor. Si ce n'est pas vous, retirez-le dans Athanor › À distance."
	case "es":
		return "Athanor: nuevo dispositivo vinculado", nom + " ya puede controlar Athanor. Si no es usted, quítelo en Athanor › A distancia."
	case "de":
		return "Athanor: neues Gerät gekoppelt", nom + " kann Athanor jetzt steuern. Wenn Sie das nicht sind, entfernen Sie es in Athanor › Fernzugriff."
	case "it":
		return "Athanor: nuovo dispositivo associato", nom + " ora può guidare Athanor. Se non sei tu, toglilo in Athanor › A distanza."
	case "pt":
		return "Athanor: novo dispositivo associado", nom + " já pode controlar o Athanor. Se não foi você, retire-o em Athanor › À distância."
	}
	return "Athanor: new device paired", nom + " can now drive Athanor. If it is not you, remove it in Athanor › Remote."
}

// texteAppareilRetireMdp: the computer's notification when a device typed too many wrong passwords.
func texteAppareilRetireMdp(langue, nom string) (string, string) {
	switch langue {
	case "fr":
		return "Athanor : appareil retiré", nom + " a tapé 5 mots de passe faux : il n'est plus appairé. Si c'était vous, scannez à nouveau le QR code."
	case "es":
		return "Athanor: dispositivo quitado", nom + " escribió 5 contraseñas incorrectas: ya no está vinculado. Si era usted, vuelva a escanear el código QR."
	case "de":
		return "Athanor: Gerät entfernt", nom + " hat 5 falsche Passwörter eingegeben: Es ist nicht mehr gekoppelt. Wenn Sie es waren, scannen Sie den QR-Code erneut."
	case "it":
		return "Athanor: dispositivo tolto", nom + " ha scritto 5 password errate: non è più associato. Se eri tu, scansiona di nuovo il codice QR."
	case "pt":
		return "Athanor: dispositivo retirado", nom + " escreveu 5 palavras-passe erradas: já não está associado. Se foi você, volte a ler o código QR."
	}
	return "Athanor: device removed", nom + " typed 5 wrong passwords: it is no longer paired. If it was you, scan the QR code again."
}
