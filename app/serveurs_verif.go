package main

// The two other Servers agents of a task (VISION.md §6.2, M98): the security critic reads every plan before the
// owner sees it (ports, root, secrets, backups) and its verdict goes on the plan's card; the external verifier
// tests the result as a visitor once a plan was applied (verifier_http: status, TLS and its expiry, security
// headers, a password asked) and its report goes to the RUNBOOK and to the task, a failure turns the task to echec
// and rings the bell. Both are short sessions of their own, with read-only tools and a token of their own, in a
// project of their own (no plan nor "do not ask again" rule of the task applies to them).

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CritiquePlan: the security critic's verdict on a plan; Verdict "" while it reads it (accepting waits).
type CritiquePlan struct {
	Verdict string   `json:"verdict"` // "" | "ok" | "risques"
	Lignes  []string `json:"lignes,omitempty"`
}

// VerificationTache: the external verifier's report on a task's applied plans.
type VerificationTache struct {
	Verdict  string           `json:"verdict"` // "ok" | "echec"
	Lignes   []string         `json:"lignes,omitempty"`
	Criteres []CritereVerifie `json:"criteres,omitempty"` // each acceptance criterion and whether it holds
	T        string           `json:"t"`
}

var (
	outilsCritique     = []string{"serveurs_liste", "serveur_fiche", "fichier_lire", "runbook_lire", "ssh_commande", "service", "docker", "pare_feu"}
	outilsVerificateur = append(slices.Clone(outilsCritique), "verifier_http")
	errCritiqueEnCours = errors.New("the security critic is still reading this plan: accept it once its verdict is on the card (refusing is possible now)")
	racinesVerif       *x509.CertPool // nil: the system's roots (tests set theirs)
)

// critiqueEnCours: plan id is a Servers plan whose security critique has not come yet.
func (s *Serveur) critiqueEnCours(id string) bool {
	p, err := s.lirePlan(id)
	return err == nil && p.Critique != nil && p.Critique.Verdict == ""
}

// planEnDonnees: a plan as the critic and the verifier read it: its steps with their arguments masked, never cut, as data.
func (s *Serveur) planEnDonnees(p PlanServeur) string {
	etapes := []map[string]any{}
	for i, e := range p.Etapes {
		etapes = append(etapes, map[string]any{"n": i + 1, "outil": e.Outil, "arguments": s.masquerSecrets(string(e.Arguments)), // whole (C51): nothing hidden past a cut
			"classe": e.Classe, "pourquoi": e.Pourquoi, "sauvegarde": e.Sauvegarde, "sans_sauvegarde": e.SansSauvegarde})
	}
	return enDonnees("plan", fmt.Sprintf(` id="%s" server="%s" env="%s"`, p.ID, p.Serveur, p.Env),
		enJSON(map[string]any{"resume": p.Resume, "etapes": etapes, "criteres": p.Criteres}))
}

func consigneCritique(plan string) string {
	return "You are Athanor's security critic. Another agent proposed the plan below for the owner's server; the owner decides " +
		"after reading your verdict. Your tools are athanor-hub read-only ones (the server's sheet, its runbook, files, status " +
		"commands): use them to check what the plan touches. You change nothing and nobody answers questions.\n\n" +
		"Look for: ports opened to everyone (databases, admin panels, SSH), root or sudo where it is not needed, secrets written " +
		"in clear (files, command lines, environment), destructive steps without a backup, a way back that is missing.\n\n" +
		"Answer with exactly this form and nothing before it:\nVERDICT: ok\nor\nVERDICT: risques\n- one risk per line, plain and short\n\n" +
		plan
}

func consigneVerificateur(texte string, plans []string) string {
	return "You are Athanor's external verifier. The owner asked: <owner-request>" + strings.ReplaceAll(texte, "<", "&lt;") +
		"</owner-request>\nThe plans below were applied. Test the result as a visitor would: verifier_http on the addresses " +
		"concerned (status, TLS and its expiry, security headers, whether a password is asked when it should be), and " +
		"read-only checks on the server (the service runs, the port listens). Check every acceptance criterion of the plans (criteres): " +
		"the result passes only when each holds. You change nothing and nobody answers questions.\n\n" +
		"Answer with exactly this form and nothing before it:\nVERIFICATION: ok\nor\nVERIFICATION: echec\n" +
		"- [ok] <a criterion, as written>: what you saw (one line per criterion; [echec] for one that does not hold)\n- any other finding, one per line, plain and short\n\n" +
		strings.Join(plans, "\n\n")
}

