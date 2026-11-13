package main

// The tools of external MCP clients (ARCHITECTURE.md §Ouverture, stage 11): Claude Desktop, Claude Code or
// another Athanor, within the scope the owner chose on screen at pairing (projects, tools). None decides an
// approval, launches a mission, reads a vault value or touches a server. Every call reads the client again
// (verifierClient: revoked or emergency stop = 401), then checks the tool and the project; each goes to the
// audit log as client:<name>.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// OutilClient: a tool of external clients. Faire answers the text the client reads; an error is a tool error.
type OutilClient struct {
	Description string
	Schema      map[string]any
	Faire       func(s *Serveur, c ClientMCP, args json.RawMessage) (string, error)
}

// outilsExternes: by name, the same names as outilsClients (what the window offers at pairing).
var outilsExternes = map[string]OutilClient{}

func init() {
	objet, texte := schemaObjet, schemaTexte
	projet := texte("The project's name, as the projets tool lists it")
	outilsExternes["etat"] = OutilClient{Description: "Athanor's state: its modules and the alerts of the bell that concern the projects you may see.",
		Schema: objet(map[string]any{}), Faire: outilClientEtat}
	outilsExternes["projets"] = OutilClient{Description: "The projects you may see, each with its pilot (running or not), phase, plan progress and what waits for the owner.",
		Schema: objet(map[string]any{}), Faire: outilClientProjets}
	outilsExternes["plan"] = OutilClient{Description: "A project's milestone plan: each milestone's id, title and whether it is done.",
		Schema: objet(map[string]any{"projet": projet}, "projet"), Faire: outilClientPlan}
	outilsExternes["demandes"] = OutilClient{Description: "A project's requests: the current request's text and the queue of the next ones.",
		Schema: objet(map[string]any{"projet": projet}, "projet"), Faire: outilClientDemandes}
	outilsExternes["ajouter_demande"] = OutilClient{Description: "Add a request to a project: it becomes the request if the project has none, otherwise it joins the queue the pilot takes next.",
		Schema: objet(map[string]any{"projet": projet, "texte": texte("What the project should do or change, in plain words"), "titre": texte("A short title (optional)")}, "projet", "texte"), Faire: outilClientAjouterDemande}
	outilsExternes["pilote"] = OutilClient{Description: "Start a project's pilot, stop it at the end of its current cycle (nothing lost), or stop it now.",
		Schema: objet(map[string]any{"projet": projet, "action": map[string]any{"type": "string", "enum": []string{"demarrer", "pause_fin_cycle", "arreter"}}}, "projet", "action"), Faire: outilClientPilote}
	outilsExternes["missions"] = OutilClient{Description: "The missions you may see; with an id, that mission's detail (lanes, journal). Launching, pausing or stopping one is the owner's, in Athanor.",
		Schema: objet(map[string]any{"id": texte("A mission's id, for its detail (optional)")}), Faire: outilClientMissions}
	outilsExternes["proposer_mission"] = OutilClient{Description: "Propose a mission in one sentence: Athanor prepares its card, and only the owner can launch it, in Athanor.",
		Schema: objet(map[string]any{"phrase": texte("What you want done, in one sentence")}, "phrase"), Faire: outilClientProposerMission}
	outilsExternes["approbations"] = OutilClient{Description: "The approvals waiting for the owner on the projects you may see. Only the owner decides, in Athanor's window or on a paired phone.",
		Schema: objet(map[string]any{}), Faire: outilClientApprobations}
	// another Athanor only (a hub paired with a module without a window, module_sans_fenetre.go)
	serveur := texte("The server's id, as the serveurs tool lists it")
	outilsExternes["serveurs"] = OutilClient{Description: "This Athanor's servers (Servers module): id, name, roles, state, last seen.",
		Schema: objet(map[string]any{}), Faire: outilClientServeurs}
	outilsExternes["taches_serveur"] = OutilClient{Description: "A server's tasks, newest first: id, text, state, dates, result.",
		Schema: objet(map[string]any{"serveur": serveur}, "serveur"), Faire: outilClientTachesServeur}
	outilsExternes["envoyer_bus"] = OutilClient{Description: "Ask the Servers module one of its declared actions (fiche, rafraichir_fiche) with donnees {serveur}. On a prod server the owner of this Athanor accepts first: the answer then says it waits.",
		Schema: objet(map[string]any{"action": map[string]any{"type": "string", "enum": moduleServeurs{}.Actions()}, "donnees": map[string]any{"type": "object"}}, "action"), Faire: outilClientEnvoyerBus}
}

