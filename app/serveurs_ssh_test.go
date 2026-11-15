package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// serveurSSHMemoire : un serveur SSH de test (x/crypto/ssh côté serveur) qui accepte une clé publique et un mot de passe,
// et répond aux commandes « exec » par repondre(commande, entrée).
type serveurSSHMemoire struct {
	hote, empreinte string
	port            int
	commandes       chan string
	mu              sync.Mutex
	autorisees      []ssh.PublicKey // keys let in besides the one given at launch (autoriser)
	fichiers        sftp.Handlers   // its in-memory SFTP files, kept across connections
	tailles         chan string     // PTY sizes asked ("pty 80x24", "resize 120x40"), for the terminal's tests
	invite          string          // the shell's first words (the captures' coloured prompt); "$ " when empty
}

// autoriser: the key is now in the server's authorized_keys.
func (s *serveurSSHMemoire) autoriser(k ssh.PublicKey) {
	s.mu.Lock()
	s.autorisees = append(s.autorisees, k)
	s.mu.Unlock()
}

func nouvelleCleSSH(t *testing.T) (ssh.Signer, []byte) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bloc, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return signer, pem.EncodeToMemory(bloc)
}

func lancerSSHMemoire(t *testing.T, autorisee ssh.PublicKey, motDePasse string, repondre func(cmd string, entree []byte) (string, int)) *serveurSSHMemoire {
	t.Helper()
	hote, _ := nouvelleCleSSH(t)
	s := &serveurSSHMemoire{hote: "127.0.0.1", empreinte: ssh.FingerprintSHA256(hote.PublicKey()), commandes: make(chan string, 100), fichiers: sftp.InMemHandler(), tailles: make(chan string, 100)}
	if autorisee != nil {
		s.autoriser(autorisee)
	}
	conf := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, a := range s.autorisees {
				if bytes.Equal(k.Marshal(), a.Marshal()) {
					return nil, nil
				}
			}
			return nil, os.ErrPermission
		},
		PasswordCallback: func(_ ssh.ConnMetadata, mdp []byte) (*ssh.Permissions, error) {
			if motDePasse != "" && string(mdp) == motDePasse {
				return nil, nil
			}
			return nil, os.ErrPermission
		},
	}
	conf.AddHostKey(hote)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	s.port = l.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.servir(c, conf, repondre)
		}
	}()
	return s
}

func (s *serveurSSHMemoire) servir(c net.Conn, conf *ssh.ServerConfig, repondre func(string, []byte) (string, int)) {
	defer c.Close()
	_, canaux, requetes, err := ssh.NewServerConn(c, conf)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requetes)
	for nc := range canaux {
		if nc.ChannelType() != "session" {
			nc.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		canal, reqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			defer canal.Close()
			for r := range reqs {
				if r.Type == "subsystem" && len(r.Payload) > 4 && string(r.Payload[4:]) == "sftp" {
					r.Reply(true, nil)
					fs := sftp.NewRequestServer(canal, s.fichiers)
					fs.Serve()
					fs.Close()
					return
				}
				if s.terminal(canal, r) {
					continue
				}
				if r.Type != "exec" || len(r.Payload) < 4 {
					r.Reply(false, nil)
					continue
				}
				cmd := string(r.Payload[4:])
				r.Reply(true, nil)
				s.commandes <- cmd
				var entree bytes.Buffer
				entree.ReadFrom(canal) // the whole input first (EOF at once without one): closing earlier breaks the client's copy
				sortie, code := "", 0
				if repondre != nil {
					sortie, code = repondre(cmd, entree.Bytes())
				}
				canal.Write([]byte(sortie))
				canal.SendRequest("exit-status", false, binary.BigEndian.AppendUint32(nil, uint32(code)))
				return
			}
		}()
	}
}

