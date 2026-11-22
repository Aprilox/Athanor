package main

// RELAY_CONTEXT_K=auto: the context past which a fresh session takes the milestone over, worked out for the
// project from its own cycles. High enough for most of its milestones to fit in one session (each relay costs a
// fresh session that finds its way again), and no higher: every turn rereads the whole context, and a session
// that grows far beyond what the project's milestones need is more often one that has lost its way.

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	seuilRelaisDefautK = 200 // until the project has finished enough milestones to tell
	seuilRelaisMinK    = 110
	seuilRelaisMaxK    = 300
	contexteDepartK    = 30 // what a session holds before its first step (the instructions, the tools)
)

// seuilRelaisAuto: the threshold, in thousands of tokens, that three in four of the project's last 30 finished
// milestones fit under, plus 15 % (each milestone's need: the context of the cycle that finished it, plus the
// work of the sessions that relayed it before), between 110 and 300; 200 until 8 milestones are known.
func seuilRelaisAuto(stats string) int {
	b, err := lireBorne(stats, 32<<20)
	if err != nil {
		return seuilRelaisDefautK
	}
	var besoins []int
	avant := 0 // the work of the relayed sessions of the milestone being finished
	for i, l := range strings.Split(string(b), "\n") {
		c := strings.Split(strings.TrimRight(l, "\r"), "\t")
		if i == 0 || len(c) < 13 || strings.Contains(c[1], "·") { // a parallel worker's line apart
			continue
		}
		ctx, err := strconv.Atoi(strings.TrimSpace(c[12]))
		if err != nil || ctx <= 0 || len(c) > 15 && strings.Contains(c[15], "cadrage") {
			continue
		}
		switch c[3] {
		case "relais":
			avant += max(ctx-contexteDepartK*1000, 0)
		case "ok":
			besoins = append(besoins, avant+ctx)
			avant = 0
		}
	}
	if len(besoins) < 8 {
		return seuilRelaisDefautK
	}
	if len(besoins) > 30 {
		besoins = besoins[len(besoins)-30:]
	}
	sort.Ints(besoins)
	k := besoins[len(besoins)*3/4] * 115 / 100 / 1000
	k = (k + 9) / 10 * 10 // up to the next ten
	return min(max(k, seuilRelaisMinK), seuilRelaisMaxK)
}

// consigneTailleJalons: the framing's rule for the size of a milestone, from the context a session may reach.
func consigneTailleJalons(seuilK int) string {
	if seuilK <= contexteDepartK {
		return ""
	}
	return fmt.Sprintf("\n\n**Size of a milestone.** Each milestone must fit in one session: about %dk tokens of work (what Claude reads, writes and runs, its checks included) once the session has started. Past that, the pilot hands the milestone to a fresh session, which loses time finding its way again. Split a milestone that would not fit (several screens, several levels, a whole subsystem) into smaller ones; never drop any part of the request.\n", seuilK-contexteDepartK)
}

// consigneDecoupageRelais: after a relay, what is left of the milestone is split when it will not fit in this session.
func consigneDecoupageRelais(seuilK int) string {
	if seuilK <= contexteDepartK {
		return ""
	}
	return fmt.Sprintf("### This milestone did not fit in one session\n\nIf what is left of it will not be finished in this session (about %dk tokens of work), first split it in `.athanor/PLAN.md`: replace its line by sub-milestones (`M7a`, `M7b`…) that each fit in one session, each with its own `Files:` and `Acceptance:`, the part already done ticked, nothing of it dropped. Then carry on with the first one left. If it is nearly done, just finish it.\n", seuilK-contexteDepartK)
}
