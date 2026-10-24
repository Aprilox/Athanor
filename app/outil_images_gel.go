package main

// athanor images gel|passee JOURNAL: what the images of a Claude session did. A session that receives a
// large image may never answer again (seen with Claude Code 2.1.306 on native Windows: every image of
// more than about 170 KB in the session log, after a night of them going through); the pilots name such a
// silence, stop it sooner, and tell the next sessions to keep their images small.
//   gel JOURNAL     the session's last line is a tool result holding an image: prints its size in KB (exit 0),
//                   else nothing (exit 1)
//   passee JOURNAL  the size in KB of the largest image line some other line followed (0: none)
//   consigne KB     the instructions that come with the cycles meanwhile

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
)

// ligneImage: a session log line that hands Claude an image (a tool result holding one).
func ligneImage(l []byte) bool {
	l = bytes.TrimSpace(l)
	if len(l) == 0 || l[0] != '{' || !bytes.Contains(l, []byte(`"image"`)) {
		return false
	}
	var m struct {
		Type    string `json:"type"`
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(l, &m) != nil || m.Type != "user" {
		return false
	}
	var contenu []struct {
		Type    string          `json:"type"`
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(m.Message.Content, &contenu) != nil {
		return false
	}
	for _, c := range contenu {
		if c.Type != "tool_result" && c.Type != "image" {
			continue
		}
		if c.Type == "image" {
			return true
		}
		var parts []struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(c.Content, &parts) == nil {
			for _, x := range parts {
				if x.Type == "image" {
					return true
				}
			}
		}
	}
	return false
}

// derniereLigneJournal: the last non-empty line of a log (read from its end: logs can be large).
func derniereLigneJournal(f string) []byte {
	o, err := os.Open(f)
	if err != nil {
		return nil
	}
	defer o.Close()
	st, err := o.Stat()
	if err != nil || st.Size() == 0 {
		return nil
	}
	const max = 8 << 20 // an image line holds a few MB at most
	debut := st.Size() - max
	if debut < 0 {
		debut = 0
	}
	b := make([]byte, st.Size()-debut)
	if _, err := o.ReadAt(b, debut); err != nil && err != io.EOF {
		return nil
	}
	b = bytes.TrimRight(b, "\r\n \t")
	if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
		return b[i+1:]
	}
	if debut > 0 {
		return nil // a line longer than what was read: not an image the pilots know
	}
	return b
}

// imageGelee: the session went silent right after an image (its size in KB), or not.
func imageGelee(journal string) (int, bool) {
	l := derniereLigneJournal(journal)
	if !ligneImage(l) {
		return 0, false
	}
	return (len(l) + 1023) / 1024, true
}

func imageGeleeOK(journal string) bool { _, ok := imageGelee(journal); return ok }

// plusGrandeImagePassee: the largest image line (KB) some other line followed: images of that size went through.
func plusGrandeImagePassee(journal string) int {
	o, err := os.Open(journal)
	if err != nil {
		return 0
	}
	defer o.Close()
	r := bufio.NewReaderSize(o, 1<<20)
	max, attente := 0, 0
	for {
		l, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(l)) > 0 {
			if attente > max {
				max = attente // the image line before this one was answered
			}
			attente = 0
			if ligneImage(l) {
				attente = (len(bytes.TrimRight(l, "\r\n")) + 1023) / 1024
			}
		}
		if err != nil {
			break
		}
	}
	return max
}

func lancerImages(args []string) int {
	if len(args) == 2 && args[0] != "consigne" {
		args = cheminsOutil(args)
	}
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor images gel|passee JOURNAL | consigne KB")
		return 2
	}
	switch args[0] {
	case "gel":
		if kb, ok := imageGelee(args[1]); ok {
			fmt.Println(kb)
			return 0
		}
		return 1
	case "passee":
		fmt.Println(plusGrandeImagePassee(args[1]))
		return 0
	case "consigne":
		kb, _ := strconv.Atoi(args[1])
		fmt.Print(consigneImages(kb))
		return 0
	}
	fmt.Fprintln(os.Stderr, "usage: athanor images gel|passee JOURNAL | consigne KB")
	return 2
}

// consigneImages: the instructions that come with the cycles while large images freeze the sessions.
func consigneImages(kb int) string {
	return fmt.Sprintf("\n\n## Images: keep them small\n\nA previous session froze right after it received a large image (about %d KB in its log) and stayed silent until the pilot stopped it; smaller images went through. Until large images work again:\n\n"+
		"- Before you read an image file, make a small copy of it (at most about 800 pixels wide, under about 60 KB: PNG with few colours, or JPEG at a low quality) with a command-line tool, and read that copy.\n"+
		"- A tool that hands you a screenshot directly (an MCP capture) sends it at full size: when it can save the capture to a file instead, do that, shrink the file, then read the copy; otherwise use it as little as possible and check what you can through code, logs or properties.\n", kb)
}