// lireVerdict: the value after "MOT:" in an agent's answer (one of valeurs, "" when none) and its "- " lines.
func (s *Serveur) lireVerdict(texte, mot string, valeurs []string) (string, []string) {
	verdict, lignes := "", []string{}
	for _, l := range strings.Split(texte, "\n") {
		l = strings.TrimSpace(strings.Trim(l, "*` "))
		if k, v, ok := strings.Cut(l, ":"); ok && verdict == "" && strings.EqualFold(strings.TrimSpace(k), mot) {
			if v = strings.ToLower(strings.TrimSpace(v)); slices.Contains(valeurs, v) {
				verdict = v
			}
			continue
		}
		if verdict != "" && len(lignes) < 12 && (strings.HasPrefix(l, "- ") || strings.HasPrefix(l, "• ")) {
			lignes = append(lignes, couper(s.masquerSecrets(strings.TrimSpace(strings.Trim(l[2:], "*` "))), 300))
		}
	}
	return verdict, lignes
}

// sessionAnnexe: a short session of the task (the critic, the verifier) in dossier/nom, with its own token of
// outils, valid while it runs; its last answer, or why it failed.
func (s *Serveur) sessionAnnexe(ctx context.Context, e *tacheEnCours, dossier, nom, consigne string, outils []string) (string, error) {
	d := filepath.Join(dossier, nom)
	defer os.RemoveAll(d)
	if err := os.MkdirAll(filepath.Join(d, "travail"), 0o700); err != nil {
		return "", err
	}
	projet := prefixeTache + e.t.ID + ":" + nom
	jeton, err := s.jetons.CreerSession(SessionHub{Projet: projet, Mission: e.t.ID, Agent: agentServeurs, Outils: outils, Serveurs: e.t.Serveurs})
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	e.annexe = hacherJeton(jeton)
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.annexe = ""
		e.mu.Unlock()
		s.revoquerJetons("session "+nom+" finie", func(x SessionHub) bool { return x.Projet == projet })
	}()
	for f, v := range map[string]string{"hub-jeton": jeton + "\n", "consigne.txt": consigne, "reprise": ""} {
		if err := os.WriteFile(filepath.Join(d, f), []byte(v), 0o600); err != nil {
			return "", err
		}
	}
	err = sessionTacheServeurs(s, ctx, d)
	_, resultat, enErreur := lireSessionClaude(filepath.Join(d, "session.jsonl"))
	if err == nil && enErreur {
		err = errors.New("the session ended with an error: " + couper(resultat, 200))
	}
	return resultat, err
}

// critiquerPlan: the security critic reads plan before the owner sees it; its verdict is kept on the plan, then
// the owner's devices are told (demanderApprobation leaves a task's plan to this). No readable verdict: risks.
func (s *Serveur) critiquerPlan(ctx context.Context, e *tacheEnCours, dossier, plan string) {
	p, err := s.lirePlan(plan)
	if err != nil {
		return
	}
	res, err := s.sessionAnnexe(ctx, e, dossier, "critique", consigneCritique(s.planEnDonnees(p)), outilsCritique)
	verdict, lignes := s.lireVerdict(res, "verdict", []string{"ok", "risques"})
	if err != nil || verdict == "" {
		pourquoi := "no verdict in its answer"
		if err != nil {
			pourquoi = s.masquerSecrets(err.Error())
		}
		verdict, lignes = "risques", append([]string{couper("The security critic could not judge this plan ("+pourquoi+"): read it with care.", 300)}, lignes...)
	}
	verrouPlans.Lock()
	if p, err = s.lirePlan(plan); err == nil {
		p.Critique = &CritiquePlan{Verdict: verdict, Lignes: lignes}
		err = s.enregistrerPlan(p)
	}
	verrouPlans.Unlock()
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "critique", Vers: "serveur:" + p.Serveur, Projet: prefixeTache + e.t.ID, Mission: e.t.ID,
		Agent: agentServeurs, Action: "serveurs.critique", Resultat: "plan " + plan + " → " + verdict + " " + couper(strings.Join(lignes, " · "), 200)})
	if ap, ok := s.approbations.Lire(plan); ok && ap.Etat == "attente" && err == nil {
		s.notifierApprobation(ap)
	}
}

