package main

// athanor mcp-client (ARCHITECTURE.md §Ouverture, stage 11): the stdio relay an external MCP client
// (Claude Desktop, Claude Code) starts. "mcp-client appairer NAME" pairs it in the terminal (the 6 digits
// shown by Settings › MCP clients, then the owner's consent on screen) and keeps its token in
// UserConfigDir()/athanor-clients/NAME.jeton (0600); "mcp-client NAME" relays stdin/stdout to /api/mcp with
// that token. Athanor closed or the token revoked: a clear tool error, never a wait without end.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var textesClient = textesRelais{"athanor",
	"Athanor is not open on this computer: its tools are unavailable until you open it.",
	"This MCP client is not paired with Athanor: run «athanor mcp-client appairer NAME» in a terminal first.",
	"Athanor refused this client's token (revoked, or Athanor is in emergency stop). Pair it again: «athanor mcp-client appairer NAME»."}

// nomClientValide: the name is also a file name.
var nomClientValide = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,59}$`)

func fichierJetonClient(nom string) string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "athanor-clients", nom+".jeton")
}

// pauseStatutClient: how often the terminal asks whether the owner answered (a variable for the tests).
var pauseStatutClient = 2 * time.Second

func init() {
	enregistrerCommande("mcp-client", "mcp-client NAME | mcp-client appairer NAME [--sorte desktop|code]: Athanor's MCP for Claude Desktop or Claude Code (stdio relayed to the running Athanor)", func(args []string) int {
		usage := func() int {
			fmt.Fprintln(os.Stderr, "usage: athanor mcp-client appairer NAME [--sorte desktop|code]\n       athanor mcp-client NAME")
			return 2
		}
		if len(args) >= 2 && args[0] == "appairer" {
			sorte := "code"
			if len(args) == 4 && args[2] == "--sorte" {
				sorte = args[3]
			} else if len(args) != 2 {
				return usage()
			}
			if err := appairerClientTerminal(os.Stdin, os.Stderr, args[1], sorte, fichierInstance()); err != nil {
				fmt.Fprintln(os.Stderr, "✖", err)
				return 1
			}
			return 0
		}
		if len(args) != 1 || !nomClientValide.MatchString(args[0]) {
			return usage()
		}
		relaisMCP(os.Stdin, os.Stdout, fichierJetonClient(args[0]), fichierInstance(), 60*time.Second, textesClient)
		return 0
	})
}

// portInstance: the running Athanor's port (0 when closed).
func portInstance(fichierInst string) int {
	var i infoInstance
	if b, err := os.ReadFile(fichierInst); err != nil || json.Unmarshal(b, &i) != nil {
		return 0
	}
	return i.Port
}

// appairerClientTerminal: asks the code on the terminal, waits for the owner's answer, writes the token.
func appairerClientTerminal(in io.Reader, out io.Writer, nom, sorte, fichierInst string) error {
	if !nomClientValide.MatchString(nom) {
		return errors.New("the name: letters, digits, dot, dash or underscore (60 at most)")
	}
	if !slices.Contains([]string{"desktop", "code"}, sorte) {
		return errors.New("--sorte: desktop or code")
	}
	port := portInstance(fichierInst)
	if port <= 0 {
		return errors.New("Athanor is not open on this computer: open it, then Settings › MCP clients › Connect")
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port) + "/api/mcp/appairer"
	client := &http.Client{Timeout: 15 * time.Second}
	poster := func(url string, corps any) (int, map[string]any, error) {
		b, _ := json.Marshal(corps)
		r, err := client.Post(url, "application/json", bytes.NewReader(b))
		if err != nil {
			return 0, nil, errors.New("Athanor does not answer: is it still open?")
		}
		defer r.Body.Close()
		var v map[string]any
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&v)
		return r.StatusCode, v, nil
	}
	texte := func(v map[string]any) string { s, _ := v["erreur"].(string); return s }
	fmt.Fprint(out, "In Athanor: Settings › MCP clients › Connect. Type the 6-digit code shown there: ")
	ligne, _ := bufio.NewReader(in).ReadString('\n')
	code, v, err := poster(base, map[string]string{"code": strings.TrimSpace(ligne), "nom": nom, "sorte": sorte})
	if err != nil {
		return err
	}
	demande, _ := v["demande"].(string)
	if code != http.StatusAccepted || demande == "" {
		return errors.New(texte(v))
	}
	fmt.Fprintln(out, "Code accepted. Accept «"+nom+"» in Athanor's window (and choose what it may see)…")
	for fin := time.Now().Add(dureeCodeDirect + time.Minute); time.Now().Before(fin); time.Sleep(pauseStatutClient) {
		code, v, err := poster(base+"/statut", map[string]string{"demande": demande})
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return errors.New(texte(v))
		}
		jeton, _ := v["jeton"].(string)
		if jeton == "" {
			continue
		}
		f := fichierJetonClient(nom)
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return err
		}
		if err := ecrireAtomique(f, []byte(jeton+"\n"), 0o600); err != nil {
			return err
		}
		exe, _ := os.Executable()
		fmt.Fprintf(out, "✔ Paired. Token kept in %s.\nClaude Code: claude mcp add athanor -- \"%s\" mcp-client %s\n", f, exe, nom)
		return nil
	}
	return errors.New("no answer in 10 minutes: open a new code in Athanor")
}
