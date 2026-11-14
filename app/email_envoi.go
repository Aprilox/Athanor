package main

// The e-mail module's test send (M203): one message through an account, its result in plain words on the
// account's card and in the bell when it failed. The password is read from the vault for the send only.

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ResultatEnvoiEmail: the last test send of an account (a fixed message, never the server's raw answer).
type ResultatEnvoiEmail struct {
	T            string `json:"t"`
	OK           bool   `json:"ok"`
	Message      string `json:"message"` // English, translated by the page
	Destinataire string `json:"destinataire"`
}

// erreurEnvoi: why a send failed, in words the owner can act on (translated by the page).
type erreurEnvoi string

func (e erreurEnvoi) Error() string { return string(e) }

const (
	msgEnvoiOK           = "The test e-mail was sent: check the recipient's inbox (and its spam folder)"
	errEnvoiConnexion    = erreurEnvoi("Athanor could not reach the SMTP server: check the server name, the port and your connection")
	errEnvoiDelai        = erreurEnvoi("The SMTP server did not answer within 20 seconds")
	errEnvoiTLS          = erreurEnvoi("The secure connection (TLS) with the SMTP server failed: check the security setting and the port")
	errEnvoiTLSAbsent    = erreurEnvoi("The SMTP server does not offer STARTTLS: choose TLS (port 465) or check the port")
	errEnvoiIdentifiants = erreurEnvoi("The SMTP server refused the user name or the password")
	errEnvoiExpediteur   = erreurEnvoi("The SMTP server refused the sender address")
	errEnvoiDestinataire = erreurEnvoi("The SMTP server refused the recipient address")
	errEnvoiMessage      = erreurEnvoi("The SMTP server refused the message")
	errEnvoiDest         = erreurEntree("The recipient is not a valid e-mail address")
	errEnvoiTrop         = "Too many test e-mails: wait a minute"
)

// errsEnvoi: every message of the test send that reaches the page (TestEmailComptesTraduction).
var errsEnvoi = []string{msgEnvoiOK, errEnvoiConnexion.Error(), errEnvoiDelai.Error(), errEnvoiTLS.Error(),
	errEnvoiTLSAbsent.Error(), errEnvoiIdentifiants.Error(), errEnvoiExpediteur.Error(), errEnvoiDestinataire.Error(),
	errEnvoiMessage.Error(), errEnvoiDest.Error(), errEnvoiTrop}

var (
	delaiEnvoiEmail = 20 * time.Second
	racinesEmail    *x509.CertPool // nil: the system's; the tests trust their fake server
	muTestsEmail    sync.Mutex
	testsEmail      []time.Time // the test sends of the last minute (5 at most)
)

// testEmailPermis: false once 5 test sends were made in the last minute.
func testEmailPermis(maintenant time.Time) bool {
	muTestsEmail.Lock()
	defer muTestsEmail.Unlock()
	testsEmail = slices.DeleteFunc(testsEmail, func(t time.Time) bool { return maintenant.Sub(t) >= time.Minute })
	if len(testsEmail) >= 5 {
		return false
	}
	testsEmail = append(testsEmail, maintenant)
	return true
}

// classerEnvoi: a timeout is told as such, any other error as defaut.
func classerEnvoi(err error, defaut erreurEnvoi) erreurEnvoi {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errEnvoiDelai
	}
	return defaut
}

// authLogin: AUTH LOGIN, for the servers that do not offer PLAIN (Microsoft 365); never over clear text but to this PC.
type authLogin struct{ u, p string }

func (a authLogin) Start(s *smtp.ServerInfo) (string, []byte, error) {
	if !s.TLS && !hoteLocal(s.Name) {
		return "", nil, errors.New("unencrypted connection")
	}
	return "LOGIN", nil, nil
}

func (a authLogin) Next(de []byte, encore bool) ([]byte, error) {
	if !encore {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(string(de))) {
	case "username:":
		return []byte(a.u), nil
	case "password:":
		return []byte(a.p), nil
	}
	return nil, errors.New("unexpected server challenge")
}

