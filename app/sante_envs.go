package main

// M137 (VISION.md §7 P6): the deployed environments' health, checked every 15 minutes (adjustable, can be
// switched off): the external check, the certificate's days left, the disk and memory of their servers' sheets.
// An incident (the last deployment failed, or the health red twice in a row) rings the bell and the phone in
// plain words, with three actions: Retry, Go back to the previous version, I take over.

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

type ProblemeSante struct {
	Code  string `json:"code"` // verif | certificat | disque | memoire
	Args  []any  `json:"args,omitempty"`
	Rouge bool   `json:"rouge,omitempty"` // counts for an incident
}

type SanteEnv struct {
	Etat         string            `json:"etat"` // ok | alerte | incident (the checks only: vueSante adds a failed deployment)
	Problemes    []ProblemeSante   `json:"problemes,omitempty"`
	Verification *VerifDeploiement `json:"verification,omitempty"`
	Rouges       int               `json:"rouges,omitempty"`       // red checks in a row
	RougeDepuis  string            `json:"rouge_depuis,omitempty"` // the first of them (RFC 3339): the incident's key
	T            string            `json:"t"`
}

// Incident: what the bell, the phone and the environment card tell, and what Retry deploys again.
type Incident struct {
	Cle        string         `json:"cle"`
	Source     string         `json:"source"`            // deploiement | sante
	Echec      string         `json:"echec"`             // what failed
	Message    *MessageMoteur `json:"message,omitempty"` // B79: Echec to translate (a deployment's)
	Tente      string         `json:"tente"`             // what was tried
	Commit     string         `json:"commit,omitempty"`
	Retour     bool           `json:"retour,omitempty"`      // the failure was a way back: Retry goes back again
	Cible      string         `json:"cible,omitempty"`       // C71: the version that way back aimed at (Retry aims at it again)
	SansRetour bool           `json:"sans_retour,omitempty"` // C71: Go back not offered (failed before its switch, or already back)
	PrisEnMain string         `json:"pris_en_main,omitempty"`
}

type fichierSante struct {
	Minutes  int                 `json:"minutes,omitempty"` // 0: every 15 minutes
	Arret    bool                `json:"arret,omitempty"`
	Dernier  string              `json:"dernier,omitempty"`  // the last round (RFC 3339)
	Envs     map[string]SanteEnv `json:"envs,omitempty"`     // "projet/env"
	Mains    map[string]string   `json:"mains,omitempty"`    // incident key → when the owner took over
	Notifies map[string]string   `json:"notifies,omitempty"` // incident key → when the phone was told
}

const (
	minutesSanteDefaut = 15
	joursCertAlerte    = 14
	joursCertIncident  = 3
	pourcentRessources = 90
)

var (
	intervallesSante = []int{5, 15, 30, 60}
	santeMu          sync.Mutex
	errIncidentFini  = errors.New("This incident is over.")
)

func fichierSanteEnvs() string { return filepath.Join(dossierModule("hub"), "sante_envs.json") }

func lireSante() fichierSante {
	var f fichierSante
	lireJSONFichier(fichierSanteEnvs(), &f)
	f.Minutes = cmp.Or(f.Minutes, minutesSanteDefaut)
	if f.Envs == nil {
		f.Envs = map[string]SanteEnv{}
	}
	if f.Mains == nil {
		f.Mains = map[string]string{}
	}
	if f.Notifies == nil {
		f.Notifies = map[string]string{}
	}
	return f
}

func modifierSante(fn func(*fichierSante)) {
	santeMu.Lock()
	defer santeMu.Unlock()
	f := lireSante()
	fn(&f)
	_ = ecrireJSONFichier(fichierSanteEnvs(), f, 0o600)
}

func pourcentUtilise(total, libre int) int {
	if total <= 0 {
		return 0
	}
	return (total - libre) * 100 / total
}

