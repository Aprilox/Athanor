package main

// athanor affichage JOURNAL FIN: the live display of a Claude session that writes its stream into its log
// file (pilot.sh in native Windows: no pipe between Claude and the pilot, whose Git Bash pipes stall on
// the long lines of an image): the log read as it grows, each line shown as afficher_en_direct shows it,
// until the FIN file exists and the end of the log is reached.

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"time"
)

func lancerAffichage(args []string) int {
	args = cheminsOutil(args)
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: athanor affichage JOURNAL FIN")
		return 2
	}
	suivreJournal(args[0], args[1], os.Stdout, 250*time.Millisecond, 6*time.Hour)
	return 0
}

// suivreJournal: shows the lines of journal as they come, until fin exists and nothing more comes (or
// nothing at all came for maxSilence: a pilot gone without saying so).
func suivreJournal(journal, fin string, sortie io.Writer, pas, maxSilence time.Duration) {
	var f *os.File
	for f == nil {
		o, err := os.Open(journal)
		if err == nil {
			f = o
			break
		}
		if fichierExiste(fin) {
			return
		}
		time.Sleep(pas)
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var reste []byte
	dernier := time.Now()
	finVue := time.Time{}
	for {
		morceau, err := r.ReadBytes('\n')
		if len(morceau) > 0 {
			dernier = time.Now()
			reste = append(reste, morceau...)
			if bytes.HasSuffix(reste, []byte("\n")) {
				for _, l := range lignesEnDirectPilote(string(reste)) {
					fmt.Fprintln(sortie, l)
				}
				reste = reste[:0]
			}
		}
		if err == nil {
			continue
		}
		// end of what is written for now
		if fichierExiste(fin) {
			if finVue.IsZero() {
				finVue = time.Now()
			} else if time.Since(finVue) > 2*pas {
				// the very last line may come without its line end
				if len(reste) > 0 {
					for _, l := range lignesEnDirectPilote(string(reste)) {
						fmt.Fprintln(sortie, l)
					}
				}
				return
			}
		} else if time.Since(dernier) > maxSilence {
			return
		}
		time.Sleep(pas)
	}
}

func fichierExiste(f string) bool { _, err := os.Stat(f); return err == nil }