func TestConnexionSSH(t *testing.T) {
	signer, pemCle := nouvelleCleSSH(t)
	_, autreCle := nouvelleCleSSH(t)
	srv := lancerSSHMemoire(t, signer.PublicKey(), "mdp-essai", func(cmd string, entree []byte) (string, int) {
		switch {
		case cmd == "gros":
			return strings.Repeat("x", sortieMaxSSH+500), 0
		case cmd == "dort":
			time.Sleep(2 * time.Second)
			return "", 0
		case strings.HasPrefix(cmd, "cat"):
			return string(entree), 0
		case cmd == "echoue":
			return "non", 3
		}
		return "ok:" + cmd, 0
	})
	ctx := context.Background()
	acces := AccesSSH{Hote: srv.hote, Port: srv.port, Utilisateur: "athanor", Cle: pemCle, Empreinte: srv.empreinte}

	vue, typ, err := empreinteHoteSSH(ctx, srv.hote, srv.port)
	if nom, cmd := cleHote(typ); nom == "" || !strings.HasSuffix(cmd, "_key.pub") {
		t.Errorf("type %q: %q, %q", typ, nom, cmd)
	}
	if err != nil || vue != srv.empreinte {
		t.Fatalf("empreinte présentée %q (%v), attendue %q", vue, err, srv.empreinte)
	}

	c, err := connecterSSH(ctx, acces)
	if err != nil {
		t.Fatalf("bonne clé refusée : %v", err)
	}
	defer c.Close()
	if r, err := executerSSH(ctx, c, "uname", nil, 5*time.Second); err != nil || string(r.Sortie) != "ok:uname" || r.Code != 0 || r.Coupee {
		t.Fatalf("commande : %+v %v", r, err)
	}
	if r, err := executerSSH(ctx, c, "echoue", nil, 5*time.Second); err != nil || r.Code != 3 {
		t.Fatalf("code de sortie : %+v %v", r, err)
	}
	if r, err := executerSSH(ctx, c, "cat > /tmp/f", strings.NewReader("contenu"), 5*time.Second); err != nil || string(r.Sortie) != "contenu" {
		t.Fatalf("entrée : %+v %v", r, err)
	}
	if r, err := executerSSH(ctx, c, "gros", nil, 5*time.Second); err != nil || len(r.Sortie) != sortieMaxSSH || !r.Coupee {
		t.Fatalf("sortie non coupée à 1 Mo : %d octets, coupée=%v, %v", len(r.Sortie), r.Coupee, err)
	}
	if _, err := executerSSH(ctx, c, "dort", nil, 200*time.Millisecond); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("délai de commande non tenu : %v", err)
	}

	// mot de passe (ajout) : accepté, et un mauvais refusé
	parMdp := acces
	parMdp.Cle, parMdp.MotDePasse = nil, "mdp-essai"
	if c2, err := connecterSSH(ctx, parMdp); err != nil {
		t.Fatalf("bon mot de passe refusé : %v", err)
	} else {
		c2.Close()
	}
	parMdp.MotDePasse = "faux"
	if _, err := connecterSSH(ctx, parMdp); err == nil {
		t.Fatal("mauvais mot de passe accepté")
	}

	mauvaise := acces
	mauvaise.Cle = autreCle
	if _, err := connecterSSH(ctx, mauvaise); err == nil {
		t.Fatal("mauvaise clé acceptée")
	}

	changee := acces
	changee.Empreinte = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if _, err := connecterSSH(ctx, changee); err != errEmpreinteChangee {
		t.Fatalf("empreinte changée : %v", err)
	}
	sans := acces
	sans.Empreinte = ""
	if _, err := connecterSSH(ctx, sans); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("connexion sans empreinte confirmée : %v", err)
	}
}

// Un serveur qui accepte le TCP mais ne parle jamais : la connexion abandonne au bout du délai.
func TestConnexionSSHDelai(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	ancien := delaiSSH
	delaiSSH = 300 * time.Millisecond
	defer func() { delaiSSH = ancien }()
	debut := time.Now()
	_, err = connecterSSH(context.Background(), AccesSSH{Hote: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, Utilisateur: "x", MotDePasse: "y", Empreinte: "SHA256:x"})
	if err == nil || !strings.Contains(err.Error(), "no answer") || time.Since(debut) > 3*time.Second {
		t.Fatalf("délai non tenu : %v après %s", err, time.Since(debut))
	}
	if ancien != 10*time.Second {
		t.Fatalf("délai de connexion %s, attendu 10 s", ancien)
	}
}

// Serveur de test en conteneur (.athanor/tools/serveur_ssh.sh start) : ATHANOR_SSH_TEST=hote:port.
func TestConnexionSSHConteneur(t *testing.T) {
	cible := os.Getenv("ATHANOR_SSH_TEST")
	if cible == "" {
		t.Skip("ATHANOR_SSH_TEST absent (bash .athanor/tools/serveur_ssh.sh start)")
	}
	hote, portTxt, err := net.SplitHostPort(cible)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portTxt)
	ctx := context.Background()
	empreinte, typ, err := empreinteHoteSSH(ctx, hote, port)
	// M240l5 : juste après un autre test sur le même sshd, il ferme encore des connexions non authentifiées
	// (MaxStartups) : « handshake failed: EOF » ; deux nouveaux essais espacés
	for i := 0; err != nil && i < 2; i++ {
		time.Sleep(2 * time.Second)
		empreinte, typ, err = empreinteHoteSSH(ctx, hote, port)
	}
	if err != nil {
		t.Fatalf("empreinte du conteneur : %v", err)
	}
	c, err := connecterSSH(ctx, AccesSSH{Hote: hote, Port: port, Utilisateur: "athanor", MotDePasse: motDePasseConteneurSSH, Empreinte: empreinte})
	if err != nil {
		t.Fatalf("connexion au conteneur : %v", err)
	}
	defer c.Close()
	// la commande que la page donne affiche l'empreinte montrée, avec le même type (comme ssh-keygen -l)
	nom, commande := cleHote(typ)
	if r, err := executerSSH(ctx, c, commande, nil, 10*time.Second); err != nil || !strings.Contains(string(r.Sortie), empreinte) || !strings.Contains(string(r.Sortie), "("+nom+")") {
		t.Fatalf("%s : %q %v, attendu %s (%s)", commande, r.Sortie, err, empreinte, nom)
	}
	r, err := executerSSH(ctx, c, "id -un && sudo -n true && echo sudo-ok", nil, 10*time.Second)
	if err != nil || r.Code != 0 || string(r.Sortie) != "athanor\nsudo-ok\n" {
		t.Fatalf("commande dans le conteneur : %q code %d %v", r.Sortie, r.Code, err)
	}
}

