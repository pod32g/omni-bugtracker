package service

import (
	"sort"
	"strings"
	"testing"
)

func TestRankBetweenOrders(t *testing.T) {
	for _, tc := range []struct{ prev, next string }{
		{"", ""}, {"", "n"}, {"n", ""}, {"a", "b"}, {"a", "z"}, {"0", "1"},
		{"n", "nn"}, {"zz", ""}, {"abc", "abd"}, {"01", "1"},
	} {
		got := RankBetween(tc.prev, tc.next)
		if got == "" {
			t.Errorf("RankBetween(%q, %q) = empty", tc.prev, tc.next)
			continue
		}
		if tc.prev != "" && !(got > tc.prev) {
			t.Errorf("RankBetween(%q, %q) = %q, not after prev", tc.prev, tc.next, got)
		}
		if tc.next != "" && !(got < tc.next) {
			t.Errorf("RankBetween(%q, %q) = %q, not before next", tc.prev, tc.next, got)
		}
	}
}

// "0" is the floor: nothing in this alphabet sorts before it, so "insert before 0" has
// no correct answer. The generator never produces it — TestGeneratedRanksNeverEndAtTheFloor
// is what guarantees that — so this can only arrive from a hand-edited database. The
// contract is that it degrades to a usable rank rather than panicking or returning
// empty, and the card lands at the top on the next read once the bad row is fixed.
func TestRankBeforeTheFloorIsUnreachableAndDegradesSafely(t *testing.T) {
	got := RankBetween("", string(rankFloor))
	if got == "" {
		t.Fatal("RankBetween must never return an empty rank")
	}
	if got[len(got)-1] == rankFloor {
		t.Errorf("degraded rank %q still ends at the floor", got)
	}
}

// Repeatedly inserting into the same gap is the pathological case for any ordering
// scheme: with integer positions it forces a renumber, and with a bad string scheme it
// either collides or grows without bound. It must keep working and stay short.
func TestRankBetweenRepeatedInsertionInTheSameGap(t *testing.T) {
	lo, hi := RankBetween("", ""), RankBetween("n", "")
	for i := 0; i < 200; i++ {
		mid := RankBetween(lo, hi)
		if !(mid > lo && mid < hi) {
			t.Fatalf("iteration %d: %q not strictly between %q and %q", i, mid, lo, hi)
		}
		hi = mid // always insert just above lo — the tightest possible squeeze
	}
	// Growth is inherent to any midpoint scheme under a worst-case squeeze; what
	// matters is that it stays linear-ish and small, not that it never grows.
	if len(hi) > 64 {
		t.Errorf("rank grew to %d chars after 200 insertions in one gap: %q", len(hi), hi)
	}
}

// Dragging a card to the top, repeatedly, is the other direction of the same problem.
func TestRankBeforeRepeatedly(t *testing.T) {
	top := RankBetween("", "")
	for i := 0; i < 200; i++ {
		next := RankBetween("", top)
		if !(next < top) {
			t.Fatalf("iteration %d: %q is not before %q", i, next, top)
		}
		top = next
	}
}

// A whole column reordered by repeated insertion must read back in the order it was
// built, which is the only property the board actually depends on.
func TestRanksSortIntoInsertionOrder(t *testing.T) {
	var ranks []string
	prev := ""
	for i := 0; i < 50; i++ {
		r := RankBetween(prev, "")
		ranks = append(ranks, r)
		prev = r
	}
	shuffled := append([]string(nil), ranks...)
	sort.Slice(shuffled, func(a, b int) bool { return shuffled[a] > shuffled[b] }) // reverse
	sort.Strings(shuffled)

	for i := range ranks {
		if shuffled[i] != ranks[i] {
			t.Fatalf("position %d: sorted %q, inserted %q", i, shuffled[i], ranks[i])
		}
	}
}

// A stale client can send neighbours in the wrong order. The drop should still land
// where the user aimed rather than be rejected.
func TestRankBetweenToleratesInvertedNeighbours(t *testing.T) {
	got := RankBetween("z", "a")
	if !(got > "z") {
		t.Errorf("RankBetween(%q, %q) = %q, want it to fall after prev", "z", "a", got)
	}
}

// The invariant the scheme rests on: a generated rank never ends in the reserved floor
// character. If it did, a rank of exactly "0" would become reachable and nothing in the
// alphabet could sort before it — "drag to the top" would have no answer.
func TestGeneratedRanksNeverEndAtTheFloor(t *testing.T) {
	var ranks []string
	prev, next := "", ""
	for i := 0; i < 100; i++ {
		r := RankBetween(prev, next)
		ranks = append(ranks, r)
		prev = r
	}
	top := RankBetween("", "")
	for i := 0; i < 100; i++ {
		top = RankBetween("", top)
		ranks = append(ranks, top)
	}
	for _, r := range ranks {
		if r == "" || r[len(r)-1] == rankFloor {
			t.Fatalf("rank %q ends at the reserved floor", r)
		}
	}
}

func TestRanksUseOnlySafeCharacters(t *testing.T) {
	prev := ""
	for i := 0; i < 100; i++ {
		r := RankBetween(prev, "")
		for _, c := range []byte(r) {
			if c != rankFloor && strings.IndexByte(rankAlphabet, c) < 0 {
				t.Fatalf("rank %q contains %q, outside the alphabet", r, string(c))
			}
		}
		prev = r
	}
}
