package stem

import (
	"strings"
	"sync"

	"github.com/reiver/go-porterstemmer"
)

var builders = sync.Pool{
	New: func() any {
		return &strings.Builder{}
	},
}

// StemLine is a highly optimized way of stemming a line, removing common punctuation.
func StemLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	b := builders.Get().(*strings.Builder)
	b.Reset()
	b.Grow(len(value))

	lastSpace := -1
	for i, ch := range value {
		if ch == ' ' {
			if i > 1 {
				word := strings.TrimFunc(value[lastSpace+1:i], trimPuntuation)
				b.WriteString(porterstemmer.StemString(word))
				b.WriteByte(byte(' '))
			}

			lastSpace = i
		}
	}

	word := strings.TrimFunc(value[lastSpace+1:], trimPuntuation)
	b.WriteString(porterstemmer.StemString(word))

	s := b.String()
	builders.Put(b)
	return s
}

func trimPuntuation(r rune) bool {
	return r == ',' || r == '.' || r == '!' || r == '?' || r == '"'
}
