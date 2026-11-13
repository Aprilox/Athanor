package main

// The git source of a deployment (M132, ARCHITECTURE.md §Déploiements): the server fetches the commit itself
// from the project's repository with the project's deployment key (stage 7). The key goes from the vault to
// /etc/athanor/deploy/<projet> (0600 root, only during the fetch) through the script's standard input, never
// through an agent nor an argument; ssh uses only it (IdentitiesOnly) and only the pinned host keys (no trust on first use).

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

// hotesGitHub: github.com's SSH host keys, as published by https://api.github.com/meta (ssh_keys).
const hotesGitHub = `github.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl
github.com ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBEmKSENjQEezOmxkZMy7opKgwFB9nkt5YRrYMjNuG5N87uRgg6CLrbo5wAdT/y6v0mKV0U2w0WZ2YB/++Tpockg=
github.com ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQCj7ndNxQowgcQnjshcLrqPEiiphnt+VTTvDP6mHBL9j1aNUkY4Ue1gvwnGLVlOhGeYrnZaMgRK6+PKCUXaDbC7qtbW8gIkhL7aGCsOr/C56SJMy/BCZfxd1nWzAOxSDPgVsmerOBYfNqltV9/hWCqBywINIR+5dIg6JTJ72pcEpEjcYgXkE2YEFXV1JHnsKgbLWNlhScqb2UmyRkQyytRLtL+38TGxkxCflmO+5Z8CSSNY7GidjMIZ7Q4zMjA2n1nGrlTDkzwDCsw+wqFPGQA179cnfGWOWRVruj16z6XyvxvjJwbz0wQZ75XK5tKSb7FNyeIEs4TT4jk+S4dhPeAUC5y+bDYirYgM4GC7uEnztnZyaVWQ7B381AK4Qdrwt51ZqExKbQpTUNn+EjqoTwvqNj4kqx5QUCI0ThS/YkOxJCXmPUWZbhjpCg56i+2aB6CmK2JGhn57K5mj0MNdBXA4/WnwH6XoPWJzK5Nyu2zB3nAZp+S5hpQs+p1vN1/wsjk=`

var reOrigineGitHub = regexp.MustCompile(`^(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// sourceGit: where a server fetches the commit, the host keys it accepts, the key (clear, from the vault) and
// its file on the server.
type sourceGit struct{ url, hotes, cle, fichier string }

// depotGitServeur: the project's GitHub repository over SSH and github.com's pinned keys (tests: a repository
// served by the test container).
var depotGitServeur = func(p Projet) (url, hotes string, err error) {
	o, err := gitSortie(p.Dossier, "remote", "get-url", "origin")
	m := reOrigineGitHub.FindStringSubmatch(strings.TrimSpace(o))
	if err != nil || m == nil {
		return "", "", errors.New("the git source fetches from GitHub: the project's origin is not a GitHub repository (use the artefact source)")
	}
	return "git@github.com:" + m[1] + "/" + m[2] + ".git", hotesGitHub, nil
}

// scriptRecupererGit ($1 the environment's folder, $2 the key's file, $3 the repository, $4 the commit, $5 the
// pinned host keys, $6 the version; the private key on standard input): the key installed, the commit fetched
// into $1/depot.git and checked, archived for scriptPreparer; the archive's path on the last line. The key is
// removed right after the fetch (C69): a container of another project cannot read it later.
const scriptRecupererGit = `b=$1 k=$2 a=$1/.git-$6.tar.gz
umask 077
{ [ -d /etc/athanor ] || install -d -m 755 /etc/athanor; } && install -d -m 700 /etc/athanor/deploy || exit 1
t=$(mktemp /etc/athanor/deploy/.cle.XXXXXX) && cat > "$t" && chmod 600 "$t" && mv -f -- "$t" "$k" || { rm -f -- "$t"; echo "the deployment key cannot be installed"; exit 1; }
printf '%s\n' "$5" > "$k.known_hosts" || exit 1
command -v git >/dev/null 2>&1 || { echo "git is not installed on this server: install it first"; exit 5; }
umask 022
mkdir -p -- "$b" && { [ -d "$b/depot.git" ] || git init -q --bare "$b/depot.git"; } || { echo "the repository cache cannot be made"; exit 1; }
export GIT_SSH_COMMAND="ssh -i $k -o IdentitiesOnly=yes -o IdentityAgent=none -o StrictHostKeyChecking=yes -o UserKnownHostsFile=$k.known_hosts -o BatchMode=yes -o ConnectTimeout=30"
o=$(git -C "$b/depot.git" fetch -q --depth 1 --no-tags "$3" "$4" 2>&1); f=$?
rm -f -- "$k"
[ $f = 0 ] || { echo "git fetch of $4 failed (is the commit pushed to the repository?):"; { printf '%s\n' "$o" | grep -Ei 'error|fatal|denied|host key|refused|not found|could not' || printf '%s\n' "$o" | tail -n 4; } | head -n 4; exit 6; }
git -C "$b/depot.git" rev-parse -q --verify "$4^{commit}" >/dev/null || { echo "the commit $4 is not in the fetched repository"; exit 6; }
git -C "$b/depot.git" archive --format=tar.gz -o "$a" "$4" || { rm -f -- "$a"; echo "git archive failed on the server"; exit 1; }
echo "$a"`

// recupererGit: on the server of c, the commit fetched by git; the answer: the archive's path there.
func recupererGit(ctx context.Context, c *ssh.Client, g *sourceGit, base, commit, version string) (string, error) {
	r, err := executerSSH(ctx, c, commeRoot(scriptRecupererGit, base, g.fichier, g.url, commit, g.hotes, version), strings.NewReader(strings.TrimSpace(g.cle)+"\n"), delaiDeploiement)
	if err = echecSSH(r, err); err != nil {
		return "", err
	}
	l := strings.Split(strings.TrimSpace(string(r.Sortie)), "\n")
	if a := l[len(l)-1]; strings.HasPrefix(a, base+"/.git-") {
		return a, nil
	}
	return "", errMoteur("unexpected answer of the git fetch: \"{0}\"", couper(string(r.Sortie), 200))
}
