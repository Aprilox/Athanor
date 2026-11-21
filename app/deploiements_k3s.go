package main

// The k3s deployment method (M139, ARCHITECTURE.md §Déploiements), only when the owner asks for it (never
// detected): the release extracted like a static one, then `kubectl apply -k` (a kustomization in the folder) or
// `-f` of its folder with the server's own kubeconfig, which never leaves the server; its workloads are waited for
// before the switch. Going back = the release then current applied again (C72). One server per
// environment: the cluster spreads the pods itself, and applying or undoing twice on one cluster would be wrong.
// C69: without the owner's accord for this deployment, refusK3s refuses the manifests that give root.

import (
	"errors"
	"os/exec"
	"path"
	"strings"
)

const kubeconfigK3s = "/etc/rancher/k3s/k3s.yaml"

// fonctionsK3s ($b the environment's folder, $dd the manifests' folder): cible DIR sets $d and $f (-k or -f);
// charges lists the workloads of $d as "Kind/name namespace"; appliquer DIR applies a release and waits for its
// workloads; relancer applies the version served (current) again, nothing when nothing is served yet.
const fonctionsK3s = `k() { kubectl --kubeconfig ` + kubeconfigK3s + ` "$@"; }
cible() { d=$1${dd:+/$dd}; [ -d "$d" ] || { echo "the release has no folder $dd"; return 1; }; f=-f; for n in kustomization.yaml kustomization.yml Kustomization; do [ -f "$d/$n" ] && f=-k; done; return 0; }
charges() { k get $f "$d" -o custom-columns=K:.kind,N:.metadata.name,NS:.metadata.namespace --no-headers | while read -r K N NS; do case $K in Deployment|StatefulSet|DaemonSet) echo "$K/$N $NS";; esac; done; }
appliquer() { cible "$1" || return 1; o=$(k apply $f "$d" 2>&1) || { echo "kubectl apply failed:"; printf '%s\n' "$o" | tail -n 8; return 4; }
  o=$(charges | while read -r c ns; do k rollout status "$c" -n "$ns" --timeout=180s 2>&1 || exit 1; done) || { echo "the rollout did not finish:"; printf '%s\n' "$o" | tail -n 8; return 4; }; }
relancer() { c=$(readlink -f -- "$b/current") && [ -d "$c" ] || return 0; appliquer "$c" >/dev/null || echo "(the version served before could not be applied again)"; }
`

// scriptPreparerK3s ($1 the archive, $2 its SHA-256 or "", $3 the environment's folder, $4 the version, $5 the
// manifests' folder): the release applied; on failure it is removed and the version served before applied again.
const scriptPreparerK3s = `a=$1 b=$3 r=$3/releases/$4 dd=$5
command -v kubectl >/dev/null 2>&1 && [ -f ` + kubeconfigK3s + ` ] || { rm -f -- "$a"; echo "k3s is not installed on this server (` + kubeconfigK3s + ` missing): install it first"; exit 5; }
` + extraireVersion + fonctionsK3s + `appliquer "$r" || { e=$?; relancer; rm -rf -- "$r"; exit $e; }
echo ready`

// scriptAnnulerK3s ($1 folder, $2 version, $3 manifests' folder): the version served before applied again, the
// release removed.
const scriptAnnulerK3s = `b=$1 dd=$3
` + fonctionsK3s + `relancer; rm -rf -- "$1/releases/$2"`

// Manifest lines that give root on the server (C69): a host path, the host's namespaces, a privileged
// container; in a kustomization, a remote base (its content is not in the commit the owner can read).
const (
	motifsHoteK3s    = `["']?(hostPath["']?[[:space:]]*:|(hostNetwork|hostPID|hostIPC|privileged)["']?[[:space:]]*:[[:space:]]*["']?true)`
	motifsDistantK3s = `://|[?&]ref=|^[[:space:]]*-?[[:space:]]*["']?(github\.com|gitlab\.com|bitbucket\.org)/`
)

// refusK3s (C69): the manifests of commit's folder dossier that would give root on the server; refused unless
// the owner accepted this very deployment. A folder git cannot read is refused too.
func refusK3s(depot, commit, dossier string) []string {
	racine := "."
	if dossier != "" {
		racine = dossier
	}
	var r []string
	for _, x := range []struct{ motif, glob, quoi string }{
		{motifsHoteK3s, "**", "host access"},
		{motifsDistantK3s, "**/[Kk]ustomization*", "remote base"},
	} {
		o, err := gitSortie(depot, "grep", "-I", "-E", "-e", x.motif, commit, "--", ":(glob)"+path.Join(racine, x.glob))
		var e *exec.ExitError
		if errors.As(err, &e) && e.ExitCode() == 1 { // no line found
			continue
		} else if err != nil {
			return []string{"the manifests of the commit cannot be read"}
		}
		for _, l := range strings.Split(o, "\n") {
			if _, l, ok := strings.Cut(l, ":"); ok { // commit:file:line
				f, ligne, _ := strings.Cut(l, ":")
				r = append(r, f+": "+x.quoi+" ("+couper(strings.TrimSpace(ligne), 80)+")")
			}
		}
	}
	return r
}
