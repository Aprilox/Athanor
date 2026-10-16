package main

// Labels written into the project files (REQUEST.md), in the chosen language.
// The headings are landmarks for the pilot: every variant is recognised.

// Palettes and accents of the interface (defined in static/app.css).
var themesConnus = map[string]bool{"": true, "ardoise": true, "minuit": true, "ocean": true, "clair": true, "sable": true}
var accentsConnus = map[string]bool{"": true, "sarcelle": true, "bleu": true, "violet": true, "ambre": true, "rose": true}

var languesConnues = map[string]bool{"en": true, "fr": true, "es": true, "de": true, "it": true, "pt": true}

var texteFichiers = map[string]map[string]string{
	"en": {"demande_le": "Requested on", "remarques": "## Notes along the way", "remarques_aide": "(You can write here while Claude works: it re-reads this file at the start of each cycle.)", "demande_suivante": "## Next request"},
	"fr": {"demande_le": "Demandé le", "remarques": "## Remarques en cours de route", "remarques_aide": "(Vous pouvez écrire ici pendant que Claude travaille : il relit ce fichier au début de chaque cycle.)", "demande_suivante": "## Demande suivante"},
	"es": {"demande_le": "Solicitado el", "remarques": "## Notas sobre la marcha", "remarques_aide": "(Puede escribir aquí mientras Claude trabaja: relee este archivo al inicio de cada ciclo.)", "demande_suivante": "## Siguiente solicitud"},
	"de": {"demande_le": "Angefragt am", "remarques": "## Hinweise unterwegs", "remarques_aide": "(Hier können Sie schreiben, während Claude arbeitet: die Datei wird zu Beginn jedes Zyklus neu gelesen.)", "demande_suivante": "## Nächste Anfrage"},
	"it": {"demande_le": "Richiesto il", "remarques": "## Note lungo il percorso", "remarques_aide": "(Puoi scrivere qui mentre Claude lavora: rilegge questo file all'inizio di ogni ciclo.)", "demande_suivante": "## Richiesta successiva"},
	"pt": {"demande_le": "Pedido em", "remarques": "## Notas pelo caminho", "remarques_aide": "(Pode escrever aqui enquanto o Claude trabalha: ele relê este ficheiro no início de cada ciclo.)", "demande_suivante": "## Pedido seguinte"},
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
