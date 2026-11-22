package main

// RTK and Ponytail, the same on every Athanor: each version of Athanor names the RTK it downloads (versionRTK) and
// carries the Ponytail ruleset it gives Claude (versionPonytail). Setup & health brings an installed RTK back to
// that version, and only says when a newer one exists upstream: it comes with an update of Athanor, once checked
// there, never installed on one computer alone (two Athanors of the same version would then work differently).

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var reVersionOutil = regexp.MustCompile(`v?([0-9]+\.[0-9]+(?:\.[0-9]+)?)`)

// versionRTKInstallee: what `rtk --version` says ("0.50.0"), "" when it does not answer.
func versionRTKInstallee(chemin string) string {
	out, _ := sortieDe(20*time.Second, chemin, "--version")
	if m := reVersionOutil.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// versionAmont: the latest release of a GitHub repository ("0.52.1"), asked at most once in 12 hours (the answer is
// kept in logs/versions-amont.json); "" when GitHub does not answer. Nothing of this computer is sent.
func versionAmont(kit, depot string) string {
	f := filepath.Join(kit, "logs", "versions-amont.json")
	cache := map[string]struct {
		Version string `json:"version"`
		Quand   int64  `json:"quand"`
	}{}
	if b, err := lireBorne(f, 64<<10); err == nil {
		_ = json.Unmarshal(b, &cache)
	}
	if c, ok := cache[depot]; ok && time.Since(time.Unix(c.Quand, 0)) < 12*time.Hour {
		return c.Version
	}
	cl := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/"+depot+"/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "athanor")
	r, err := cl.Do(req)
	if err != nil {
		return ""
	}
	defer r.Body.Close()
	var rel struct {
		Tag string `json:"tag_name"`
	}
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if r.StatusCode != http.StatusOK || json.Unmarshal(b, &rel) != nil {
		return ""
	}
	v := ""
	if m := reVersionOutil.FindStringSubmatch(rel.Tag); m != nil {
		v = m[1]
	}
	c := cache[depot]
	c.Version, c.Quand = v, time.Now().Unix()
	cache[depot] = c
	if j, err := json.Marshal(cache); err == nil {
		_ = os.MkdirAll(filepath.Dir(f), 0o755)
		_ = publierFichier(f, j)
	}
	return v
}

// noteAmont: the line saying that a newer version exists upstream, "" otherwise.
func noteAmont(kit, nom, depot, notre string) string {
	notre = strings.TrimPrefix(notre, "v")
	if a := versionAmont(kit, depot); a != "" && versionPlusRecente(a, notre) {
		return nom + " " + a + " exists upstream (github.com/" + depot + "): it comes with an update of Athanor once checked there, the same for every Athanor"
	}
	return ""
}