// envoyerEmail: one plain-text message through the account c, within delaiEnvoiEmail.
func envoyerEmail(c CompteEmail, mdp, dest, sujet, corps string) error {
	de, err := mail.ParseAddress(c.Expediteur)
	if err != nil {
		return errEnvoiExpediteur
	}
	limite := time.Now().Add(delaiEnvoiEmail)
	conn, err := (&net.Dialer{Deadline: limite}).Dial("tcp", net.JoinHostPort(c.Hote, strconv.Itoa(c.Port)))
	if err != nil {
		return classerEnvoi(err, errEnvoiConnexion)
	}
	defer conn.Close()
	_ = conn.SetDeadline(limite)
	conf := &tls.Config{ServerName: c.Hote, RootCAs: racinesEmail, MinVersion: tls.VersionTLS12}
	if c.Securite == "tls" {
		tc := tls.Client(conn, conf)
		if err := tc.Handshake(); err != nil {
			return classerEnvoi(err, errEnvoiTLS)
		}
		conn = tc
	}
	cl, err := smtp.NewClient(conn, c.Hote)
	if err != nil {
		return classerEnvoi(err, errEnvoiConnexion)
	}
	defer cl.Close()
	if c.Securite == "starttls" {
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errEnvoiTLSAbsent
		}
		if err := cl.StartTLS(conf); err != nil {
			return classerEnvoi(err, errEnvoiTLS)
		}
	}
	if c.Utilisateur != "" {
		var a smtp.Auth = smtp.PlainAuth("", c.Utilisateur, mdp, c.Hote)
		if _, mecas := cl.Extension("AUTH"); !strings.Contains(" "+strings.ToUpper(mecas)+" ", " PLAIN ") && strings.Contains(" "+strings.ToUpper(mecas)+" ", " LOGIN ") {
			a = authLogin{c.Utilisateur, mdp}
		}
		if err := cl.Auth(a); err != nil {
			return classerEnvoi(err, errEnvoiIdentifiants)
		}
	}
	if err := cl.Mail(de.Address); err != nil {
		return classerEnvoi(err, errEnvoiExpediteur)
	}
	if err := cl.Rcpt(dest); err != nil {
		return classerEnvoi(err, errEnvoiDestinataire)
	}
	wc, err := cl.Data()
	if err != nil {
		return classerEnvoi(err, errEnvoiMessage)
	}
	msg := "From: " + de.String() + "\r\nTo: <" + dest + ">\r\nSubject: " + mime.QEncoding.Encode("utf-8", sujet) +
		"\r\nDate: " + time.Now().Format(time.RFC1123Z) + "\r\nMessage-ID: <" + aleatoire(12) + "@athanor.local>" +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" +
		strings.ReplaceAll(corps, "\n", "\r\n") + "\r\n"
	if _, err := wc.Write([]byte(msg)); err != nil {
		return classerEnvoi(err, errEnvoiMessage)
	}
	if err := wc.Close(); err != nil {
		return classerEnvoi(err, errEnvoiMessage)
	}
	_ = cl.Quit()
	return nil
}

// apiEmailTester: POST /api/email/comptes/{id}/test {destinataire}, one test e-mail; 200 with its result.
func (s *Serveur) apiEmailTester(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	var corps struct {
		Destinataire string `json:"destinataire"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := lireCorps(r, &corps); err != nil {
		s.erreurCoffre(w, errEmailRequete)
		return
	}
	a, err := mail.ParseAddress(strings.TrimSpace(corps.Destinataire))
	if err != nil || a.Address == "" || len(a.Address) > 320 {
		s.erreurCoffre(w, errEnvoiDest)
		return
	}
	dest := a.Address
	liste, err := lireComptesEmail()
	if err != nil {
		s.erreur(w, http.StatusInternalServerError, errEmailComptes)
		return
	}
	i := slices.IndexFunc(liste, func(x CompteEmail) bool { return x.ID == r.PathValue("id") })
	if i < 0 {
		s.erreur(w, http.StatusNotFound, errEmailInconnu.Error())
		return
	}
	compte := liste[i]
	mdp, err := s.coffre.Valeur(compte.Entree)
	if err != nil {
		s.erreurCoffre(w, err) // a closed code key: 423
		return
	}
	if !testEmailPermis(time.Now()) {
		s.erreur(w, http.StatusTooManyRequests, errEnvoiTrop)
		return
	}
	res := ResultatEnvoiEmail{OK: true, Message: msgEnvoiOK, Destinataire: dest}
	err = envoyerEmail(compte, mdp, dest, "Athanor — test e-mail",
		"This is a test e-mail sent by Athanor through the account \""+compte.Nom+"\".\nIf you can read it, this account can send e-mails.")
	if err != nil {
		res.OK, res.Message = false, err.Error()
		if !errors.As(err, new(erreurEnvoi)) {
			res.Message = errEnvoiMessage.Error()
		}
	}
	res.T = time.Now().UTC().Format(time.RFC3339)
	// the result on the account's card (read again: the account may have changed during the send)
	muComptesEmail.Lock()
	if l, err := lireComptesEmail(); err == nil {
		if j := slices.IndexFunc(l, func(x CompteEmail) bool { return x.ID == compte.ID }); j >= 0 {
			l[j].DernierTest = &res
			_ = ecrireComptesEmail(l)
		}
	}
	muComptesEmail.Unlock()
	etat := "ok"
	if !res.OK {
		etat = res.Message
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "email", Action: "test d'envoi e-mail",
		Resultat: compte.Nom + " → " + dest + " : " + etat})
	s.ecrireJSON(w, res)
}
