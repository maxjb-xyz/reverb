// Package matching implements Crate's external⇄library matcher and the shared,
// pure Normalize() used by both matching and (future) dedup_key. Normalization is
// SYMMETRIC: callers apply it to both sides before comparison.
package matching

import (
	"regexp"
	"strings"
)

// diacriticFold maps common Latin diacritics to ASCII. Cyrillic/CJK are untouched.
var diacriticFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a', 'ā': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'ē': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i', 'ī': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ø': 'o', 'ō': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u', 'ū': 'u',
	'ç': 'c', 'ñ': 'n', 'ý': 'y', 'ÿ': 'y', 'ß': 's',
}

func foldDiacritics(s string) string {
	var b strings.Builder
	for _, r := range s {
		if f, ok := diacriticFold[r]; ok {
			b.WriteRune(f)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// featRe removes a feat./featuring/ft. group: an optional opening paren/bracket,
// the keyword, and everything to the end of the string. Applied after lowercasing.
var featRe = regexp.MustCompile(`(?i)\s*[\(\[]?\s*\b(feat\.?|featuring|ft\.?)\b.*$`)

// ptRe expands a "pt"/"pt." token at a word boundary into "part".
var ptRe = regexp.MustCompile(`\bpt\.?\b`)

// dropRe matches characters to DROP: anything not a letter, digit, space, or paren.
// \p{L} and \p{N} cover Cyrillic/CJK code points.
var dropRe = regexp.MustCompile(`[^\p{L}\p{N}\s()]+`)

var wsRe = regexp.MustCompile(`\s+`)

// Normalize lowercases, folds Latin diacritics, strips feat groups symmetrically,
// expands &→and and pt→part, removes stray punctuation (keeping parentheses so
// version qualifiers like "(remaster 2011)" survive), and collapses whitespace.
// It is pure and deterministic. It does NOT strip version qualifiers.
func Normalize(s string) string {
	s = foldDiacritics(s)
	s = strings.ToLower(s)
	s = featRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&", " and ")
	s = ptRe.ReplaceAllString(s, "part")
	s = dropRe.ReplaceAllString(s, " ")
	s = wsRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}
