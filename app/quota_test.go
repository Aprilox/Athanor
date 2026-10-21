package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestQuotaDuJournal(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "cycle-x.jsonl")
	_ = os.WriteFile(f, []byte(`{"type":"system"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","unifiedWindows":{"seven_day":{"utilization":0.5,"resetsAt":20},"five_hour":{"utilization":0.02,"resetsAt":10}}}}
{"type":"assistant"}
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed_warning","unifiedWindows":{"seven_day_fable":{"utilization":0.9,"resetsAt":30},"seven_day":{"utilization":0.66,"resetsAt":20},"five_hour":{"utilization":0.04,"resetsAt":10}}}}
{"type":"result"}
`), 0o644)
	q := quotaDuJournal(f)
	if q == nil || q.Statut != "allowed_warning" || len(q.Fenetres) != 3 {
		t.Fatalf("%+v", q)
	}
	if q.Fenetres[0].Nom != "five_hour" || q.Fenetres[1].Nom != "seven_day" || q.Fenetres[2].Nom != "seven_day_fable" || *q.Fenetres[1].Utilisation != 0.66 || q.Fenetres[2].Reinit != 30 {
		t.Fatalf("order or values: %+v", q.Fenetres)
	}
	_ = os.WriteFile(f, []byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":99,"rateLimitType":"five_hour"}}`+"\n"), 0o644)
	if q := quotaDuJournal(f); q == nil || q.Fenetres[0].Nom != "five_hour" || q.Fenetres[0].Reinit != 99 || q.Fenetres[0].Utilisation != nil {
		t.Fatalf("without windows: %+v", q)
	}
	_ = os.WriteFile(f, []byte("{\"type\":\"assistant\"}\n"), 0o644)
	if quotaDuJournal(f) != nil {
		t.Fatal("no event")
	}
}