// jugerSante: the problems of one check (jours nil: no certificate known); a site that fails its check
// and a certificate under 3 days are red.
func jugerSante(v *VerifDeploiement, jours *int, ressources map[string]RessourcesFiche) []ProblemeSante {
	l := []ProblemeSante{}
	if v != nil && !v.OK {
		l = append(l, ProblemeSante{Code: "verif", Rouge: true}) // its details: the card's Verification
	}
	if jours != nil && *jours < joursCertAlerte {
		l = append(l, ProblemeSante{Code: "certificat", Args: []any{*jours}, Rouge: *jours < joursCertIncident})
	}
	noms := make([]string, 0, len(ressources))
	for n := range ressources {
		noms = append(noms, n)
	}
	slices.Sort(noms)
	for _, n := range noms {
		r := ressources[n]
		if p := pourcentUtilise(r.DisqueMo, r.DisqueLibreMo); p > pourcentRessources {
			l = append(l, ProblemeSante{Code: "disque", Args: []any{n, p}})
		}
		if p := pourcentUtilise(r.MemoireMo, r.MemoireLibreMo); p > pourcentRessources {
			l = append(l, ProblemeSante{Code: "memoire", Args: []any{n, p}})
		}
	}
	return l
}

// noterControle: r recorded for cle, after the previous check (the red ones in a row, its state).
func noterControle(f *fichierSante, cle string, r SanteEnv) {
	if slices.ContainsFunc(r.Problemes, func(p ProblemeSante) bool { return p.Rouge }) {
		avant := f.Envs[cle]
		r.Rouges, r.RougeDepuis = avant.Rouges+1, cmp.Or(avant.RougeDepuis, r.T)
	}
	r.Etat = "ok"
	if r.Rouges >= 2 {
		r.Etat = "incident"
	} else if len(r.Problemes) > 0 {
		r.Etat = "alerte"
	}
	f.Envs[cle] = r
}

// texteProblemes: the problems in plain words, in Athanor's language (the bell and the phone).
func (s *Serveur) texteProblemes(l []ProblemeSante) string {
	out := make([]string, 0, len(l))
	for _, p := range l {
		out = append(out, fmt.Sprintf(s.textePush("s_"+p.Code), p.Args...))
	}
	return strings.Join(out, "; ")
}

// ficheFraiche: the server's sheet, made again first when it is older than age and its key can be opened.
func (s *Serveur) ficheFraiche(ctx context.Context, srv ServeurInventaire, age time.Duration) (RessourcesFiche, bool) {
	chemin := filepath.Join(dossierModule("servers"), srv.ID, "fiche.json")
	if st, err := os.Stat(chemin); (err != nil || time.Since(st.ModTime()) >= age) && s.coffre != nil && idServeurValide.MatchString(srv.ID) {
		if _, err := s.coffre.Valeur(srv.Cle); err == nil {
			if f, err := s.rafraichirFiche(ctx, srv); err == nil {
				return f.Ressources, true
			}
		}
	}
	var f FicheServeur
	lireJSONFichier(chemin, &f)
	return f.Ressources, !f.Date.IsZero()
}

// controlerEnv: one check of a deployed environment.
func (s *Serveur) controlerEnv(ctx context.Context, e Environnement, age time.Duration) SanteEnv {
	v := s.verifierEnvironnement(ctx, e)
	var jours *int
	if x := v.HTTP.TLS; x != nil && x.Valide {
		jours = &x.JoursRestants
	}
	if t, err := time.Parse(time.RFC3339, e.CertificatExpire); err == nil {
		if j := int(time.Until(t).Hours() / 24); jours == nil || j < *jours {
			jours = &j
		}
	}
	ressources := map[string]RessourcesFiche{}
	l, _ := lireServeurs()
	for _, srv := range l {
		if slices.Contains(e.Serveurs, srv.ID) {
			if r, ok := s.ficheFraiche(ctx, srv, age); ok {
				ressources[srv.Nom] = r
			}
		}
	}
	return SanteEnv{Problemes: jugerSante(v, jours, ressources), Verification: v, T: time.Now().UTC().Format(time.RFC3339)}
}

// envsDeployes: "projet/env" → the environment, for the projects not archived whose environment has a method and a URL.
func (s *Serveur) envsDeployes() map[string]Environnement {
	m := map[string]Environnement{}
	if s.acces == nil {
		return m
	}
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	for _, p := range projets {
		if p.Archive {
			continue
		}
		for env, e := range s.acces.Environnements(p.Nom) {
			if e.Deploiement != nil && e.URL != "" {
				m[p.Nom+"/"+env] = e
			}
		}
	}
	return m
}

