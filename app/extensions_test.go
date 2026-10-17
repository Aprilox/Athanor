package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLienGitHub(t *testing.T) {
	cas := map[string]lienGH{
		"https://github.com/anthropics/skills":                       {"anthropics", "skills", "", ""},
		"github.com/obra/superpowers/tree/main/skills/writing-plans": {"obra", "superpowers", "main", "skills/writing-plans"},
		"https://github.com/a/b/blob/dev/x/y/SKILL.md":               {"a", "b", "dev", "x/y"},
		"https://www.github.com/a/b.git?tab=readme":                  {"a", "b", "", ""},
	}
	for u, attendu := range cas {
		l, err := lireLienGitHub(u)
		if err != nil || l != attendu {
			t.Fatalf("%s: %+v %v", u, l, err)
		}
	}
	for _, u := range []string{"https://gitlab.com/a/b", "https://github.com/a", "https://github.com/a/b/tree/main/../x", "https://github.com/a b/c"} {
		if _, err := lireLienGitHub(u); err == nil {
			t.Fatalf("%s should be refused", u)
		}
	}
}

func TestEnTete(t *testing.T) {
	n, d := enTete("---\nname: demo\ndescription: \"Use when testing.\"\nlicense: MIT\n---\n# x")
	if n != "demo" || d != "Use when testing." {
		t.Fatalf("%q %q", n, d)
	}
	n, d = enTete("\uFEFF---\r\nname: plie\r\ndescription: >\r\n  first line\r\n  second line\r\n---\r\n")
	if n != "plie" || d != "first line second line" {
		t.Fatalf("folded: %q %q", n, d)
	}
	if n, _ := enTete("# no front matter"); n != "" {
		t.Fatal("no front matter")
	}
}

func TestConfigCollee(t *testing.T) {
	s, err := lireConfigCollee(`{"mcpServers":{"ctx":{"type":"http","url":"https://x.dev/mcp","headers":{"Authorization":"Bearer sk-123"}}}}`, "")
	if err != nil || s["ctx"].URL != "https://x.dev/mcp" {
		t.Fatalf("%v %v", s, err)
	}
	c := s["ctx"]
	sec := separerSecrets(&c)
	if c.Headers["Authorization"] != "Bearer ${AUTHORIZATION}" || sec["AUTHORIZATION"] != "sk-123" {
		t.Fatalf("secret not moved: %+v %v", c, sec)
	}
	s, err = lireConfigCollee(`{"command":"npx","args":["-y","pkg"],"env":{"API_KEY":"YOUR_API_KEY","MODE":"fast"}}`, "solo")
	c = s["solo"]
	sec = separerSecrets(&c)
	if err != nil || c.Command != "npx" || c.Env["API_KEY"] != "${API_KEY}" || c.Env["MODE"] != "fast" || len(sec) != 0 {
		t.Fatalf("single server: %+v %v %v", c, sec, err)
	}
	s, err = lireConfigCollee(`claude mcp add --transport http --header "Authorization: Bearer abc" tavily https://mcp.tavily.com/mcp/`, "")
	if err != nil || s["tavily"].URL != "https://mcp.tavily.com/mcp/" || s["tavily"].Headers["Authorization"] != "Bearer abc" {
		t.Fatalf("claude mcp add http: %+v %v", s, err)
	}
	s, err = lireConfigCollee("claude mcp add serena -e TOKEN=x -- serena start-mcp-server --project \"$(pwd)\"", "")
	if err != nil || s["serena"].Command != "serena" || len(s["serena"].Args) != 3 || s["serena"].Env["TOKEN"] != "x" {
		t.Fatalf("claude mcp add stdio: %+v %v", s, err)
	}
	s, err = lireConfigCollee(`claude mcp add-json github '{"type":"http","url":"https://api.githubcopilot.com/mcp"}'`, "")
	if err != nil || s["github"].URL == "" {
		t.Fatalf("add-json: %+v %v", s, err)
	}
	if _, err := lireConfigCollee("rm -rf /", ""); err == nil {
		t.Fatal("garbage accepted")
	}
}

func zipDeTest(t *testing.T, fichiers map[string]string, liens map[string]string) *zip.Reader {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for nom, contenu := range fichiers {
		w, _ := z.Create(nom)
		w.Write([]byte(contenu))
	}
	for nom, cible := range liens {
		h := &zip.FileHeader{Name: nom}
		h.SetMode(os.ModeSymlink | 0o777)
		w, _ := z.CreateHeader(h)
		w.Write([]byte(cible))
	}
	z.Close()
	r, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSkillsArchive(t *testing.T) {
	z := zipDeTest(t, map[string]string{
		"repo-main/README.md":               "x",
		"repo-main/skills/a/SKILL.md":       "---\nname: alpha\ndescription: A.\n---\n",
		"repo-main/skills/a/scripts/run.py": "print(1)",
		"repo-main/skills/a/ref/notes.md":   "notes",
		"repo-main/skills/b/SKILL.md":       "---\nname: Beta Skill\ndescription: B.\n---\n",
		"repo-main/autre/c/SKILL.md":        "---\ndescription: C.\n---\n",
	}, map[string]string{"repo-main/skills/a/fuite": "/etc/passwd"})
	tous := skillsDansArchive(z, "")
	if len(tous) != 3 {
		t.Fatalf("found %d", len(tous))
	}
	sous := skillsDansArchive(z, "skills")
	if len(sous) != 2 || sous[0].ID != "alpha" || !sous[0].Scripts || sous[1].ID != "beta-skill" || sous[1].Scripts {
		t.Fatalf("%+v", sous)
	}
	if c := skillsDansArchive(z, "autre/c"); len(c) != 1 || c[0].ID != "c" {
		t.Fatalf("name from folder: %+v", c)
	}
	k := Kit{t.TempDir()}
	if err := extraireSkill(k, z, "skills/a", "alpha"); err != nil {
		t.Fatal(err)
	}
	d := filepath.Join(dossierExt(k), "skills", "alpha")
	for _, f := range []string{"SKILL.md", "scripts/run.py", "ref/notes.md"} {
		if _, err := os.Stat(filepath.Join(d, f)); err != nil {
			t.Fatalf("%s missing", f)
		}
	}
	if _, err := os.Lstat(filepath.Join(d, "fuite")); err == nil {
		t.Fatal("a link was copied")
	}
	if err := extraireSkill(k, z, "skills/a", "../x"); err == nil {
		t.Fatal("bad id accepted")
	}
}

func TestIdentExt(t *testing.T) {
	for in, out := range map[string]string{"My Skill!": "my-skill", "  --x--  ": "x", "Été": "t", "a.b_c": "a.b_c"} {
		if g := identExt(in); g != out {
			t.Fatalf("%q -> %q, want %q", in, g, out)
		}
	}
}

func TestNomSecret(t *testing.T) {
	for nom, attendu := range map[string]bool{"API_KEY": true, "TAVILY_API_KEY": true, "x-api-key": true, "Authorization": true, "GITHUB_PAT": true, "GITHUB_PERSONAL_ACCESS_TOKEN": true,
		"MEMORY_FILE_PATH": false, "NODE_ENV": false, "PATH": false, "KEYBOARD_LAYOUT": false, "DEBUG": false} {
		if estNomSecret(nom) != attendu {
			t.Fatalf("%s: %v", nom, !attendu)
		}
	}
}
