package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMissionLancerNatifM245: the native Windows launch of a mission (5ced0d7). A Dev-only mission forces nothing; a
// Servers lane forces the project once, logged, without a secret; a project already forced is not forced again;
// outside native mode nothing changes; a Servers task that cannot start after the forcing leaves it logged.
func TestMissionLancerNatifM245(t *testing.T) {
	s := serveurCoffreTest(t)
	motDePasseCoffreTest(t, s)
	if err := ecrireConfig(s.config); err != nil {
		t.Fatal(err)
	}
	temoin := "TEMOIN-" + aleatoire(10)
	if err := modifierServeurs(func(l []ServeurInventaire) ([]ServeurInventaire, error) {
		return append(l, ServeurInventaire{ID: "a2", Nom: "dev-1", Hote: "203.0.113.6", Roles: []string{"dev"}}), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.coffre.Ajouter(EntreeCoffre{Nom: "Jeton Cloudflare", Famille: "secret", Type: "cloudflare"}, temoin+"-infra"); err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	_ = os.RemoveAll(dossierMissions())
	t.Cleanup(func() { _ = os.RemoveAll(dossierMissions()) })
	ancienPilote, ancienneSession := demarrerPiloteMission, sessionTacheServeurs
	t.Cleanup(func() { demarrerPiloteMission, sessionTacheServeurs = ancienPilote, ancienneSession })
	demarrerPiloteMission = func(_ *Serveur, p Projet) (string, error) { return "Pilot started", nil }
	sessionTacheServeurs = func(_ *Serveur, _ context.Context, dossier string) error {
		j := `{"type":"system","subtype":"init","session_id":"s"}` + "\n" + `{"type":"result","is_error":false,"result":"nothing to do"}` + "\n"
		return os.WriteFile(filepath.Join(dossier, "session.jsonl"), []byte(j), 0o600)
	}
	t.Cleanup(func() { modeNatif.Store(false) })
	attendre := func() {
		for range 400 {
			tachesEnCours.Lock()
			n := len(tachesEnCours.m)
			tachesEnCours.Unlock()
			if n == 0 {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("a Servers task did not end")
	}
	envServeur := func(nom string) []EnvCarte {
		return []EnvCarte{{Nom: "dev", URL: "https://dev." + nom + ".fr", Serveurs: []string{"a2"}}}
	}
	// lance: a fresh existing project, the card as the hub computes it, launched; returns the mission and the audit's actions for it
	lance := func(nom string, natif, force bool, c CarteMission) (Mission, map[string]int) {
		t.Helper()
		p := Projet{Nom: nom, Dossier: filepath.Join(base, nom)}
		ecrireFichier(t, p.Demande(), "# "+nom+"\n\nUn projet.\n", 0o644)
		s.config.Projets = append(s.config.Projets, p)
		if force {
			if err := s.acces.ForcerInfra(nom, true); err != nil {
				t.Fatal(err)
			}
		}
		modeNatif.Store(natif)
		c.Projet, c.JalonsEstimes = ProjetCarte{Nom: nom}, 1
		x := s.contexteCarte()
		if err := validerCarte(&c, x); err != nil {
			t.Fatal(err)
		}
		id := aleatoire(8)
		if err := enregistrerMission(Mission{ID: id, Phrase: "une mission", Etat: "proposee", Politique: "prod", Carte: mustJSON(calculerCarte(c, "prod", x)),
			Voies: []VoieMission{}, Questions: []QuestionMission{}, Historique: []EvenementMission{evenementMission("proposee", "")}, Cree: time.Now().UTC().Format(formatCree)}); err != nil {
			t.Fatal(err)
		}
		m, _ := s.lancerMission(id, "proprietaire", "", "")
		attendre()
		actions := map[string]int{}
		for _, l := range lignesAudit(t, dossierJournalAudit()) {
			if l.Projet == nom {
				actions[l.Action]++
				if strings.Contains(l.Resultat+l.Action+l.Vers, temoin) || strings.Contains(l.Resultat, "Ouvrir le port") {
					t.Errorf("%s: audit line holds a brief or a secret: %+v", nom, l)
				}
			}
		}
		return m, actions
	}
	forcesParLancement := func(m Mission) bool {
		return slices.ContainsFunc(m.Historique, func(h EvenementMission) bool {
			return strings.Contains(h.Detail, "the project is forced by this launch")
		})
	}

	t.Run("natif_dev_seul_sans_environnement", func(t *testing.T) {
		m, a := lance("p-dev", true, false, CarteMission{Titre: "Dev", Dev: BriefCarte{Brief: "x"}})
		if m.Etat != "lancee" || s.acces.Projet("p-dev").ForcerInfraNatif || a["forcer_infra_natif"] != 0 || a["voie Serveurs sans barrière"] != 0 || forcesParLancement(m) {
			t.Fatalf("a Dev-only mission forced something: %s %v %+v", m.Etat, a, m.Historique)
		}
	})
	t.Run("natif_dev_seul_avec_serveurs_dans_un_environnement", func(t *testing.T) {
		m, a := lance("p-env", true, false, CarteMission{Titre: "Dev + env", Dev: BriefCarte{Brief: "x"}, Envs: envServeur("p-env")})
		if m.Etat != "lancee" || s.acces.Projet("p-env").ForcerInfraNatif || a["forcer_infra_natif"] != 0 {
			t.Fatalf("no Servers lane, yet the project is forced: %s %v", m.Etat, a)
		}
	})
	t.Run("natif_voie_serveurs_force_une_fois", func(t *testing.T) {
		m, a := lance("p-srv", true, false, CarteMission{Titre: "Srv", Dev: BriefCarte{Brief: "x"}, Serveurs: BriefCarte{Brief: "Ouvrir le port"}, Envs: envServeur("p-srv"), Acces: []AccesCarte{{Entree: "Jeton Cloudflare", Pour: "p-srv"}}})
		if m.Etat != "lancee" || !s.acces.Projet("p-srv").ForcerInfraNatif || a["forcer_infra_natif"] != 1 || a["voie Serveurs sans barrière"] != 1 || !forcesParLancement(m) {
			t.Fatalf("Servers lane in native mode: %s %v %+v", m.Etat, a, m.Voies)
		}
	})
	t.Run("natif_projet_deja_force_pas_de_double_audit", func(t *testing.T) {
		m, a := lance("p-deja", true, true, CarteMission{Titre: "Srv", Dev: BriefCarte{Brief: "x"}, Serveurs: BriefCarte{Brief: "Ouvrir le port"}, Envs: envServeur("p-deja")})
		if m.Etat != "lancee" || a["forcer_infra_natif"] != 0 || a["voie Serveurs sans barrière"] != 1 || forcesParLancement(m) {
			t.Fatalf("already forced: %s %v", m.Etat, a)
		}
	})
	t.Run("hors_natif_inchange", func(t *testing.T) {
		m, a := lance("p-wsl", false, false, CarteMission{Titre: "Srv", Dev: BriefCarte{Brief: "x"}, Serveurs: BriefCarte{Brief: "Ouvrir le port"}, Envs: envServeur("p-wsl")})
		if m.Etat != "lancee" || s.acces.Projet("p-wsl").ForcerInfraNatif || a["forcer_infra_natif"] != 0 || a["voie Serveurs sans barrière"] != 0 || forcesParLancement(m) ||
			!slices.ContainsFunc(m.Voies, func(v VoieMission) bool { return v.Module == "servers" && v.Etat != "refusee" }) {
			t.Fatalf("outside native mode: %s %v %+v", m.Etat, a, m.Voies)
		}
	})
	t.Run("natif_tache_en_echec_apres_le_forçage", func(t *testing.T) {
		jetons := s.jetons
		s.jetons = nil // lancerTache refuses: the hub's sessions cannot be read
		t.Cleanup(func() { s.jetons = jetons })
		m, a := lance("p-echec", true, false, CarteMission{Titre: "Srv", Dev: BriefCarte{Brief: "x"}, Serveurs: BriefCarte{Brief: "Ouvrir le port"}, Envs: envServeur("p-echec")})
		if m.Etat != "echec" || !s.acces.Projet("p-echec").ForcerInfraNatif || a["forcer_infra_natif"] != 1 || a["voie Serveurs sans barrière"] != 0 || !forcesParLancement(m) {
			t.Fatalf("task failed after forcing: %s %v %+v", m.Etat, a, m.Historique)
		}
	})
}