// controlerSante: one round over every deployed environment; those no longer deployed are forgotten.
func (s *Serveur) controlerSante(ctx context.Context) {
	envs := s.envsDeployes()
	age := time.Duration(lireSante().Minutes) * time.Minute
	resultats := map[string]SanteEnv{}
	for cle, e := range envs {
		resultats[cle] = s.controlerEnv(ctx, e, age)
	}
	modifierSante(func(f *fichierSante) {
		avant := f.Envs
		f.Envs = map[string]SanteEnv{}
		for cle, r := range resultats {
			f.Envs[cle] = avant[cle]
			noterControle(f, cle, r)
		}
		f.Dernier = time.Now().UTC().Format(time.RFC3339)
	})
}

// recontroler: the environment checked again at once (after an incident's action).
func (s *Serveur) recontroler(ctx context.Context, projet, env string) {
	cle := projet + "/" + env
	e, ok := s.envsDeployes()[cle]
	if !ok || lireSante().Arret {
		return
	}
	r := s.controlerEnv(ctx, e, time.Duration(lireSante().Minutes)*time.Minute)
	modifierSante(func(f *fichierSante) {
		delete(f.Envs, cle) // a fresh start: the red ones before the action do not count
		noterControle(f, cle, r)
	})
}

// incidentEnv: the environment's incident (nil: none): its last deployment failed, or its health was red twice in a row.
func (s *Serveur) incidentEnv(f fichierSante, projet, env string) *Incident {
	h := historiqueDeploiements(projet, env)
	var i *Incident
	n := len(h)
	if n > 1 && h[n-1].Retour && h[n-1].Auteur == "auto" && h[n-1].Etat == "ok" && h[n-2].Etat == "revenu" {
		n-- // C76: the failure the automatic way back followed is still told
	}
	if n > 0 && (h[n-1].Etat == "echec" || n < len(h)) {
		d := h[n-1]
		i = &Incident{Cle: "deploi-" + d.ID, Source: "deploiement", Echec: d.Erreur, Message: d.Message, Commit: d.Commit, Retour: d.Retour,
			SansRetour: n < len(h) || avantBascule(d)}
		if d.Retour {
			i.Cible = d.Version
		}
		switch {
		case d.Retour:
			i.Tente = s.textePush("t_retour")
		case d.Verification != nil:
			i.Tente = fmt.Sprintf(s.textePush("t_verif"), s.methodeLisible(d.Methode), couper(d.Commit, 7))
		default:
			i.Tente = fmt.Sprintf(s.textePush("t_deploi"), s.methodeLisible(d.Methode), couper(d.Commit, 7))
		}
	} else if sv := f.Envs[projet+"/"+env]; !f.Arret && sv.Etat == "incident" {
		i = &Incident{Cle: "sante-" + projet + "/" + env + "-" + sv.RougeDepuis, Source: "sante", Echec: s.texteProblemes(sv.Problemes),
			Tente: fmt.Sprintf(s.textePush("t_sante"), sv.Rouges, f.Minutes)}
		for k := len(h) - 1; k >= 0 && i.Commit == ""; k-- {
			if h[k].Etat == "ok" {
				i.Commit = h[k].Commit // the version served
			}
		}
	}
	if i != nil {
		i.Echec = strings.TrimSuffix(i.Echec, ".")
		i.PrisEnMain = f.Mains[i.Cle]
	}
	return i
}

type incidentLieu struct {
	Projet, Env string
	*Incident
}

// incidentsCourants: the incidents of the projects not archived.
func (s *Serveur) incidentsCourants(f fichierSante) []incidentLieu {
	s.mu.Lock()
	projets := append([]Projet{}, s.config.Projets...)
	s.mu.Unlock()
	var out []incidentLieu
	for _, p := range projets {
		if p.Archive {
			continue
		}
		for _, env := range envsProjet {
			if i := s.incidentEnv(f, p.Nom, env); i != nil {
				out = append(out, incidentLieu{p.Nom, env, i})
			}
		}
	}
	return out
}