var errPasAthanor = errors.New("this tool is for another Athanor only")

func outilClientServeurs(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	if c.Sorte != "athanor" {
		return "", errPasAthanor
	}
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	l, err := lireServeurs()
	if err != nil {
		return "", err
	}
	vus := []map[string]any{}
	for _, v := range l {
		vus = append(vus, map[string]any{"id": v.ID, "nom": v.Nom, "roles": append([]string{}, v.Roles...), "etat": v.Etat, "vu": v.Vu})
	}
	return enJSON(map[string]any{"serveurs": vus}), nil
}

func outilClientTachesServeur(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct {
		Serveur string `json:"serveur"`
	}
	if c.Sorte != "athanor" {
		return "", errPasAthanor
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	l, err := lireServeurs()
	if err != nil {
		return "", err
	}
	if !idServeurValide.MatchString(a.Serveur) || !slices.ContainsFunc(l, func(v ServeurInventaire) bool { return v.ID == a.Serveur }) {
		return "", errServeurInconnu
	}
	taches := []map[string]any{}
	for _, t := range tachesServeur(a.Serveur) {
		taches = append(taches, map[string]any{"id": t.ID, "texte": s.masquerSecrets(t.Texte), "etat": t.Etat, "cree": t.Cree, "fin": t.Fin, "resultat": s.masquerSecrets(t.Resultat)})
	}
	return enJSON(map[string]any{"taches": taches}), nil
}

// outilClientEnvoyerBus: through this Athanor's bus (I5 audit, I6 here: a prod server waits for its owner).
func outilClientEnvoyerBus(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct {
		Action  string         `json:"action"`
		Donnees map[string]any `json:"donnees"`
	}
	if c.Sorte != "athanor" {
		return "", errPasAthanor
	}
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	m := MessageHub{De: "client:" + c.Nom, Vers: "servers", Action: a.Action, Donnees: a.Donnees}
	if id, _ := a.Donnees["serveur"].(string); id != "" {
		if l, err := lireServeurs(); err == nil {
			if i := slices.IndexFunc(l, func(v ServeurInventaire) bool { return v.ID == id }); i >= 0 && slices.Contains(l[i].Roles, "prod") {
				m.Env = "prod"
			}
		}
	}
	rep := s.envoyerBus(m)
	return enJSON(map[string]any{"etat": rep.Etat, "raison": rep.Raison, "donnees": rep.Donnees}), nil
}

func tousProjets(c ClientMCP) bool { return slices.Contains(c.Projets, "*") }

func dansPerimetre(c ClientMCP, projet string) bool {
	return tousProjets(c) || slices.Contains(c.Projets, projet)
}

// projetClient: the project of a tool's argument; unknown and outside the scope get the same answer.
func (s *Serveur) projetClient(c ClientMCP, nom string) (Projet, error) {
	p, ok := s.projetNomme(strings.TrimSpace(nom))
	if !ok || !dansPerimetre(c, p.Nom) {
		return Projet{}, errors.New("unknown project, or outside what this client may see: " + couper(nom, 60))
	}
	return p, nil
}

// outilsListeClient: tools/list for a client, only the tools it was granted.
func outilsListeClient(c ClientMCP) []map[string]any {
	l := []map[string]any{}
	for _, nom := range c.Outils {
		if o, ok := outilsExternes[nom]; ok {
			l = append(l, map[string]any{"name": nom, "description": o.Description, "inputSchema": o.Schema})
		}
	}
	return l
}

// appelOutilClient: a tool outside the client's grant is answered as unknown; every call is audited.
func (s *Serveur) appelOutilClient(w http.ResponseWriter, c ClientMCP, q requeteMCP) {
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(q.Params, &p); err != nil || p.Name == "" {
		repondreMCP(w, http.StatusOK, q.ID, nil, &erreurMCP{-32602, "invalid params"})
		return
	}
	p.Name = couper(p.Name, 64)
	if len(p.Arguments) == 0 || string(p.Arguments) == "null" {
		p.Arguments = json.RawMessage("{}")
	}
	var a struct {
		Projet string `json:"projet"`
	}
	_ = json.Unmarshal(p.Arguments, &a)
	ligne := LigneAudit{Type: "demande", De: "client:" + c.Nom, Vers: "hub", Projet: couper(a.Projet, 60), Action: "mcp " + p.Name}
	o, connu := outilsExternes[p.Name]
	if !connu || !slices.Contains(c.Outils, p.Name) {
		ligne.Resultat = "refusé : outil hors du périmètre du client"
		s.ecrireAudit(ligne)
		repondreMCP(w, http.StatusOK, q.ID, nil, &erreurMCP{-32602, "unknown tool: " + p.Name})
		return
	}
	texte, err := o.Faire(s, c, p.Arguments)
	ligne.Resultat = "ok"
	if err != nil {
		texte, ligne.Resultat = err.Error(), "erreur : "+err.Error()
	}
	s.ecrireAudit(ligne)
	repondreMCP(w, http.StatusOK, q.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": texte}}, "isError": err != nil}, nil)
}

func outilClientEtat(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	modules := []map[string]any{}
	for _, m := range modulesHub() {
		v := map[string]any{"id": m.ID(), "nom": m.Nom()}
		if tousProjets(c) { // a module's summary counts every project and server
			v["etat"] = m.Resume(s).Etat
		}
		modules = append(modules, v)
	}
	_, el := s.vueHub()
	actives, _ := s.alertesHub(el)
	alertes := []map[string]any{}
	for _, a := range actives {
		projet := a.Element
		if a.Approbation != nil {
			projet = a.Approbation.Projet
		}
		if !tousProjets(c) && (a.Module != "dev" || !slices.Contains(c.Projets, projet)) {
			continue
		}
		raison := a.Raison
		for i, x := range a.Args {
			raison = strings.ReplaceAll(raison, fmt.Sprintf("{%d}", i), fmt.Sprint(x))
		}
		alertes = append(alertes, map[string]any{"module": a.Module, "element": a.Element, "titre": a.Titre, "raison": raison})
	}
	return enJSON(map[string]any{"modules": modules, "alertes": alertes}), nil
}

func outilClientProjets(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	projets := []map[string]any{}
	for _, pv := range s.vuesProjets("") {
		if dansPerimetre(c, pv.Nom) {
			projets = append(projets, map[string]any{"nom": pv.Nom, "pilote_actif": pv.Actif, "phase": pv.Phase,
				"plan": fmt.Sprintf("%d/%d milestones", pv.Faits, pv.Jalons), "jalon": firstNonEmpty(pv.JalonTitre, pv.Jalon),
				"besoin": pv.Besoin, "pour_le_proprietaire": pv.PourVous})
		}
	}
	return enJSON(map[string]any{"projets": projets}), nil
}

func outilClientPlan(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct{ Projet string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	p, err := s.projetClient(c, a.Projet)
	if err != nil {
		return "", err
	}
	jalons := []map[string]any{}
	for _, j := range p.LirePlan() {
		jalons = append(jalons, map[string]any{"id": j.ID, "fait": j.Fait, "titre": j.Titre})
	}
	return enJSON(map[string]any{"projet": p.Nom, "jalons": jalons}), nil
}

func outilClientDemandes(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct{ Projet string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	p, err := s.projetClient(c, a.Projet)
	if err != nil {
		return "", err
	}
	b, _ := os.ReadFile(p.Demande())
	attente := p.LireFileAttente()
	if attente == nil {
		attente = []DemandeAttente{}
	}
	return enJSON(map[string]any{"projet": p.Nom, "demande": couper(string(b), 20000), "attente": attente}), nil
}

func outilClientAjouterDemande(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct{ Projet, Texte, Titre string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	p, err := s.projetClient(c, a.Projet)
	if err != nil {
		return "", err
	}
	texte, err := texteRequis("texte", a.Texte, 20000)
	if err != nil {
		return "", err
	}
	texte = s.masquerSecrets(texte)
	titre := couper(firstNonEmpty(strings.TrimSpace(a.Titre), court(texte, 50)), 80)
	ou, f, err := s.ajouterDemande(p, titre, texte)
	if err != nil {
		return "", err
	}
	if ou == "demande" {
		return "Saved as the project's request: the pilot works on it once started.", nil
	}
	return "Queued as " + filepath.Base(f) + ": the pilot takes it after the current request.", nil
}

func outilClientPilote(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct{ Projet, Action string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	p, err := s.projetClient(c, a.Projet)
	if err != nil {
		return "", err
	}
	switch a.Action {
	case "demarrer":
		return s.demarrerPiloteAuto(p)
	case "pause_fin_cycle":
		marqueAuto(p, false)
		if _, err := s.demanderArretApresCycle(p); err != nil {
			return "", err
		}
		return "The pilot will stop at the end of this cycle: nothing is lost.", nil
	case "arreter":
		marqueAuto(p, false)
		if _, err := s.arreterPiloteProjet(p); err != nil {
			return "", err
		}
		return "The pilot is stopped.", nil
	}
	return "", errors.New("action must be demarrer, pause_fin_cycle or arreter")
}

// projetsMission: the Dev projects a mission names or works on.
func projetsMission(m Mission) []string {
	l := slices.Clone(m.Mentions.Projets)
	for _, v := range m.Voies {
		if v.Module == "dev" {
			l = append(l, v.Ref)
		}
	}
	return l
}

// missionVisible: a client limited to some projects sees a mission only if every project of it is among them.
func missionVisible(c ClientMCP, m Mission) bool {
	if tousProjets(c) {
		return true
	}
	l := projetsMission(m)
	return len(l) > 0 && !slices.ContainsFunc(l, func(p string) bool { return !slices.Contains(c.Projets, p) })
}

func outilClientMissions(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct{ ID string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	if id := strings.TrimSpace(a.ID); id != "" {
		m, err := lireMission(id)
		if err != nil || !missionVisible(c, m) {
			return "", errors.New("unknown mission, or outside what this client may see")
		}
		m = s.voiesEnDirect(m)
		journal := m.Journal
		if len(journal) > 20 {
			journal = journal[len(journal)-20:]
		}
		return enJSON(map[string]any{"id": m.ID, "etat": m.Etat, "titre": titreMission(m), "phrase": m.Phrase, "politique": m.Politique,
			"voies": m.Voies, "journal": journal, "cree": m.Cree, "suite": "Launching, pausing or stopping a mission is the owner's, on its page in Athanor."}), nil
	}
	l := []map[string]any{}
	for _, m := range lireMissions() {
		if missionVisible(c, m) {
			l = append(l, map[string]any{"id": m.ID, "etat": m.Etat, "titre": titreMission(m), "cree": m.Cree})
		}
	}
	return enJSON(map[string]any{"missions": l}), nil
}

// outilClientProposerMission: a card in preparation; launching it stays the owner's (I6).
func outilClientProposerMission(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	var a struct{ Phrase string }
	if err := lireArguments(args, &a); err != nil {
		return "", err
	}
	phrase := strings.TrimSpace(a.Phrase)
	if phrase == "" || len([]rune(phrase)) > maxPhraseMission {
		return "", fmt.Errorf("say what you want in one sentence, %d characters at most", maxPhraseMission)
	}
	mentions, err := s.resoudreMentions(nil)
	if err != nil {
		return "", err
	}
	m, err := s.creerMission(phrase, mentions, "client:"+c.Nom)
	if err != nil {
		return "", err
	}
	return enJSON(map[string]any{"mission": m.ID, "etat": m.Etat,
		"suite": "Athanor prepares the mission's card; the owner reviews it and alone can launch it, in Athanor."}), nil
}

func outilClientApprobations(s *Serveur, c ClientMCP, args json.RawMessage) (string, error) {
	if err := lireArguments(args, &struct{}{}); err != nil {
		return "", err
	}
	l := []map[string]any{}
	if s.approbations != nil {
		toutes, _ := s.approbations.Liste()
		for _, x := range toutes {
			if x.Etat == "attente" && dansPerimetre(c, x.Projet) {
				l = append(l, map[string]any{"id": x.ID, "projet": x.Projet, "env": x.Env, "action": x.Action, "nom": x.Details["nom"],
					"raison": x.Raison, "cree": x.Cree, "expire": x.Expire})
			}
		}
	}
	return enJSON(map[string]any{"approbations": l,
		"decider": "Only the owner decides, in Athanor's window (bell or Approvals page) or on a paired phone."}), nil
}
