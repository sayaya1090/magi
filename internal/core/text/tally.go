package text

import (
	"fmt"
	"strings"
)

// Tally renders names as distinct entries in first-seen order, with ×N on the ones that repeat:
// ["read","edit","edit","bash"] joined by " · " reads "read · edit×2 · bash". Empty in, empty out.
//
// # Why it is here and not in the two places that had it
//
// This convention was written twice, in two layers, and both are read by the same person on the
// same screen: the header badge counting live subagents ("explore×3, coder") and the compaction
// brief counting a path's tools ("read · edit×2 · bash"). Neither package could reach the other's
// copy — one is an adapter and one is the application — so it was spelled out in full on both
// sides, differing only in the separator.
//
// Written twice it can drift, and every way it could drift is visible: the multiplication sign,
// whether ordering is first-seen or alphabetical, whether a single occurrence gets a count. The
// same fact spelled two ways on one screen is the cost, so the spelling lives in one place.
func Tally(names []string, sep string) string {
	order := make([]string, 0, len(names))
	count := map[string]int{}
	for _, n := range names {
		if count[n] == 0 {
			order = append(order, n)
		}
		count[n]++
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		if count[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s×%d", n, count[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, sep)
}