// alertesIncidents: the bell rings for each incident the owner has not taken over, with its three actions;
// in prod, Retry and Go back ask for the owner's accord (the page asks for the password).
func (s *Serveur) alertesIncidents() []AlerteHub {
	out := []AlerteHub{}
	for _, x := range s.incidentsCourants(lireSante()) {
		if x.PrisEnMain != "" {
			continue
		}
		action := func(id, libelle string) ActionAlerte {
			return ActionAlerte{ID: id, Libelle: libelle, Route: "projets/" + url.PathEscape(x.Projet) + "/incident",
				Corps: map[string]any{"env": x.Env, "action": id, "cle": x.Cle}, Distance: true, Accord: x.Env == "prod" && id != "main"}
		}
		actions := []ActionAlerte{action("reessayer", "Retry"), action("revenir", "Go back to the previous version"), action("main", "I take over")}
		if x.SansRetour {
			actions = slices.Delete(actions, 1, 2)
		}
		h := sha256.Sum256([]byte("incident\x00" + x.Projet + "\x00" + x.Cle))
		out = append(out, AlerteHub{Empreinte: hex.EncodeToString(h[:12]), Module: "servers", Element: x.Projet + "/" + x.Env, Titre: x.Projet,
			Raison: "{0} is in trouble. What failed: {1}. What was tried: {2}.", Args: []any{x.Env, echecLisible(s, x), x.Tente},
			Actions: actions})
	}
	return out
}

// notifierIncidents: each new incident told once on the paired devices; the keys of incidents over are forgotten.
func (s *Serveur) notifierIncidents() {
	f := lireSante()
	courants, mains := map[string]string{}, map[string]string{}
	maintenant := time.Now().UTC().Format(time.RFC3339)
	for _, x := range s.incidentsCourants(f) {
		if x.PrisEnMain != "" {
			mains[x.Cle] = x.PrisEnMain
		}
		if quand, ok := f.Notifies[x.Cle]; ok {
			courants[x.Cle] = quand
			continue
		}
		courants[x.Cle] = maintenant
		s.notifierAppareils(MessagePush{Titre: fmt.Sprintf(s.textePush("incident"), x.Projet, x.Env),
			Texte: fmt.Sprintf(s.textePush("incident_t"), s.masquerSecrets(couper(x.Echec, 200)), x.Tente), URL: "/#serveurs", Tag: "incident-" + x.Projet + "-" + x.Env})
	}
	modifierSante(func(f *fichierSante) { f.Notifies, f.Mains = courants, mains })
}

// santeEnvsFond (main.go): every minute, a round when it is due, then the new incidents told.
func (s *Serveur) santeEnvsFond() {
	for range time.Tick(time.Minute) {
		f := lireSante()
		dernier, _ := time.Parse(time.RFC3339, f.Dernier)
		if !f.Arret && time.Since(dernier) >= time.Duration(f.Minutes)*time.Minute {
			s.controlerSante(context.Background())
		}
		s.notifierIncidents()
	}
}

// vueSante: env → its health for the environment card (absent: never checked and no incident).
func (s *Serveur) vueSante(projet string) map[string]any {
	f, m := lireSante(), map[string]any{}
	for _, env := range envsProjet {
		sv, controle := f.Envs[projet+"/"+env]
		i := s.incidentEnv(f, projet, env)
		if !controle && i == nil {
			continue
		}
		if f.Arret {
			sv = SanteEnv{}
		}
		etat := cmp.Or(sv.Etat, "ok")
		if i != nil {
			etat = "incident"
			i.Echec = s.masquerSecrets(couper(i.Echec, 300))
		}
		m[env] = map[string]any{"etat": etat, "problemes": sv.Problemes, "verification": sv.Verification, "t": sv.T, "incident": i}
	}
	return m
}

// santeServeurs: the Servers page: the setting and "projet/env" → its state.
func (s *Serveur) santeServeurs() map[string]any {
	f := lireSante()
	etats := map[string]string{}
	for cle, sv := range f.Envs {
		if !f.Arret {
			etats[cle] = sv.Etat
		}
	}
	for _, x := range s.incidentsCourants(f) {
		etats[x.Projet+"/"+x.Env] = "incident"
	}
	return map[string]any{"minutes": f.Minutes, "arret": f.Arret, "dernier": f.Dernier, "intervalles": intervallesSante, "envs": etats}
}

