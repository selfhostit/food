package food

import (
	"slices"
	"strings"
)

// Words splits a search into its normalized words, at most six.
func Words(q string) []string {
	words := strings.Fields(Normalize(q))
	if len(words) > 6 {
		words = words[:6]
	}
	return words
}

// Stemmed drops common plural endings from the words, so "apples", "pommes"
// and "eieren" find "apple", "pomme" and "eier". It returns nil when no word
// had one.
func Stemmed(words []string) []string {
	out := make([]string, len(words))
	changed := false
	for i, w := range words {
		out[i] = w
		if len(w) < 5 {
			continue
		}
		for _, end := range []string{"eren", "en", "es", "s"} {
			if strings.HasSuffix(w, end) {
				out[i] = strings.TrimSuffix(w, end)
				changed = true
				break
			}
		}
	}
	if !changed {
		return nil
	}
	return out
}

// FieldScore rates how well one normalized name matches the words: each word
// found counts, more at the start of a word and more again as a whole word; a
// name that starts with the first word or holds every word counts extra.
func FieldScore(text string, words []string) float64 {
	if text == "" || len(words) == 0 {
		return 0
	}
	padded := " " + text + " "
	s, all := 0.0, true
	for _, w := range words {
		switch {
		case strings.Contains(padded, " "+w+" "), strings.Contains(padded, " "+w+"s "), strings.Contains(padded, " "+w+"es "):
			// a whole word, or its plural: "apple" in "Apples, raw"
			s += 4
		case strings.Contains(padded, " "+w):
			s += 3
		case strings.Contains(text, w):
			s += 1
		default:
			all = false
		}
	}
	if strings.HasPrefix(text, words[0]) {
		s += 3
	}
	if all {
		s += 4
	}
	return s
}

// CoversAll reports whether every word of the normalized text is one of the
// words, or its plural: "rice" for "white rice", "apples" for "apple".
func CoversAll(text string, words []string) bool {
	for _, t := range strings.Fields(text) {
		if !slices.ContainsFunc(words, func(w string) bool { return t == w || t == w+"s" || t == w+"es" }) {
			return false
		}
	}
	return text != ""
}

// HasOtherWord reports whether the normalized name holds one of these words
// that wasn't asked for.
func HasOtherWord(name string, these, asked []string) bool {
	for _, w := range these {
		if strings.Contains(" "+name+" ", " "+w+" ") && !slices.Contains(asked, w) {
			return true
		}
	}
	return false
}
