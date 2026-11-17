package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// TestFichiersConteneurChanges (C69): with compose, the compose files and Dockerfiles changed since the last
// accepted deployment need the owner; with k3s, any file of the manifests' folder.
func TestFichiersConteneurChanges(t *testing.T) {
	depot, c := depotEssai(t,
		map[string]string{"app/compose.yaml": "services: {}", "app/Dockerfile": "FROM nginx", "app/index.html": "v1", "k8s/deploy.yaml": "kind: Deployment", "README.md": "x"},
		map[string]string{"app/index.html": "v2", "README.md": "y"},
		map[string]string{"app/docker-compose.override.yml": "services: {}", "app/web.Dockerfile": "FROM x"},
		map[string]string{"k8s/sous/patch.yaml": "x"})
	p := Projet{Nom: "m144-fichiers", Dossier: depot}
	_ = os.Remove(cheminHistorique(p.Nom, "dev"))
	t.Cleanup(func() { _ = os.Remove(cheminHistorique(p.Nom, "dev")) })
	compose := &ConfigDeploiement{Methode: "compose", Source: "artefact", Dossier: "app"}
	k3s := &ConfigDeploiement{Methode: "k3s", Source: "artefact", Dossier: "k8s"}
	egal := func(nom string, f []string, attendu ...string) {
		t.Helper()
		if !slices.Equal(f, attendu) {
			t.Errorf("%s: %v, expected %v", nom, f, attendu)
		}
	}
	egal("compose, nothing accepted yet", fichiersBuildChanges(p, "dev", c[0], compose), "app/Dockerfile", "app/compose.yaml")
	egal("k3s, nothing accepted yet", fichiersBuildChanges(p, "dev", c[0], k3s), "k8s/deploy.yaml")
	if err := noterHistorique(Deploiement{ID: "a", Projet: p.Nom, Env: "dev", Commit: c[0], Accorde: true}); err != nil {
		t.Fatal(err)
	}
	egal("compose, site only", fichiersBuildChanges(p, "dev", c[1], compose))
	egal("k3s, site only", fichiersBuildChanges(p, "dev", c[1], k3s))
	egal("compose, an override and a named Dockerfile", fichiersBuildChanges(p, "dev", c[2], compose), "app/docker-compose.override.yml", "app/web.Dockerfile")
	egal("k3s, a file deeper in the folder", fichiersBuildChanges(p, "dev", c[3], k3s), "k8s/sous/patch.yaml")
}

// TestRefusCompose (C69): what gives root on the server is listed, the rest passes; unreadable is refused.
func TestRefusCompose(t *testing.T) {
	const r = "/srv/athanor/p-dev/releases/20261113-1"
	sain := `{"name":"p-dev","services":{"web":{"image":"nginx","ports":[{"target":80,"published":"8080","host_ip":"127.0.0.1"}],"volumes":[{"type":"bind","source":"` + r + `/app/data","target":"/data"},{"type":"volume","source":"db","target":"/var/lib/db"}],"network_mode":"bridge","ipc":"private"}},"volumes":{"db":{}}}`
	if l := refusCompose("ready\n"+sain, r); len(l) != 0 {
		t.Errorf("a plain configuration refused: %v", l)
	}
	dur := `{"services":{"a":{"privileged":true,"cap_add":["SYS_ADMIN"],"devices":[{"source":"/dev/sda","target":"/dev/sda"}],"security_opt":["apparmor=unconfined"],"pid":"host","ports":[{"target":3000,"published":"3000"},{"target":53,"published":"53","host_ip":"0.0.0.0","protocol":"udp"},{"target":81,"host_ip":"::1"}]},
"b":{"network_mode":"host","ipc":"container:autre","userns_mode":"host","volumes":[{"type":"bind","source":"/","target":"/host"},{"type":"bind","source":"` + r + `/../../../../etc","target":"/e"},{"type":"bind","source":"/srv/athanor/p-dev/releases/20261113-10","target":"/x"}]}},
"volumes":{"v":{"driver_opts":{"type":"none","o":"bind","device":"/etc/athanor"}}},"secrets":{"s":{"file":"/etc/shadow"}},"configs":{"c":{"file":"` + r + `/app/c.conf"}}}`
	attendu := []string{
		"secret s: the host's /etc/shadow",
		"service a: cap_add SYS_ADMIN", "service a: devices", "service a: pid host", "service a: port 3000 published beyond 127.0.0.1", "service a: port 53 published beyond 127.0.0.1", "service a: privileged", "service a: security_opt apparmor=unconfined",
		"service b: ipc container:autre", "service b: mounts the host's /", "service b: mounts the host's " + r + "/../../../../etc", "service b: mounts the host's /srv/athanor/p-dev/releases/20261113-10",
		"service b: network_mode host", "service b: userns_mode host",
		"volume v: a host device or path",
	}
	if l := refusCompose(dur, r); !slices.Equal(l, attendu) {
		t.Errorf("refused:\n got  %q\n want %q", l, attendu)
	}
	for _, x := range []string{"", "docker compose config failed", `{"services":{"a":{"privileged":"yes"}}}`} {
		if l := refusCompose(x, r); len(l) != 1 || !strings.Contains(l[0], "cannot be read") {
			t.Errorf("%q: %v", x, l)
		}
	}
}