// apiSanteReglage: POST /api/serveurs/sante {minutes, arret}.
func (s *Serveur) apiSanteReglage(w http.ResponseWriter, r *http.Request) {
	var c struct {
		Minutes int  `json:"minutes"`
		Arret   bool `json:"arret"`
	}
	if lireCorps(r, &c) != nil || !c.Arret && !slices.Contains(intervallesSante, c.Minutes) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	modifierSante(func(f *fichierSante) {
		f.Arret = c.Arret
		if !c.Arret {
			f.Minutes = c.Minutes
		}
	})
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Action: "santé des sites", Resultat: fmt.Sprintf("arret=%v minutes=%d", c.Arret, c.Minutes)})
	s.ecrireJSON(w, s.santeServeurs())
}

// agirIncident: one of the three actions on the environment's current incident (cle: the one the owner saw).
func (s *Serveur) agirIncident(ctx context.Context, projet, env, action, cle, auteur string) (map[string]any, error) {
	i := s.incidentEnv(lireSante(), projet, env)
	if i == nil || cle != "" && cle != i.Cle {
		return nil, errIncidentFini
	}
	audit := func(resultat string) {
		s.ecrireAudit(LigneAudit{Type: "evenement", De: auteur, Vers: "hub", Projet: projet, Env: env, Action: "incident." + action, Resultat: resultat})
	}
	if action == "main" {
		e := s.acces.Environnements(projet)[env]
		if e.DeployerAuJalon { // the relay of this environment stops: the owner works by hand
			e.DeployerAuJalon = false
			if err := s.acces.DefinirEnvironnement(projet, env, e); err != nil {
				return nil, err
			}
		}
		modifierSante(func(f *fichierSante) { f.Mains[i.Cle] = time.Now().UTC().Format(time.RFC3339) })
		audit(i.Cle)
		page := "#acces"
		if len(e.Serveurs) > 0 {
			page = "#serveur/" + url.PathEscape(e.Serveurs[0])
		}
		return map[string]any{"page": page, "projet": projet}, nil
	}
	var d Deploiement
	var err error
	switch {
	case action == "revenir" && i.SansRetour:
		err = errors.New("nothing to go back from: the version served did not change")
	case action == "reessayer" && i.Retour: // C71: the same version again, never one more step back
		d, err = s.revenir(ctx, projet, env, auteur, i.Cible)
	case action == "revenir":
		d, err = s.revenir(ctx, projet, env, auteur, "")
	case i.Commit == "":
		err = errors.New("nothing to deploy again: no version of this environment is known")
	default:
		d, err = s.deployer(ctx, projet, env, i.Commit, OptionsDeploiement{Auteur: auteur, Accorde: auteur == "proprietaire"})
	}
	audit(cmp.Or(d.Etat, "refus") + " " + couper(s.masquerSecrets(fmt.Sprint(err)), 160))
	if d.ID == "" {
		return nil, cmp.Or(err, errIncidentFini)
	}
	s.recontroler(ctx, projet, env)
	return map[string]any{"deploiement": d, "deploiements": deploiementsAffiches(projet)[env], "sante": s.vueSante(projet)[env]}, nil
}

// apiIncident: POST /api/projets/{nom}/incident {env, action: reessayer|revenir|main, cle, mot_de_passe}:
// in prod, Retry and Go back only with the owner's accord (as apiRevenir).
func (s *Serveur) apiIncident(w http.ResponseWriter, r *http.Request) {
	projet := r.PathValue("nom")
	var c struct {
		Env        string `json:"env"`
		Action     string `json:"action"`
		Cle        string `json:"cle"`
		MotDePasse string `json:"mot_de_passe"`
	}
	if lireCorps(r, &c) != nil || !slices.Contains(envsProjet, c.Env) || !slices.Contains([]string{"reessayer", "revenir", "main"}, c.Action) {
		s.erreur(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if !s.accesPret(w, projet) {
		return
	}
	if c.Env == "prod" && c.Action != "main" {
		if code, err := s.preuvePresence(r, c.MotDePasse); err != nil {
			s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "hub", Projet: projet, Env: c.Env, Action: "incident." + c.Action + " refusé", Resultat: err.Error()})
			s.erreur(w, code, err.Error())
			return
		}
	}
	ctx, fin := context.WithTimeout(context.Background(), 10*time.Minute) // not cut halfway when the page goes away
	defer fin()
	v, err := s.agirIncident(ctx, projet, c.Env, c.Action, c.Cle, "proprietaire")
	if err != nil {
		s.erreur(w, http.StatusConflict, err.Error())
		return
	}
	s.ecrireJSON(w, v)
}
