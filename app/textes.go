package main

// Labels written into the project files (REQUEST.md), in the chosen language.
// The headings are landmarks for the pilot: every variant is recognised.

// Palettes and accents of the interface (defined in static/app.css).
var themesConnus = map[string]bool{"": true, "ardoise": true, "minuit": true, "ocean": true, "clair": true, "sable": true}
var accentsConnus = map[string]bool{"": true, "sarcelle": true, "bleu": true, "violet": true, "ambre": true, "rose": true}

var languesConnues = map[string]bool{"en": true, "fr": true, "es": true, "de": true, "it": true, "pt": true}

var texteFichiers = map[string]map[string]string{
	"en": {"sans_session": "Open Athanor from its shortcut (Athanor.exe, or athanor on Linux): this page only works in the window Athanor opens.", "mission_retiree": "A client never launches a mission: the owner does, in Athanor's window. Use ajouter_demande to add a request to a project: its pilot builds it.", "natif_titre": "Athanor: pilots still running", "natif_texte": "Pilots are running in native Windows mode: %s.\n\nYes: they go on in the background (reopen Athanor to follow them or stop them).\nNo: they stop now (the cycle under way is interrupted).", "demande_le": "Requested on", "remarques": "## Notes along the way", "remarques_aide": "(You can write here while Claude works: it re-reads this file at the start of each cycle.)", "demande_suivante": "## Next request"},
	"fr": {"sans_session": "Ouvrez Athanor depuis son raccourci (Athanor.exe, ou athanor sous Linux) : cette page ne fonctionne que dans la fenêtre qu'Athanor ouvre.", "mission_retiree": "Un client ne lance jamais de mission : le propriétaire le fait, dans la fenêtre d'Athanor. Utilise ajouter_demande pour ajouter une demande à un projet : son pilote la réalise.", "natif_titre": "Athanor : des pilotes tournent encore", "natif_texte": "Des pilotes tournent en mode Windows natif : %s.\n\nOui : ils continuent en arrière-plan (rouvrez Athanor pour les suivre ou les arrêter).\nNon : ils s'arrêtent maintenant (le cycle en cours est interrompu).", "demande_le": "Demandé le", "remarques": "## Remarques en cours de route", "remarques_aide": "(Vous pouvez écrire ici pendant que Claude travaille : il relit ce fichier au début de chaque cycle.)", "demande_suivante": "## Demande suivante"},
	"es": {"sans_session": "Abra Athanor desde su acceso directo (Athanor.exe, o athanor en Linux): esta página solo funciona en la ventana que abre Athanor.", "mission_retiree": "Un cliente nunca lanza una misión: lo hace el propietario, en la ventana de Athanor. Usa ajouter_demande para añadir una solicitud a un proyecto: su piloto la realiza.", "natif_titre": "Athanor: hay pilotos en marcha", "natif_texte": "Hay pilotos en marcha en modo Windows nativo: %s.\n\nSí: siguen en segundo plano (vuelva a abrir Athanor para seguirlos o detenerlos).\nNo: se detienen ahora (el ciclo en curso se interrumpe).", "demande_le": "Solicitado el", "remarques": "## Notas sobre la marcha", "remarques_aide": "(Puede escribir aquí mientras Claude trabaja: relee este archivo al inicio de cada ciclo.)", "demande_suivante": "## Siguiente solicitud"},
	"de": {"sans_session": "Öffnen Sie Athanor über seine Verknüpfung (Athanor.exe, oder athanor unter Linux): Diese Seite funktioniert nur in dem Fenster, das Athanor öffnet.", "mission_retiree": "Ein Client startet nie eine Mission: Das tut der Eigentümer im Fenster von Athanor. Nutze ajouter_demande, um einem Projekt eine Anfrage hinzuzufügen: sein Pilot setzt sie um.", "natif_titre": "Athanor: Piloten laufen noch", "natif_texte": "Im nativen Windows-Modus laufen Piloten: %s.\n\nJa: sie laufen im Hintergrund weiter (öffnen Sie Athanor erneut, um sie zu verfolgen oder zu stoppen).\nNein: sie stoppen jetzt (der laufende Zyklus wird unterbrochen).", "demande_le": "Angefragt am", "remarques": "## Hinweise unterwegs", "remarques_aide": "(Hier können Sie schreiben, während Claude arbeitet: die Datei wird zu Beginn jedes Zyklus neu gelesen.)", "demande_suivante": "## Nächste Anfrage"},
	"it": {"sans_session": "Apra Athanor dal suo collegamento (Athanor.exe, o athanor su Linux): questa pagina funziona solo nella finestra che Athanor apre.", "mission_retiree": "Un client non avvia mai una missione: lo fa il proprietario, nella finestra di Athanor. Usa ajouter_demande per aggiungere una richiesta a un progetto: il suo pilota la realizza.", "natif_titre": "Athanor: ci sono piloti in funzione", "natif_texte": "Ci sono piloti in funzione in modalità Windows nativa: %s.\n\nSì: continuano in background (riapri Athanor per seguirli o fermarli).\nNo: si fermano ora (il ciclo in corso viene interrotto).", "demande_le": "Richiesto il", "remarques": "## Note lungo il percorso", "remarques_aide": "(Puoi scrivere qui mentre Claude lavora: rilegge questo file all'inizio di ogni ciclo.)", "demande_suivante": "## Richiesta successiva"},
	"pt": {"sans_session": "Abra o Athanor a partir do seu atalho (Athanor.exe, ou athanor no Linux): esta página só funciona na janela que o Athanor abre.", "mission_retiree": "Um cliente nunca lança uma missão: fá-lo o proprietário, na janela do Athanor. Usa ajouter_demande para adicionar um pedido a um projeto: o seu piloto realiza-o.", "natif_titre": "Athanor: há pilotos em execução", "natif_texte": "Há pilotos em execução no modo Windows nativo: %s.\n\nSim: continuam em segundo plano (reabra o Athanor para os acompanhar ou parar).\nNão: param agora (o ciclo em curso é interrompido).", "demande_le": "Pedido em", "remarques": "## Notas pelo caminho", "remarques_aide": "(Pode escrever aqui enquanto o Claude trabalha: ele relê este ficheiro no início de cada ciclo.)", "demande_suivante": "## Pedido seguinte"},
}

func libelle(langue, cle string) string {
	if m, ok := texteFichiers[langue]; ok {
		if v, ok := m[cle]; ok {
			return v
		}
	}
	return texteFichiers["en"][cle]
}

// marqueursRemarques: every variant of the "notes" heading, to find the section whatever the language.
func marqueursRemarques() []string {
	var v []string
	for _, m := range texteFichiers {
		v = append(v, m["remarques"])
	}
	return v
}
