package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fauxKubectl (M139): logs every call; apply fails when its folder holds a file ECHEC; get lists one Deployment
// and one Service (charges keeps the Deployment only).
const fauxKubectl = `#!/bin/sh
echo "kubectl $*" >> /var/log/faux-kubectl.log
case "$*" in
  *" apply "*) for x; do d=$x; done; [ -e "$d/ECHEC" ] && { echo "error: essai d'échec" >&2; exit 1; } ;;
  *" get "*) printf 'Deployment   site   web\nService   site   web\n' ;;
esac
exit 0
`

// TestDeploiementK3sConteneur (M139): never detected; refused while k3s is missing; apply -f, then -k with a
// kustomization, with the server's kubeconfig and the Deployment waited for; a failed apply applies the version
// served again and switches nothing; going back = rollout undo of the version left; the kubeconfig's content
// appears in no answer, history, audit line or kubectl argument.
func TestDeploiementK3sConteneur(t *testing.T) {
	web := os.Getenv("ATHANOR_SSH_HTTP")
	if web == "" {
		t.Skip("ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	s, _, id, _, appel := serveurConteneurTest(t)
	man := func(v string, extra ...string) map[string]string {
		a := map[string]string{"k8s/site.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: site, namespace: web}\n# " + v + "\n"}
		for _, f := range extra {
			a["k8s/"+f] = "resources: [site.yaml]\n"
		}
		return a
	}
	depot, c := depotCompose(t, man("v1"), man("v2", "kustomization.yaml"), man("v3", "kustomization.yaml", "ECHEC"))
	if m, err := detecterMethode(depot, c[0], "k8s"); err != nil || m != "statique" {
		t.Errorf("k3s is never detected: %q %v", m, err)
	}
	s.mu.Lock()
	s.config.Projets = append(s.config.Projets, Projet{Nom: "m139", Dossier: depot})
	s.mu.Unlock()
	const hote, base, temoin = "m139.test", "/srv/athanor/m139-dev", "temoin-kubeconfig-5521"
	resoudreTest(t, hote+":80", web)
	if err := s.acces.DefinirEnvironnement("m139", "dev", Environnement{URL: "http://" + hote, Serveurs: []string{id}, Protection: "aucune", Politique: "auto"}); err != nil {
		t.Fatal(err)
	}
	sur := func(cmd string) string { return appel("ssh_commande", map[string]any{"commande": cmd}).Sortie }
	// encoded so that no tool argument names the kubeconfig (an agent may not)
	racine := func(script string) string {
		return sur("echo " + base64.StdEncoding.EncodeToString([]byte(script)) + " | base64 -d | sudo -n sh")
	}
	nettoyer := "rm -rf " + base + " /etc/rancher /usr/local/bin/kubectl /var/log/faux-kubectl.log; true"
	racine(nettoyer)
	t.Cleanup(func() { racine(nettoyer) })
	ctx := context.Background()
	cfg := &ConfigDeploiement{Methode: "k3s", Source: "artefact", Dossier: "k8s"}

	if d, err := s.deployer(ctx, "m139", "dev", c[0], OptionsDeploiement{Config: cfg, Auteur: "proprietaire"}); err == nil || !strings.Contains(err.Error(), "k3s is not installed") || d.Etat != "echec" {
		t.Fatalf("without k3s: %v %+v", err, d)
	}
	racine("mkdir -p /etc/rancher/k3s && echo 'token: " + temoin + "' > " + kubeconfigK3s + " && chmod 600 " + kubeconfigK3s +
		" && echo " + base64.StdEncoding.EncodeToString([]byte(fauxKubectl)) + " | base64 -d > /usr/local/bin/kubectl && chmod 755 /usr/local/bin/kubectl && touch /var/log/faux-kubectl.log")
	journal := func() string { return racine("cat /var/log/faux-kubectl.log; : > /var/log/faux-kubectl.log") }
	k := "kubectl --kubeconfig " + kubeconfigK3s + " "
	appliquer := func(f, d string) string {
		return k + "apply " + f + " " + d + "\n" + k + "get " + f + " " + d + " -o custom-columns=K:.kind,N:.metadata.name,NS:.metadata.namespace --no-headers\n" +
			k + "rollout status Deployment/site -n web --timeout=180s\n"
	}

	d1, err := s.deployer(ctx, "m139", "dev", c[0], OptionsDeploiement{Config: cfg, Auteur: "proprietaire"})
	if err != nil || d1.Etat != "ok" || d1.Methode != "k3s" {
		t.Fatalf("v1: %v %+v", err, d1)
	}
	r1 := base + "/releases/" + d1.Version + "/k8s"
	if got, want := journal(), appliquer("-f", r1); got != want {
		t.Errorf("kubectl for v1:\n got  %q\n want %q", got, want)
	}
	d2, err := s.deployer(ctx, "m139", "dev", c[1], OptionsDeploiement{Auteur: "relais"}) // the recorded method
	r2 := base + "/releases/" + d2.Version + "/k8s"
	if got, want := journal(), appliquer("-k", r2); err != nil || got != want {
		t.Fatalf("v2: %v %+v\n got  %q\n want %q", err, d2, got, want)
	}
	// a failed apply: the release removed, v2 applied again, nothing switched
	d3, err := s.deployer(ctx, "m139", "dev", c[2], OptionsDeploiement{Auteur: "proprietaire"})
	if err == nil || d3.Etat != "echec" || !strings.Contains(err.Error(), "kubectl apply failed") || !strings.Contains(err.Error(), "essai d'échec") {
		t.Fatalf("failed apply: %v %+v", err, d3)
	}
	r3 := base + "/releases/" + d3.Version + "/k8s"
	if got, want := journal(), k+"apply -k "+r3+"\n"+appliquer("-k", r2); got != want {
		t.Errorf("kubectl after the failed apply:\n got  %q\n want %q", got, want)
	}
	if got, want := sur("cd "+base+" && readlink current previous && ls releases | wc -l"), "releases/"+d2.Version+"\nreleases/"+d1.Version+"\n2\n"; got != want {
		t.Errorf("after the failed apply: %q, want %q", got, want)
	}
	// going back (C72): the release now current (v1) applied again, never rollout undo
	rr, err := s.revenir(ctx, "m139", "dev", "proprietaire", "")
	if err != nil || rr.Etat != "ok" || rr.Version != d1.Version || rr.Methode != "k3s" {
		t.Fatalf("back to v1: %v %+v", err, rr)
	}
	if got, want := journal(), appliquer("-f", base+"/current/k8s"); got != want || strings.Contains(got, "undo") {
		t.Errorf("kubectl when going back:\n got  %q\n want %q", got, want)
	}
	if got := sur("cd " + base + " && readlink current"); got != "releases/"+d1.Version+"\n" {
		t.Errorf("current after going back: %q", got)
	}
	// witness: the kubeconfig never leaves the server
	h, _ := json.Marshal(historiqueDeploiements("m139", "dev"))
	reponses, _ := json.Marshal([]any{d1, d2, d3, rr})
	for nom, texte := range map[string]string{"history": string(h), "answers": string(reponses), "kubectl": racine("cat /var/log/faux-kubectl.log"), "processes": sur("ps -eo args")} {
		if strings.Contains(texte, temoin) {
			t.Errorf("the kubeconfig's content is in the %s", nom)
		}
	}
	for _, d := range []string{dossierModule("servers"), dossierJournalAudit()} {
		_ = filepath.WalkDir(d, func(p string, x fs.DirEntry, err error) error {
			if err == nil && !x.IsDir() {
				if b, _ := os.ReadFile(p); strings.Contains(string(b), temoin) {
					t.Errorf("the kubeconfig's content is in %s", p)
				}
			}
			return nil
		})
	}
}

// TestConfigK3s (M139): k3s is a known method, an unknown one is refused.
func TestConfigK3s(t *testing.T) {
	if err := validerConfigDeploiement(ConfigDeploiement{Methode: "k3s", Source: "git"}); err != nil {
		t.Errorf("k3s refused: %v", err)
	}
	if err := validerConfigDeploiement(ConfigDeploiement{Methode: "helm", Source: "git"}); err == nil {
		t.Error("an unknown method accepted")
	}
}

// TestK3sReel (M139, .github/workflows/k3s.yml only, ATHANOR_TEST_K3S=1 on a runner where k3s runs): the very
// scripts of the k3s method run as root on this machine against a real cluster: v1 applied (-f), v2 applied (-k),
// then going back rolls the Deployment back to v1.
func TestK3sReel(t *testing.T) {
	if os.Getenv("ATHANOR_TEST_K3S") != "1" {
		t.Skip("ATHANOR_TEST_K3S=1 absent (k3s workflow only)")
	}
	ns := "apiVersion: v1\nkind: Namespace\nmetadata: {name: athanor-essai}\n"
	site := func(v string) string {
		return "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: site, namespace: athanor-essai}\nspec:\n  replicas: 1\n" +
			"  selector: {matchLabels: {app: site}}\n  template:\n    metadata: {labels: {app: site}}\n    spec:\n      containers:\n" +
			"        - name: site\n          image: busybox:1.36\n          command: [sleep, \"3600\"]\n          env: [{name: VERSION, value: " + v + "}]\n"
	}
	depot, c := depotCompose(t,
		map[string]string{"k8s/00-namespace.yaml": ns, "k8s/site.yaml": site("v1")},
		map[string]string{"k8s/00-namespace.yaml": ns, "k8s/site.yaml": site("v2"), "k8s/kustomization.yaml": "resources: [00-namespace.yaml, site.yaml]\n"})
	base := filepath.ToSlash(t.TempDir())
	t.Cleanup(func() {
		_ = exec.Command("sudo", "-n", "rm", "-rf", base+"/releases", base+"/current", base+"/previous").Run()
	}) // root's files, before TempDir's own cleanup
	root := func(script string, args ...string) string {
		t.Helper()
		out, err := exec.Command("sudo", append([]string{"-n", "sh", "-c", script, "sh"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return string(out)
	}
	version := func() string {
		return strings.TrimSpace(root(`kubectl --kubeconfig ` + kubeconfigK3s + ` -n athanor-essai get deploy site -o jsonpath='{.spec.template.spec.containers[0].env[0].value}'`))
	}
	for i, commit := range c {
		archive := filepath.Join(t.TempDir(), "a.tar.gz")
		if out, err := exec.Command("git", "-C", depot, "archive", "--format=tar.gz", "-o", archive, commit).CombinedOutput(); err != nil {
			t.Fatalf("git archive: %v %s", err, out)
		}
		v := "v" + string(rune('1'+i))
		if out := root(scriptPreparerK3s, archive, "", base, v, "k8s"); !strings.HasSuffix(out, "ready\n") {
			t.Fatalf("%s: %q", v, out)
		}
		root(scriptBascule, base, v)
		if got := version(); got != v {
			t.Fatalf("after %s: the Deployment runs %q", v, got)
		}
	}
	root(scriptBascule, base, "v1")
	root(scriptRevenirK3s, base, "k8s")
	if got := version(); got != "v1" {
		t.Errorf("after going back: the Deployment runs %q, want v1", got)
	}
}