// verifierTache: the external verifier on the task's applied plans; its report goes to the task and to each
// server's RUNBOOK. No readable report: a failure (nothing says the result works).
func (s *Serveur) verifierTache(ctx context.Context, e *tacheEnCours, dossier string, appliques []string) VerificationTache {
	var plans []string
	for _, id := range appliques {
		if p, err := s.lirePlan(id); err == nil {
			plans = append(plans, s.planEnDonnees(p))
		}
	}
	res, err := s.sessionAnnexe(ctx, e, dossier, "verificateur", consigneVerificateur(e.t.Texte, plans), outilsVerificateur)
	v := VerificationTache{T: time.Now().UTC().Format(time.RFC3339)}
	v.Verdict, v.Lignes = s.lireVerdict(res, "verification", []string{"ok", "echec"})
	v.Criteres = criteresVerifies(v.Lignes)
	if err != nil || v.Verdict == "" {
		pourquoi := "no report in its answer"
		if err != nil {
			pourquoi = s.masquerSecrets(err.Error())
		}
		v.Verdict, v.Lignes = "echec", append([]string{couper("The external verifier could not check the result ("+pourquoi+").", 300)}, v.Lignes...)
	}
	e.mu.Lock()
	e.t.Verification = &v
	e.mu.Unlock()
	texte := "Verification " + map[bool]string{true: "passed", false: "FAILED"}[v.Verdict == "ok"] + " (plans " + strings.Join(appliques, ", ") + ")"
	for _, l := range v.Lignes {
		texte += "\n- " + l
	}
	for _, id := range e.t.Serveurs {
		_ = ajouterRunbook(id, "historique", "external verifier", texte, 0)
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "verificateur", Vers: "servers", Projet: prefixeTache + e.t.ID, Mission: e.t.ID,
		Agent: agentServeurs, Action: "serveurs.verification", Resultat: v.Verdict + " " + couper(strings.Join(v.Lignes, " · "), 200)})
	return v
}

// ----------------------------------------------------------------------------- verifier_http

// ResultatHTTP: what a visitor sees of an address.
type ResultatHTTP struct {
	URL              string            `json:"url"`
	Statut           int               `json:"statut,omitempty"`
	Redirection      string            `json:"redirection,omitempty"` // not followed: Location
	Authentification string            `json:"authentification"`      // "demandee" (401, WWW-Authenticate) | "aucune"
	WWWAuthenticate  string            `json:"www_authenticate,omitempty"`
	TLS              *InfoTLS          `json:"tls,omitempty"`
	EnTetes          map[string]string `json:"en_tetes_securite"`
	Manquants        []string          `json:"en_tetes_manquants"`
	Erreur           string            `json:"erreur,omitempty"`
}

type InfoTLS struct {
	Version       string `json:"version"`
	Valide        bool   `json:"certificat_valide"`
	Erreur        string `json:"erreur_certificat,omitempty"`
	Sujet         string `json:"sujet"`
	Emetteur      string `json:"emetteur"`
	Expire        string `json:"expire"`
	JoursRestants int    `json:"jours_restants"`
}

var entetesSecurite = []string{"Strict-Transport-Security", "Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Permissions-Policy"}

func init() {
	outilsHub["verifier_http"] = OutilHub{Description: "Visit an address of your servers as a visitor would (GET, redirects not followed): status, TLS version, certificate validity and expiry, security headers, whether a password is asked.",
		Schema: schemaObjet(map[string]any{"url": schemaTexte("http:// or https:// address whose host is one of your servers (its address or a name pointing to it), or a project's site address")}, "url"),
		Faire:  outilVerifierHTTP}
}

