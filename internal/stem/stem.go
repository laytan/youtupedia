package stem

import (
	"strings"

	"github.com/reiver/go-porterstemmer"
)

func StemLine(value string) string {
    words := StemLineWords(value)

	stemmed := strings.Builder{}
	for _, word := range words {
        stemmed.WriteString(word)
		stemmed.WriteString(" ")
	}

	return stemmed.String()
}

func StemLineWords(value string) []string {
    repper := strings.NewReplacer( // TODO: maybe just replace all non-alpha?
		",", "",
		".", "",
		"!", "",
        "?", "",
		`"`, "",
		`\`, "",
		"[", "",
		"]", "",
        "(", "",
        ")", "",
        "~", "",
	)
	noSpecial := repper.Replace(value)
    words := strings.Fields(noSpecial)

    for i, word := range words {
        words[i] = string(porterstemmer.Stem([]rune(word)))
    }

    return words
}