// TestAmontCompose (C70): Caddy proxies to the first TCP port published (services in name order), on 127.0.0.1.
func TestAmontCompose(t *testing.T) {
	for sortie, want := range map[string]string{
		`{"services":{"b":{"ports":[{"target":80,"published":"8081","host_ip":"127.0.0.1"}]},"a":{"ports":[{"target":53,"published":"53","protocol":"udp"},{"target":3000,"published":3000,"host_ip":"127.0.0.1","protocol":"tcp"}]}}}`: "127.0.0.1:3000",
		`ready` + "\n" + `{"services":{"w":{"ports":[{"target":80,"published":"8000-8001"}]},"x":{"ports":[{"target":80,"published":"8080"}]}}}`:                                                                                        "127.0.0.1:8080",
		`{"services":{"worker":{"image":"x"}}}`: "",
		`not json`:                              "",
	} {
		if g := amontCompose(sortie); g != want {
			t.Errorf("%s: %q, want %q", sortie, g, want)
		}
	}
}

// TestRefusK3s (C69): host paths, host namespaces, privileged containers and remote bases are refused, in the
// manifests' folder only; a sound folder passes.
func TestRefusK3s(t *testing.T) {
	sain := "apiVersion: apps/v1\nkind: Deployment\nspec:\n  template:\n    spec:\n      hostNetwork: false\n      containers:\n      - name: web\n        args: [\"--url\", \"https://api.example.com\"]\n        securityContext:\n          privileged: false\n"
	depot, c := depotEssai(t,
		map[string]string{"k8s/deploy.yaml": sain, "k8s/kustomization.yaml": "resources:\n- deploy.yaml\n", "autre/hote.yaml": "hostPath:\n  path: /\n"},
		map[string]string{"k8s/deploy.yaml": sain + "      volumes:\n      - name: h\n        hostPath:\n          path: /\n      hostPID: true\n",
			"k8s/sous/p.json":        `{"spec":{"containers":[{"securityContext":{"privileged": true}}]}}`,
			"k8s/kustomization.yaml": "resources:\n- deploy.yaml\n- github.com/autre/depot//base?ref=v1\n- https://example.com/m.yaml\n"})
	if l := refusK3s(depot, c[0], "k8s"); len(l) != 0 {
		t.Errorf("sound manifests refused: %v", l)
	}
	l := refusK3s(depot, c[1], "k8s")
	joint := strings.Join(l, "\n")
	for _, x := range []string{"k8s/deploy.yaml: host access (hostPath:)", "k8s/deploy.yaml: host access (hostPID: true)", "k8s/sous/p.json: host access", "k8s/kustomization.yaml: remote base (- github.com/autre/depot//base?ref=v1)", "k8s/kustomization.yaml: remote base (- https://example.com/m.yaml)"} {
		if !strings.Contains(joint, x) {
			t.Errorf("%q missing in:\n%s", x, joint)
		}
	}
	if len(l) != 5 {
		t.Errorf("%d refusals: %v", len(l), l)
	}
	if l := refusK3s(depot, strings.Repeat("0", 40), "k8s"); len(l) != 1 || !strings.Contains(l[0], "cannot be read") {
		t.Errorf("an unknown commit: %v", l)
	}
}

// TestDemanderDeploiementComposeChange (C69): in dev auto, a changed compose file is destructive: the owner's
// accord, never the policy.
func TestDemanderDeploiementComposeChange(t *testing.T) {
	s, _, _ := serveursAjoutTest(t)
	var err error
	if s.approbations, err = ouvrirApprobations(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	depot, c := depotEssai(t, map[string]string{"compose.yaml": "services: {}", "index.html": "v1"})
	p := Projet{Nom: "m144-accord", Dossier: depot}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, p)
	s.mu.Unlock()
	if err := s.acces.definirAncien(p.Nom, "dev", Environnement{URL: "http://m144.test", Serveurs: []string{"ab12"}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	if err := s.acces.NoterDeploiement(p.Nom, "dev", ConfigDeploiement{Methode: "compose", Source: "artefact"}); err != nil {
		t.Fatal(err)
	}
	txt, err := outilDemanderDeploiement(s, SessionHub{Projet: p.Nom, Mission: "M1", Agent: "pilote"}, []byte(`{"environnement":"dev","commit":"`+c[0]+`"}`))
	_, reste, _ := strings.Cut(txt, "waiting for the owner, id ")
	id, _, _ := strings.Cut(reste, ":")
	if ap, ok := s.approbations.Lire(id); err != nil || !ok || ap.Details["fichiers_build"] != "compose.yaml" {
		t.Fatalf("demander_deploiement: %v %q %+v", err, txt, ap)
	}
}
