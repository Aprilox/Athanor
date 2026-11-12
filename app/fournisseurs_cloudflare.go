package main

// Cloudflare for the Servers module: one client (appelCloudflare, every call in the audit log) and the
// provider's Test: the token is read from the infra vault into memory only, verified, its zones listed,
// then each zone probed with reads (and one write that Cloudflare rejects as invalid) to deduce its rights.

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// droitsCloudflare: the rights Test can deduce, in display order.
var droitsCloudflare = []string{"dns:lire", "dns:écrire", "ssl", "access", "certificats"}

// maxZonesSondees: shortcut: zones beyond it are listed without rights, raise it if owners hold more domains.
const maxZonesSondees = 20

// appelCloudflare: one call to the Cloudflare API (chemin under /client/v4), the JSON answer decoded into v;
// its method, path and status go to the audit log (never a header). The status is 0 when unreachable.
func (s *Serveur) appelCloudflare(jeton, methode, chemin string, corps, v any) int {
	var lecteur io.Reader
	if corps != nil {
		b, _ := json.Marshal(corps)
		lecteur = bytes.NewReader(b)
	}
	code := 0
	if req, err := http.NewRequest(methode, urlCloudflare+"/client/v4"+chemin, lecteur); err == nil {
		req.Header.Set("Authorization", "Bearer "+jeton)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Athanor")
		if resp, err := (&http.Client{Timeout: delaiTesteur}).Do(req); err == nil {
			code = resp.StatusCode
			if v != nil {
				_ = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
			}
			resp.Body.Close()
		}
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: "hub", Vers: "cloudflare", Action: "cloudflare " + methode + " " + chemin,
		Resultat: strconv.Itoa(code)})
	return code
}

// testerFournisseur: what Test learns about the token of f (zones, rights, state); the vault must be open.
func (s *Serveur) testerFournisseur(f Fournisseur, jeton string) Fournisseur {
	f.Teste, f.Zones, f.Droits, f.Compte = time.Now().UTC(), []ZoneFournisseur{}, []string{}, ""
	etat := func(e, cle string, args ...string) Fournisseur {
		m := messageTest(cle, args...)
		f.Etat, f.Bilan = e, &m
		return f
	}
	var verif struct {
		Success bool `json:"success"`
		Result  struct {
			Status string `json:"status"`
		} `json:"result"`
	}
	code := s.appelCloudflare(jeton, "GET", "/user/tokens/verify", nil, &verif)
	switch {
	case code == 0:
		return etat("injoignable", "Could not reach {0}.", "Cloudflare")
	case code == 400 || code == 401 || code == 403 || (code == 200 && !verif.Success):
		return etat("refuse", "Cloudflare refused this token.")
	case code != 200:
		return etat("injoignable", "{0} answered with an unexpected error ({1}).", "Cloudflare", strconv.Itoa(code))
	case verif.Result.Status == "expired":
		return etat("expire", "This Cloudflare token has expired: create a new one and replace it in the Vault.")
	case verif.Result.Status != "active":
		return etat("desactive", "This Cloudflare token is disabled: enable it again on Cloudflare or replace it.")
	}
	var zones struct {
		Result []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Account struct {
				ID string `json:"id"`
			} `json:"account"`
		} `json:"result"`
	}
	if code := s.appelCloudflare(jeton, "GET", "/zones?per_page=50", nil, &zones); code != 200 {
		return etat("injoignable", "{0} answered with an unexpected error ({1}).", "Cloudflare", strconv.Itoa(code))
	}
	if len(zones.Result) == 0 {
		return etat("sans_zone", "This Cloudflare token sees no zone: give it access to your domain's zone.")
	}
	f.Compte = texteSur(zones.Result[0].Account.ID, 64)
	// Access applications belong to the account: one probe for every zone
	access := f.Compte != "" && s.appelCloudflare(jeton, "GET", "/accounts/"+url.PathEscape(f.Compte)+"/access/apps", nil, nil) == 200
	for i, z := range zones.Result {
		zone := ZoneFournisseur{ID: texteSur(z.ID, 64), Nom: texteSur(z.Name, 100), Droits: []string{}}
		if i < maxZonesSondees {
			zone.Droits = s.sonderZone(jeton, z.ID, access)
		}
		f.Zones = append(f.Zones, zone)
		for _, d := range zone.Droits {
			if !slices.Contains(f.Droits, d) {
				f.Droits = append(f.Droits, d)
			}
		}
	}
	slices.SortFunc(f.Droits, func(a, b string) int { return slices.Index(droitsCloudflare, a) - slices.Index(droitsCloudflare, b) })
	noms := make([]string, len(f.Zones))
	for i, z := range f.Zones {
		noms[i] = z.Nom
	}
	return etat("ok", "Cloudflare token active; zones: {0}", strings.Join(noms, ", "))
}

// sonderZone: the rights of the token on one zone. The write probe is a DNS record without name or content:
// Cloudflare checks the permission first (403 without it) then rejects the record (400), so nothing is created.
func (s *Serveur) sonderZone(jeton, zone string, access bool) []string {
	z := "/zones/" + url.PathEscape(zone)
	d := []string{}
	if s.appelCloudflare(jeton, "GET", z+"/dns_records?per_page=1", nil, nil) == 200 {
		d = append(d, "dns:lire")
	}
	if code := s.appelCloudflare(jeton, "POST", z+"/dns_records", map[string]string{"type": "TXT"}, nil); code == 400 || code == 200 {
		d = append(d, "dns:écrire")
	}
	if s.appelCloudflare(jeton, "GET", z+"/settings/ssl", nil, nil) == 200 {
		d = append(d, "ssl")
	}
	if access {
		d = append(d, "access")
	}
	if s.appelCloudflare(jeton, "GET", "/certificates?zone_id="+url.QueryEscape(zone), nil, nil) == 200 {
		d = append(d, "certificats")
	}
	return d
}

// apiFournisseurTester: POST /api/fournisseurs/{id}/tester: the test, saved in fournisseurs.json.
func (s *Serveur) apiFournisseurTester(w http.ResponseWriter, r *http.Request) {
	if !s.coffrePret(w) {
		return
	}
	id := r.PathValue("id")
	l, err := lireFournisseurs()
	if err != nil {
		s.erreurFournisseurs(w, err)
		return
	}
	i := slices.IndexFunc(l, func(f Fournisseur) bool { return f.ID == id })
	if i < 0 {
		s.erreurFournisseurs(w, errServeurInconnu)
		return
	}
	var jeton string
	if _, err = s.cleFournisseur(l[i].Cle); err == nil {
		jeton, err = s.coffre.Valeur(l[i].Cle)
	}
	if err != nil {
		s.erreurFournisseurs(w, err)
		return
	}
	f := s.testerFournisseur(l[i], jeton)
	err = modifierFournisseurs(func(l []Fournisseur) ([]Fournisseur, error) {
		j := slices.IndexFunc(l, func(x Fournisseur) bool { return x.ID == id })
		if j < 0 {
			return nil, errServeurInconnu // removed during the test
		}
		f.Nom, f.Cle = l[j].Nom, l[j].Cle
		l[j] = f
		return l, nil
	})
	if err != nil {
		s.erreurFournisseurs(w, err)
		return
	}
	s.ecrireAudit(LigneAudit{Type: "evenement", De: deQui(r), Vers: "cloudflare", Action: "fournisseur testé",
		Resultat: f.Nom + " [" + f.ID + "]: " + f.Etat + ", " + strconv.Itoa(len(f.Zones)) + " zone(s)"})
	s.ecrireJSON(w, f)
}
