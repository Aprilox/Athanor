package main

// The verified catalog's servers that run another way on macOS or on Linux: an entry keeps its Windows
// configuration and adds, under "systemes", what changes elsewhere:
//   "systemes": {"macos": {"config": {...}, "reglages": [...], "prerequis": "...", "sauvegarde": "..."},
//                "linux": {...}}
// Windows (and WSL, which reaches it) uses the entry as it is. A server installed before its variant
// existed keeps a Windows configuration in the library: the kit takes this system's variant instead
// (adapterAuSysteme), and the values left at the Windows default are dropped with it.
// "hote": a program of this computer run outside the cage (the catalog's only, never one a user pastes),
// for an application of the user's that talks to its own open window (Roblox Studio); {ATHANOR} is the
// kit's executable.

import (
	"encoding/json"
	"runtime"
	"strings"
)

// ReglageCatalogue: a setting of a catalog server (an address, a path…) and its default.
type ReglageCatalogue struct {
	Nom    string `json:"nom"`
	Defaut string `json:"defaut"`
}

// VarianteMCP: what changes for a catalog server on one system.
type VarianteMCP struct {
	Config     *ConfigMCP         `json:"config,omitempty"`
	Reglages   []ReglageCatalogue `json:"reglages,omitempty"`
	Prerequis  string             `json:"prerequis,omitempty"`
	Sauvegarde string             `json:"sauvegarde,omitempty"`
}

// systemeMCPForce: the tests' system (never set by the application).
var systemeMCPForce string

// systemeMCP: which variant of the catalog applies on this computer: "windows" (Windows, or WSL that
// reaches it), "macos" or "linux".
func systemeMCP() string {
	switch {
	case systemeMCPForce != "":
		return systemeMCPForce
	case runtime.GOOS == "windows":
		return "windows"
	case runtime.GOOS == "darwin":
		return "macos"
	case interopWindows():
		return "windows"
	}
	return "linux"
}

// appliquerSystemes: each catalog server as this system runs it.
func appliquerSystemes(c *Catalogue, sys string) {
	for i := range c.MCP {
		v, ok := c.MCP[i].Systemes[sys]
		if !ok {
			continue
		}
		if v.Config != nil {
			c.MCP[i].Config = *v.Config
		}
		if v.Reglages != nil {
			c.MCP[i].Reglages = v.Reglages
		}
		if v.Prerequis != "" {
			c.MCP[i].Prerequis = v.Prerequis
		}
	}
}

// entreeCatalogueSysteme: the kit's catalog entry ID (raw), with this system's variant merged in; nil if
// the catalog has no such server.
func entreeCatalogueSysteme(kit, id string) map[string]any {
	for _, m := range liste(catalogueKit(kit).brut("mcp")) {
		d := dict(m)
		if d == nil || d["id"] != id {
			continue
		}
		out := map[string]any{}
		for k, v := range d {
			out[k] = v
		}
		if v := dict(dict(d["systemes"])[systemeMCP()]); v != nil {
			for _, k := range []string{"config", "reglages", "prerequis", "sauvegarde"} {
				if x, ok := v[k]; ok {
					out[k] = x
				}
			}
		}
		return out
	}
	return nil
}

// adapterAuSysteme: a server installed with its Windows configuration, on a system that is not Windows:
// the catalog's variant for this system, its settings' defaults, and the values the user left at the
// Windows default dropped. The reason when the catalog has none (a program that only runs on Windows).
func adapterAuSysteme(kit string, def, secrets objetJSON) (objetJSON, objetJSON, string) {
	id, _ := def.brut("catalogue").(string)
	t, _ := def.objet("config").brut("type").(string)
	sys := systemeMCP()
	if id == "" || t != "windows" || sys == "windows" {
		return def, secrets, ""
	}
	e := entreeCatalogueSysteme(kit, id)
	conf := dict(e["config"])
	if conf == nil || conf["type"] == "windows" {
		p, _ := e["prerequis"].(string)
		return def, secrets, "a Windows program" + map[bool]string{true: " (" + p + ")", false: ""}[p != ""] + ", not on this system"
	}
	// the Windows defaults, from the entry as Windows runs it
	ancien := map[string]string{}
	for _, m := range liste(catalogueKit(kit).brut("mcp")) {
		if d := dict(m); d != nil && d["id"] == id {
			for _, r := range liste(d["reglages"]) {
				if rd := dict(r); rd != nil {
					n, _ := rd["nom"].(string)
					v, _ := rd["defaut"].(string)
					ancien[n] = v
				}
			}
		}
	}
	nouveau := copieObjet(def)
	nouveau.mettre("config", conf)
	var r objetJSON
	for _, x := range liste(e["reglages"]) {
		if rd := dict(x); rd != nil {
			n, _ := rd["nom"].(string)
			if n != "" {
				r.mettre(n, rd["defaut"])
			}
		}
	}
	nouveau.mettre("reglages", r)
	var s objetJSON
	for _, k := range secrets.cles {
		if v, ok := secrets.brut(k).(string); ok && ancien[k] != "" && v == ancien[k] {
			continue // left at the Windows default: this system's default instead
		}
		s.cles = append(s.cles, k)
		if s.valeur == nil {
			s.valeur = map[string]json.RawMessage{}
		}
		s.valeur[k] = secrets.valeur[k]
	}
	return nouveau, s, ""
}

// copieObjet: an objetJSON of its own (the library's stays as it is).
func copieObjet(o objetJSON) objetJSON {
	var c objetJSON
	b, _ := json.Marshal(o)
	_ = json.Unmarshal(b, &c)
	return c
}

// installeDuSysteme: the library's entry as this system runs it (the app's Skills & MCP page).
func installeDuSysteme(x MCPInstalle, cat Catalogue) MCPInstalle {
	if x.Catalogue == "" || x.Config.Type != "windows" || systemeMCP() == "windows" {
		return x
	}
	for _, m := range cat.MCP {
		if m.ID != x.Catalogue || m.Config.Type == "windows" {
			continue
		}
		x.Config = m.Config
		x.Reglages = map[string]string{}
		for _, r := range m.Reglages {
			x.Reglages[r.Nom] = r.Defaut
		}
	}
	return x
}

// commandeHote: {ATHANOR} in a host program's command or arguments: the kit's executable.
func commandeHote(s, athanor string) string { return strings.ReplaceAll(s, "{ATHANOR}", athanor) }