// TestCaddyConteneur (M130) : le conteneur de test a git et Caddy démarré sans systemd ; le port HTTP publié
// (ATHANOR_SSH_HTTP) rend la page par défaut, avant et après un `caddy reload` lancé par SSH comme le fera le déploiement.
func TestCaddyConteneur(t *testing.T) {
	cible, web := os.Getenv("ATHANOR_SSH_TEST"), os.Getenv("ATHANOR_SSH_HTTP")
	if cible == "" || web == "" {
		t.Skip("ATHANOR_SSH_TEST ou ATHANOR_SSH_HTTP absent (bash .athanor/tools/serveur_ssh.sh start, puis http)")
	}
	pageParDefaut := func(quand string) {
		t.Helper()
		r, err := http.Get("http://" + web + "/")
		if err != nil {
			t.Fatalf("%s : %v", quand, err)
		}
		defer r.Body.Close()
		corps, _ := io.ReadAll(r.Body)
		if r.StatusCode != 200 || !strings.Contains(string(corps), "Caddy works!") {
			t.Fatalf("%s : statut %d, page %.200q", quand, r.StatusCode, corps)
		}
	}
	pageParDefaut("avant le rechargement")
	hote, portTxt, err := net.SplitHostPort(cible)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portTxt)
	ctx := context.Background()
	empreinte, typ, err := empreinteHoteSSH(ctx, hote, port)
	// M240l5 : juste après un autre test sur le même sshd, il ferme encore des connexions non authentifiées
	// (MaxStartups) : « handshake failed: EOF » ; deux nouveaux essais espacés
	for i := 0; err != nil && i < 2; i++ {
		time.Sleep(2 * time.Second)
		empreinte, typ, err = empreinteHoteSSH(ctx, hote, port)
	}
	if err != nil {
		t.Fatalf("empreinte du conteneur : %v", err)
	}
	c, err := connecterSSH(ctx, AccesSSH{Hote: hote, Port: port, Utilisateur: "athanor", MotDePasse: motDePasseConteneurSSH, Empreinte: empreinte})
	if err != nil {
		t.Fatalf("connexion au conteneur : %v", err)
	}
	defer c.Close()
	// la commande que la page donne affiche l'empreinte montrée, avec le même type (comme ssh-keygen -l)
	nom, commande := cleHote(typ)
	if r, err := executerSSH(ctx, c, commande, nil, 10*time.Second); err != nil || !strings.Contains(string(r.Sortie), empreinte) || !strings.Contains(string(r.Sortie), "("+nom+")") {
		t.Fatalf("%s : %q %v, attendu %s (%s)", commande, r.Sortie, err, empreinte, nom)
	}
	r, err := executerSSH(ctx, c, "git --version >/dev/null && sudo -n caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile 2>&1 && ps -o user= -C caddy", nil, 30*time.Second)
	if err != nil || r.Code != 0 || !strings.HasSuffix("\n"+strings.TrimSpace(string(r.Sortie)), "\ncaddy") {
		t.Fatalf("git, caddy reload et utilisateur de Caddy : %q code %d %v", r.Sortie, r.Code, err)
	}
	pageParDefaut("après le rechargement")
}

// TestSiteExemple (M130) : les deux versions du site d'exemple que les déploiements de test servent, distinctes.
func TestSiteExemple(t *testing.T) {
	for _, v := range []string{"v1", "v2"} {
		b, err := os.ReadFile("testdata/site-exemple/" + v + "/index.html")
		if err != nil || !strings.Contains(string(b), "site-exemple "+v) {
			t.Fatalf("site d'exemple %s : %v %.100q", v, err, b)
		}
	}
}
