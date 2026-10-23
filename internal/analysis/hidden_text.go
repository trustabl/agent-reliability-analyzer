package analysis

import (
	"strings"
	"unicode"

	"github.com/trustabl/trustabl/internal/models"
)

// Hidden text in agent guidance files (AGENTS.md). Agents read these files as
// instructions and humans review them as rendered text, so characters that are
// invisible or that reorder what a reviewer sees can carry instructions only the
// model reads. The checks are objective code-point tests, reusing the same
// helpers as the skill body markers (CSKILL-040); no phrase matching.
//
// Legitimate uses are exempted so ordinary files are not flagged: a leading byte
// order mark, emoji joined with a zero-width joiner, a joiner or non-joiner
// between non-ASCII letters, and a subdivision-flag emoji, which is the one
// legitimate use of the Tags block. Right-to-left prose that uses bidirectional
// controls is still flagged, and is a documented false positive.

const (
	hiddenZeroWidthReason = "contains zero-width characters"
	hiddenTagsReason      = "contains Unicode Tags-block characters (invisible text)"
	hiddenBidiReason      = "contains bidirectional override characters"
)

// hiddenTextHits returns one hit per kind of hidden text found in raw, each at
// the line of its first occurrence. The text itself is never echoed.
func hiddenTextHits(path string, raw []byte) []models.ConfigHit {
	rs := []rune(string(raw))
	line := 1
	var zw, tags, bidi int
	for i, r := range rs {
		switch {
		case r == '\n':
			line++
		case strings.ContainsRune(zeroWidthChars, r):
			if zw == 0 && !benignZeroWidth(rs, i) {
				zw = line
			}
		case r >= 0xE0000 && r <= 0xE007F:
			if tags == 0 && !isFlagTagSequence(rs, i) {
				tags = line
			}
		case (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
			if bidi == 0 {
				bidi = line
			}
		}
	}
	var out []models.ConfigHit
	if zw > 0 {
		out = append(out, hit(path, zw, hiddenZeroWidthReason))
	}
	if tags > 0 {
		out = append(out, hit(path, tags, hiddenTagsReason))
	}
	if bidi > 0 {
		out = append(out, hit(path, bidi, hiddenBidiReason))
	}
	return out
}

func isEmojiish(r rune) bool {
	return r >= 0x1F000 || (r >= 0x2190 && r <= 0x2BFF) || r == 0xFE0F
}

func isZeroWidthRune(r rune) bool { return strings.ContainsRune(zeroWidthChars, r) }

// benignZeroWidth exempts the legitimate uses of the zero-width set: a byte order
// mark at the very start of the file, and a joiner or non-joiner that sits between
// two non-ASCII characters of the same kind (emoji sequences, or letters in
// scripts such as Persian and Hindi). A run of adjacent zero-width characters, or
// one placed inside ASCII text, is never exempt.
func benignZeroWidth(rs []rune, i int) bool {
	r := rs[i]
	if r == 0xFEFF && i == 0 {
		return true
	}
	if r != 0x200D && r != 0x200C {
		return false
	}
	if i == 0 || i == len(rs)-1 {
		return false
	}
	prev, next := rs[i-1], rs[i+1]
	if prev < 0x80 || next < 0x80 || isZeroWidthRune(prev) || isZeroWidthRune(next) {
		return false
	}
	return (isEmojiish(prev) && isEmojiish(next)) || (unicode.IsLetter(prev) && unicode.IsLetter(next))
}

// isFlagTagSequence reports whether the Tags-block run containing index i is a
// subdivision-flag emoji: a black-flag base (U+1F3F4) followed by tag characters
// that end with the cancel tag (U+E007F).
func isFlagTagSequence(rs []rune, i int) bool {
	start, end := i, i
	for start > 0 && rs[start-1] >= 0xE0000 && rs[start-1] <= 0xE007F {
		start--
	}
	for end < len(rs) && rs[end] >= 0xE0000 && rs[end] <= 0xE007F {
		end++
	}
	return start > 0 && rs[start-1] == 0x1F3F4 && rs[end-1] == 0xE007F
}
