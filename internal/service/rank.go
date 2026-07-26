package service

import "strings"

// Lexicographic ranking for board cards.
//
// A rank is a string compared with plain byte ordering, so inserting between two cards
// means finding a string between their two ranks — one row write, never a renumbering
// of the column. That property is what makes a shared board safe to reorder: two people
// dragging at once touch one row each instead of fighting over all of them.
//
// The scheme rests on one invariant: '0' is reserved as a below-everything prefix and
// is never generated as a rank's final character. Because every rank therefore ends in
// a character greater than '0', prefixing '0' always yields something that sorts
// strictly before it — which is what makes "drag to the top" work without limit. Drop
// that invariant and a rank of exactly "0" becomes reachable, and nothing in this
// alphabet can sort before it.
const (
	// rankFloor is the reserved prefix. It is compared against, never generated as a
	// terminal character.
	rankFloor = '0'
	// rankAlphabet is what ranks are built from — deliberately excluding rankFloor.
	rankAlphabet = "123456789abcdefghijklmnopqrstuvwxyz"
	// FirstRank starts an unordered column mid-alphabet, so the first drags in either
	// direction do not immediately grow the string.
	FirstRank = "n"
)

// RankBetween returns a rank that sorts strictly between prev and next.
//
// Empty prev means "before everything"; empty next means "after everything". The result
// is always non-empty, always ends in a generated character, and is strictly ordered —
// including for adjacent inputs, where the string grows by a character rather than
// failing.
func RankBetween(prev, next string) string {
	switch {
	case prev == "" && next == "":
		return FirstRank
	case prev == "":
		return rankBefore(next)
	case next == "":
		return rankAfter(prev)
	case prev >= next:
		// Neighbours come from a sorted column, so this means the caller's view is
		// stale. Landing the card after prev keeps it where the user aimed instead of
		// rejecting the drag; the next read re-sorts everyone.
		return rankAfter(prev)
	}
	return rankMid(prev, next)
}

// rankBefore returns a rank sorting strictly before next.
func rankBefore(next string) string {
	if next == "" {
		return FirstRank
	}
	// Room to step down within the alphabet on the first character alone.
	if i := indexIn(next[0]); i > 0 {
		return string(rankAlphabet[i/2])
	}
	// next starts at the bottom of the alphabet (or at the reserved floor), so go below
	// it by prefixing the floor. This is why the floor is never generated as a terminal
	// character: it guarantees this step always has somewhere to go.
	return string(rankFloor) + rankBefore(next[1:])
}

// rankAfter returns a rank sorting strictly after prev.
func rankAfter(prev string) string {
	if prev == "" {
		return FirstRank
	}
	last := prev[len(prev)-1]
	if i := indexIn(last); i >= 0 && i < len(rankAlphabet)-1 {
		// Halfway to the top rather than one step, so repeatedly appending to the end
		// of a column does not walk the alphabet a character at a time.
		return prev[:len(prev)-1] + string(rankAlphabet[(i+len(rankAlphabet))/2])
	}
	// Already at the top of the alphabet: extend instead of overflowing.
	return prev + FirstRank
}

// rankMid returns a rank strictly between two ordered, non-empty ranks. It grows the
// string only when the two are already adjacent at every shared position.
func rankMid(prev, next string) string {
	var out strings.Builder
	for i := 0; ; i++ {
		p := byteAt(prev, i, rankFloor)
		n := byteAt(next, i, 0) // 0 = past the end of next, i.e. no upper bound here

		if p == n {
			out.WriteByte(p) // shared prefix — keep descending
			continue
		}
		if mid, ok := midChar(p, n); ok {
			out.WriteByte(mid)
			return out.String()
		}
		// Adjacent at this position. Keep prev's character and continue after the rest
		// of prev, which is unbounded above within this prefix.
		out.WriteByte(p)
		return out.String() + rankAfter(sliceFrom(prev, i+1))
	}
}

// midChar returns a generated character strictly between lo and hi, if one exists.
// hi == 0 means "no upper bound at this position".
func midChar(lo, hi byte) (byte, bool) {
	l := indexIn(lo) // -1 when lo is the reserved floor, i.e. below the whole alphabet
	h := len(rankAlphabet)
	if hi != 0 {
		h = indexIn(hi)
		if h < 0 {
			return 0, false // hi is the floor; nothing generated sorts below it
		}
	}
	if h-l < 2 {
		return 0, false
	}
	return rankAlphabet[(l+h)/2], true
}

// indexIn returns c's position in the alphabet, or -1 for anything outside it (the
// reserved floor, or a value hand-edited into the database).
func indexIn(c byte) int { return strings.IndexByte(rankAlphabet, c) }

func byteAt(s string, i int, fallback byte) byte {
	if i < len(s) {
		return s[i]
	}
	return fallback
}

func sliceFrom(s string, i int) string {
	if i >= len(s) {
		return ""
	}
	return s[i:]
}
