package main

import "testing"

// A resumed chat session reports the total of the whole session: each turn costs the difference (the
// user's case: 5.95 $, then 6.44 $, 6.86 $… were counted in full, about 42 $ too many).
func TestCoutTourChat(t *testing.T) {
	// messages written before 2.49.4: their cost is the session's total
	avant := []MessageChat{{Role: "vous"}, {Role: "claude", Cout: 5.9516}, {Role: "vous"}}
	if c := coutTourChat(avant, true, 6.4383); c != 0.4867 {
		t.Fatalf("after an old message: %v", c)
	}
	avant = append(avant, MessageChat{Role: "claude", Cout: 0.4867, CoutSession: 6.4383}, MessageChat{Role: "vous"})
	if c := coutTourChat(avant, true, 6.8632); c != 0.4249 {
		t.Fatalf("after a new one: %v", c)
	}
	if c := coutTourChat(avant, false, 1.2); c != 1.2 {
		t.Fatalf("a new conversation: %v", c)
	}
	if c := coutTourChat(avant, true, 0.3); c != 0.3 {
		t.Fatalf("a session that counted again: %v", c)
	}
	// an error with no cost in between: the last total known before it
	avant = append(avant, MessageChat{Role: "erreur"}, MessageChat{Role: "vous"})
	if c := coutTourChat(avant, true, 6.9309); c != 0.4926 { // 6.9309 - 6.4383
		t.Fatalf("after an error: %v", c)
	}
	// the user's six turns in API mode: 2.41 $, not 44.49 $
	somme, prec := 0.0, []MessageChat{{Role: "claude", Cout: 5.9516}}
	for _, tot := range []float64{6.4383, 6.8632, 6.9309, 7.6149, 8.2872, 8.3595} {
		c := coutTourChat(prec, true, tot)
		somme += c
		prec = append(prec, MessageChat{Role: "vous"}, MessageChat{Role: "claude", Cout: c, CoutSession: tot})
	}
	if somme < 2.40 || somme > 2.42 {
		t.Fatalf("the six turns: %v", somme)
	}
}
