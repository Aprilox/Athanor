package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// the live display of a log written while it is read: every line shown once, in order, the last one even
// without its line end, and the reading stops once FIN exists
func TestSuivreJournal(t *testing.T) {
	d := t.TempDir()
	j, fin := filepath.Join(d, "j.jsonl"), filepath.Join(d, "fin")
	var sortie bytes.Buffer
	fait := make(chan struct{})
	go func() { suivreJournal(j, fin, &sortie, 20*time.Millisecond, time.Minute); close(fait) }()
	time.Sleep(60 * time.Millisecond)
	f, _ := os.Create(j)
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"text","text":"one"}]}}` + "\n")
	time.Sleep(60 * time.Millisecond)
	// a long line (an image) written in two parts
	img := `{"type":"user","message":{"content":[{"type":"tool_result","content":[{"type":"image","source":{"data":"` + strings.Repeat("A", 400000)
	f.WriteString(img)
	time.Sleep(60 * time.Millisecond)
	f.WriteString(`"}}]}]}}` + "\n")
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"ls  -la"}}]}}` + "\n")
	f.WriteString(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__s__t","input":{"studio_id":"a","code":"x"}}]}}` + "\n")
	f.WriteString(`{"type":"result","result":"done"}`)
	f.Close()
	time.Sleep(60 * time.Millisecond)
	os.WriteFile(fin, nil, 0o644)
	select {
	case <-fait:
	case <-time.After(5 * time.Second):
		t.Fatal("the display did not stop after FIN")
	}
	attendu := "  💬 one\n  ▶ ls -la\n  • mcp__s__t {\"studio_id\":\"a\",\"code\":\"x\"}\n  ■ End of cycle: done\n"
	if sortie.String() != attendu {
		t.Fatalf("display:\n%q\nwant\n%q", sortie.String(), attendu)
	}
}

func TestImagesGel(t *testing.T) {
	d := t.TempDir()
	img := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"t","type":"tool_result","content":[{"type":"image","source":{"type":"base64","data":"` + strings.Repeat("A", 200000) + `"}}]}]}}`
	petite := `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"t","type":"tool_result","content":[{"type":"image","source":{"type":"base64","data":"` + strings.Repeat("A", 50000) + `"}}]}]}}`
	texte := `{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}`
	gel, ok := filepath.Join(d, "gel"), filepath.Join(d, "ok")
	os.WriteFile(gel, []byte(texte+"\n"+petite+"\n"+texte+"\n"+img+"\n"), 0o644)
	os.WriteFile(ok, []byte(img+"\n"+texte+"\n"), 0o644)
	if kb, g := imageGelee(gel); !g || kb != 196 {
		t.Fatalf("gel: %d %v", kb, g)
	}
	if n := plusGrandeImagePassee(gel); n != 49 {
		t.Fatalf("passee gel: %d", n)
	}
	if _, g := imageGelee(ok); g {
		t.Fatal("ok seen as frozen")
	}
	if n := plusGrandeImagePassee(ok); n != 196 {
		t.Fatalf("passee ok: %d", n)
	}
	if !strings.Contains(consigneImages(196), "about 196 KB") {
		t.Fatal("rule")
	}
}