func outilVerifierHTTP(s *Serveur, x SessionHub, args json.RawMessage) (string, error) {
	var a struct {
		URL string `json:"url"`
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	if err := s.accesServeursAgent(x); err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimSpace(a.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", errors.New("url must be an http:// or https:// address, without a user or password in it")
	}
	liste, err := lireServeurs()
	if err != nil {
		return "", err
	}
	var hotes []string
	for _, v := range liste {
		if slices.Contains(x.Serveurs, v.ID) {
			hotes = append(hotes, v.Hote)
		}
	}
	ctx, fin := context.WithTimeout(s.contexteSession(x.Hache), 20*time.Second)
	defer fin()
	r := verifierHTTP(ctx, u, hotes, s.adresseDeploiement(u.Hostname())) // a site's own address: through its CDN or proxy too
	s.ecrireAudit(LigneAudit{Type: "evenement", De: x.Agent, Vers: "url:" + couper(u.Host, 80), Projet: x.Projet, Mission: x.Mission,
		Agent: x.Agent, Action: "serveurs.verifier_http", Resultat: fmt.Sprintf("%d %s", r.Statut, couper(r.Erreur, 120))})
	return enDonnees("server-output", ` tool="verifier_http"`, enJSON(r)), nil
}

// adressesPermises: the IPs of hotes (a name resolved), the only ones verifier_http connects to.
func adressesPermises(ctx context.Context, hotes []string) map[string]bool {
	ok := map[string]bool{}
	for _, h := range hotes {
		if ip := net.ParseIP(h); ip != nil {
			ok[ip.String()] = true
			continue
		}
		ips, _ := net.DefaultResolver.LookupIPAddr(ctx, h)
		for _, ip := range ips {
			ok[ip.IP.String()] = true
		}
	}
	return ok
}

// verifierHTTP: one GET of u, connected only to an address of hotes, or (public: u is a site's address the owner set)
// to any public one, whatever stands in front of the servers (a CDN such as Cloudflare, a proxy on another machine);
// checked at the connection itself, so a name that changes between two lookups gains nothing; no proxy, no redirect
// followed, body read 64 KB at most.
func verifierHTTP(ctx context.Context, u *url.URL, hotes []string, public bool) ResultatHTTP {
	r := ResultatHTTP{URL: u.String(), Authentification: "aucune", EnTetes: map[string]string{}, Manquants: []string{}}
	permises := adressesPermises(ctx, hotes)
	var info *InfoTLS
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, reseau, adresse string) (net.Conn, error) {
			if a, ok := resolutionsTest[adresse]; ok {
				adresse = a
			}
			hote, port, err := net.SplitHostPort(adresse)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, hote)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if permises[ip.IP.String()] || public && adressePublique(ip.IP) {
					return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, reseau, net.JoinHostPort(ip.IP.String(), port))
				}
			}
			return nil, errors.New(hote + " is not the address of one of your servers")
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, // verified below, so that an invalid certificate is reported, not only refused
			VerifyConnection: func(cs tls.ConnectionState) error {
				c := cs.PeerCertificates[0]
				i := &InfoTLS{Version: tls.VersionName(cs.Version), Sujet: c.Subject.String(), Emetteur: c.Issuer.String(),
					Expire: c.NotAfter.UTC().Format(time.RFC3339), JoursRestants: int(time.Until(c.NotAfter).Hours() / 24)}
				inter := x509.NewCertPool()
				for _, x := range cs.PeerCertificates[1:] {
					inter.AddCert(x)
				}
				if _, err := c.Verify(x509.VerifyOptions{DNSName: cs.ServerName, Roots: racinesVerif, Intermediates: inter}); err != nil {
					i.Erreur = err.Error()
				} else {
					i.Valide = true
				}
				info = i
				return nil
			}},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		r.Erreur = err.Error()
		return r
	}
	req.Header.Set("User-Agent", "Athanor-verifier")
	rep, err := client.Do(req)
	r.TLS = info
	if err != nil {
		r.Erreur = err.Error()
		return r
	}
	defer rep.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(rep.Body, 64<<10))
	r.Statut, r.Redirection = rep.StatusCode, rep.Header.Get("Location")
	if r.WWWAuthenticate = couper(rep.Header.Get("WWW-Authenticate"), 200); rep.StatusCode == http.StatusUnauthorized || r.WWWAuthenticate != "" {
		r.Authentification = "demandee"
	}
	for _, n := range entetesSecurite {
		if n == "Strict-Transport-Security" && u.Scheme != "https" {
			continue // only meaningful over TLS
		}
		if v := rep.Header.Get(n); v != "" {
			r.EnTetes[n] = couper(v, 200)
		} else {
			r.Manquants = append(r.Manquants, n)
		}
	}
	return r
}

// CritereVerifie: an acceptance criterion as the verifier found it.
type CritereVerifie struct {
	Texte string `json:"texte"`
	OK    bool   `json:"ok"`
}

// criteresVerifies: the verifier's "[ok] ..." and "[echec] ..." lines, in order.
func criteresVerifies(lignes []string) []CritereVerifie {
	var l []CritereVerifie
	for _, x := range lignes {
		x = strings.TrimSpace(x)
		for _, m := range []struct {
			pre string
			ok  bool
		}{{"[ok]", true}, {"[echec]", false}, {"[échec]", false}, {"[fail]", false}} {
			if len(x) > len(m.pre) && strings.EqualFold(x[:len(m.pre)], m.pre) {
				l = append(l, CritereVerifie{Texte: couper(strings.TrimSpace(x[len(m.pre):]), 300), OK: m.ok})
				break
			}
		}
	}
	return l
}
