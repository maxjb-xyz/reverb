package dj

import (
	"strings"
	"unicode"

	"github.com/maxjb-xyz/reverb/internal/matching"
)

// Looser matching for requests the exact rules miss: names with symbols
// ("A$AP Rocky" typed as "asap rocky"), half a name ("kendrick"), a typo
// ("kendrik lamar"), a part of a compound genre tag ("rap" for "Hip-Hop/Rap")
// and moods ("something chill").

// fillerWords never identify an artist or a genre on their own.
var fillerWords = toSet(`a an the and or of to in on for from with by at it is be me my i you your we us
	play put playing some something somethin anything stuff songs song tracks track tunes music
	more like similar sounds style kind sort type please pls just really maybe little bit also
	want wanna give get let lets can could would should need now then that this these those what
	good best great nice cool other else new old mix playlist radio station artist band dj
	vibe vibes mood moods feel feeling time today tonight right next`)

// genreStopWords are words of genre tags too general to stand for the tag.
var genreStopWords = toSet(`music new old the and of other misc unknown various general songs song
	de la el y n`)

// moods map a mood or activity to genres that suit it, most fitting first.
// The first one the library has wins.
var moods = []struct {
	words  []string
	genres []string
}{
	{[]string{"chill", "chilled", "chillout", "relax", "relaxing", "relaxed", "mellow", "calm", "laid back", "lofi", "lo fi", "cozy", "cosy", "easy"},
		[]string{"lo fi", "lofi", "chillhop", "chill", "chillout", "downtempo", "ambient", "jazz hop", "r and b", "rnb", "soul", "acoustic", "jazz"}},
	{[]string{"hype", "hyped", "party", "pump", "pumped", "workout", "gym", "lift", "lifting", "run", "running", "energy", "energetic", "upbeat", "turn up", "lit", "banger", "bangers", "aggressive", "loud"},
		[]string{"phonk", "trap", "drill", "hip hop", "rap", "edm", "electronic", "dance", "house", "drum and bass", "dubstep", "metal", "rock", "pop"}},
	{[]string{"sad", "melancholy", "melancholic", "heartbreak", "heartbroken", "emotional", "crying", "cry", "moody", "rainy"},
		[]string{"emo", "emo rap", "sad", "indie", "alternative", "singer songwriter", "r and b", "rnb", "acoustic", "soul"}},
	{[]string{"focus", "study", "studying", "concentrate", "concentration", "work", "working", "sleep", "sleepy", "reading"},
		[]string{"lo fi", "lofi", "ambient", "classical", "instrumental", "piano", "chillhop", "jazz", "downtempo", "post rock", "soundtrack"}},
	{[]string{"romantic", "sexy", "date night", "slow jams"},
		[]string{"r and b", "rnb", "soul", "neo soul", "pop"}},
}

func toSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// artistForm is one way of writing a library artist's name, as a key.
type artistForm struct {
	key string
	n   Name
}

// artistForms lists each artist under its key and under the forms people
// type for names with symbols: "A$AP Rocky" is "a ap rocky", "aap rocky" and
// "asap rocky".
func artistForms(artists []Name) []artistForm {
	out := make([]artistForm, 0, len(artists)*2)
	for _, n := range artists {
		seen := map[string]bool{}
		for _, k := range []string{
			n.Key,
			matching.ArtistKey(strings.ReplaceAll(n.Name, "$", "s")),
			matching.ArtistKey(strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
					return r
				}
				return -1
			}, n.Name)),
		} {
			if k != "" && !seen[k] {
				seen[k] = true
				out = append(out, artistForm{key: k, n: n})
			}
		}
	}
	return out
}

// findArtist returns the artist whose longest form appears in text as whole
// words, and where it starts. Forms shorter than two letters never match.
func findArtist(text string, forms []artistForm) (Name, int) {
	best, bestLen, at := Name{}, 0, -1
	for _, f := range forms {
		if len(f.key) < 2 || len(f.key) <= bestLen {
			continue
		}
		if i := strings.Index(text, padded(f.key)); i >= 0 {
			best, bestLen, at = f.n, len(f.key), i
		}
	}
	return best, at
}

// looseArtist finds an artist from part of a name or a misspelling. A word
// counts as part of a name only when it belongs to exactly one artist.
func looseArtist(text string, forms []artistForm) (Name, int, bool) {
	words := strings.Fields(text)
	// Half a name: "kendrick", "carti".
	var best Name
	bestWord, at := "", -1
	for _, w := range words {
		if len(w) < 4 || fillerWords[w] || len(w) <= len(bestWord) {
			continue
		}
		var hit Name
		hits := map[string]bool{}
		for _, f := range forms {
			if strings.Contains(padded(f.key), padded(w)) {
				hits[f.n.Key], hit = true, f.n
			}
		}
		if len(hits) == 1 {
			best, bestWord, at = hit, w, strings.Index(text, padded(w))
		}
	}
	if at >= 0 {
		return best, at, true
	}

	// A typo: the closest name within a letter or two.
	bestDist := 3
	for i := range words {
		for j := i + 1; j <= len(words) && j <= i+4; j++ {
			if fillerWords[words[i]] || fillerWords[words[j-1]] {
				continue
			}
			g := strings.Join(words[i:j], " ")
			for _, f := range forms {
				limit := typoLimit(len(f.key))
				if limit == 0 || abs(len(f.key)-len(g)) > limit {
					continue
				}
				if d := editDistance(g, f.key); d <= limit && d < bestDist {
					best, bestDist, at = f.n, d, strings.Index(text, padded(g))
				}
			}
		}
	}
	return best, at, at >= 0
}

// typoLimit is how many letters a name of n letters may be off by.
func typoLimit(n int) int {
	switch {
	case n >= 8:
		return 2
	case n >= 5:
		return 1
	}
	return 0
}

// partOfGenre finds a genre whose tag contains a phrase from text: "rap"
// for "Hip-Hop/Rap". The longest phrase wins, then the bigger genre.
func partOfGenre(text string, genres []Name) (Name, bool) {
	var best Name
	bestLen := 0
	for _, g := range genres {
		ws := strings.Fields(g.Key)
		for i := range ws {
			for j := i + 1; j <= len(ws); j++ {
				p := strings.Join(ws[i:j], " ")
				if len(p) < 3 || len(p) <= bestLen || (j-i == 1 && genreStopWords[p]) {
					continue
				}
				if strings.Contains(text, padded(p)) {
					best, bestLen = g, len(p)
				}
			}
		}
	}
	return best, bestLen > 0
}

// genreFor finds the library genre for a genre phrase: the genre itself, or
// one whose tag contains it.
func genreFor(phrase string, genres []Name) (Name, bool) {
	for _, g := range genres {
		if g.Key == phrase {
			return g, true
		}
	}
	for _, g := range genres {
		if strings.Contains(padded(g.Key), padded(phrase)) {
			return g, true
		}
	}
	return Name{}, false
}

// moodGenre reads a mood or activity and picks a library genre that suits it.
func moodGenre(text string, genres []Name) (Name, bool) {
	for _, m := range moods {
		for _, w := range m.words {
			if !strings.Contains(text, padded(w)) {
				continue
			}
			for _, cand := range m.genres {
				if g, ok := genreFor(cand, genres); ok {
					return g, true
				}
			}
		}
	}
	return Name{}, false
}

// likeLane is "sounds-like" when a like-word comes right before position at.
func likeLane(text string, at int) string {
	before := text[:at+1]
	for _, w := range likeWords {
		if strings.HasSuffix(before, padded(w)) {
			return "sounds-like"
		}
	}
	return ""
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			c := 1
			if ra[i-1] == rb[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
