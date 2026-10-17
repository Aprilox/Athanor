package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiecesJointes(t *testing.T) {
	d := t.TempDir()
	p := Projet{Nom: "a", Dossier: d}
	s := nouveauServeur(Config{Kit: t.TempDir(), Projets: []Projet{p}, ProjetActif: "a", Langue: "fr"})
	s.csrf = "jeton"
	mux := http.NewServeMux()
	s.routes(mux)
	envoi := func(cible, nom string, corps []byte) (int, map[string]any) {
		r := httptest.NewRequest("POST", "/api/pieces?p=a&cible="+url.QueryEscape(cible), bytes.NewReader(corps))
		r.Header.Set("X-Athanor", "jeton")
		r.Header.Set("X-Nom-Fichier", url.QueryEscape(nom))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return w.Code, v
	}
	ecrire(t, p.Demande(), "# Site\n\nUn site vitrine.\n\n## Remarques en cours de route\n\n(...)\n")
	if c, v := envoi("demande", `C:\Users\x\Cahier des charges.pdf`, []byte("%PDF-1.4")); c != 200 || v["chemin"] != ".athanor/attachments/request/Cahier des charges.pdf" {
		t.Fatalf("upload: %d %v", c, v)
	}
	envoi("demande", "Cahier des charges.pdf", []byte("autre"))     // same name: kept apart
	envoi("demande", "../../../etc/passwd`;rm -rf ~`", []byte("x")) // hostile name
	envoi("demande", "CON", []byte("x"))                            // reserved on Windows
	b, _ := os.ReadFile(p.Demande())
	txt := string(b)
	for _, attendu := range []string{"## Fichiers joints", "- `.athanor/attachments/request/Cahier des charges.pdf` (PDF, 8 B)",
		"- `.athanor/attachments/request/Cahier des charges (2).pdf`", "- `.athanor/attachments/request/passwd_rm -rf _`", "- `.athanor/attachments/request/_CON`"} {
		if !strings.Contains(txt, attendu) {
			t.Errorf("missing %q in:\n%s", attendu, txt)
		}
	}
	if strings.Count(txt, "## Fichiers joints") != 1 {
		t.Errorf("one section only:\n%s", txt)
	}
	if _, err := os.Stat(filepath.Join(p.Atelier(), "attachments", "request", "_CON")); err != nil {
		t.Error("file not written")
	}
	// a queued request
	f, _ := p.AjouterDemande("Blog", "Ajouter un blog")
	if c, _ := envoi(f, "maquette.png", []byte("png")); c != 200 {
		t.Fatalf("queue: %d", c)
	}
	if b, _ := os.ReadFile(filepath.Join(p.FileAttente(), f)); !strings.Contains(string(b), "attachments/"+strings.TrimSuffix(f, ".md")+"/maquette.png") {
		t.Fatalf("queue file: %s", b)
	}
	if c, _ := envoi("99-absente.md", "x.txt", []byte("x")); c != 404 {
		t.Errorf("missing queue file: %d", c)
	}
	if c, _ := envoi("../REQUEST.md", "x.txt", []byte("x")); c != 400 {
		t.Errorf("bad target: %d", c)
	}
	// too large
	taillePieceMax = 10
	defer func() { taillePieceMax = 50 << 20 }()
	if c, v := envoi("demande", "gros.bin", bytes.Repeat([]byte("x"), 11)); c != 400 || v["erreur_key"] != "File too large (50 MB at most)." {
		t.Errorf("too large: %d %v", c, v)
	}
	if _, err := os.Stat(filepath.Join(p.Atelier(), "attachments", "request", "gros.bin")); err == nil {
		t.Error("a too large file was kept")
	}
	// a link in place of the attachments folder: refused
	os.RemoveAll(filepath.Join(p.Atelier(), "attachments"))
	ailleurs := t.TempDir()
	os.Symlink(ailleurs, filepath.Join(p.Atelier(), "attachments"))
	if c, _ := envoi("demande", "y.txt", []byte("y")); c != 400 {
		t.Errorf("link followed: %d", c)
	}
	if e, _ := os.ReadDir(ailleurs); len(e) != 0 {
		t.Error("written through a link")
	}
}

func TestNomPiece(t *testing.T) {
	for in, want := range map[string]string{"photo é.JPG": "photo é.JPG", ".env": "env", "a/b\\c.txt": "c.txt", "...": "file", "nul.txt": "_nul.txt",
		strings.Repeat("x", 200) + ".pdf": strings.Repeat("x", 116) + ".pdf"} {
		if got := nomPiece(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
}
