package stem

import (
	"strings"

	"github.com/reiver/go-porterstemmer"
)

func StemLine(value string) string {
	repper := strings.NewReplacer(
		",", "",
		".", "",
		"!", "",
		`"`, "",
		`\`, "",
		"[", "",
		"]", "",
	)
	noSpecial := repper.Replace(value)
	words := strings.Fields(noSpecial)

	stemmed := strings.Builder{}
	for _, word := range words {
		ws := string(porterstemmer.Stem([]rune(word)))
		stemmed.WriteString(ws)
		stemmed.WriteString(" ")
	}

	return stemmed.String()
}
