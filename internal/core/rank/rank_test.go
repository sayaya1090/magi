package rank

import "testing"

// A rare shared word beats a common one.
//
// This is the whole reason the package is not a word count. Both documents below share exactly one
// query token with the query; the one sharing the token that appears in every other document is
// saying nothing, and a counter cannot tell them apart.
func TestARareWordOutranksACommonOne(t *testing.T) {
	docs := []string{
		"the handler retries the handler on failure",       // shares "handler", which everything has
		"dehydration of a shard restores the original",     // shares "dehydration", which nothing else has
		"the handler owns the handler's lock",              // handler again
		"a handler is registered per handler in the table", // and again
	}
	hits := ByIDF("dehydration handler", docs)
	if len(hits) == 0 {
		t.Fatal("nothing matched a query whose every token appears in the corpus")
	}
	if hits[0].Index != 1 {
		t.Errorf("ranked doc %d first; the one carrying the rare token is 1 — a count would tie "+
			"them and pick whichever came first", hits[0].Index)
	}
}

// Ranking and deciding-what-is-good-enough are separate, and the caller does the second.
//
// A threshold in here made a search for what somebody wrote down impossible: a query of one rare
// word is the normal case for that, and it carried one token of two. The hits come back with how
// many tokens they matched so a caller that wants precision can still be strict.
func TestItRanksLooselyAndReportsHowMuchMatched(t *testing.T) {
	docs := []string{"the handler retries", "an invoice is issued monthly"}
	hits := ByIDF("handler dehydration", docs)
	if len(hits) != 1 || hits[0].Index != 0 {
		t.Fatalf("a half-matching query found %v; ranking is not filtering", hits)
	}
	if hits[0].Matched != 1 {
		t.Errorf("Matched is %d — a caller that wants two distinct tokens has no way to tell "+
			"without it", hits[0].Matched)
	}
}

// Nothing to rank, and nothing to rank against.
func TestEmptyInputsRankNothing(t *testing.T) {
	if hits := ByIDF("", []string{"a", "b"}); hits != nil {
		t.Errorf("an empty query matched %v", hits)
	}
	if hits := ByIDF("anything", nil); hits != nil {
		t.Errorf("an empty corpus matched %v", hits)
	}
	// A query of nothing but short words tokenizes to nothing, which is not the same as matching
	// everything — it is the caller having said nothing to match on.
	if hits := ByIDF("a to of", []string{"a to of"}); hits != nil {
		t.Errorf("a query of grammar matched %v", hits)
	}
}

// The same inputs always give the same order.
//
// Ranking backs a tool result that lands in a model's context. An order that moved between
// identical calls would make a run unreproducible for no reason anybody could see.
func TestTheOrderIsStable(t *testing.T) {
	docs := []string{"alpha beta", "alpha beta", "alpha beta", "alpha beta"}
	first := ByIDF("alpha beta", docs)
	if len(first) != 4 {
		t.Fatalf("%d of 4 matched", len(first))
	}
	for range 20 {
		again := ByIDF("alpha beta", docs)
		for i := range first {
			if again[i].Index != first[i].Index {
				t.Fatalf("the order moved between identical calls: %v then %v", first, again)
			}
		}
	}
	// And ties keep the corpus order rather than an arbitrary one.
	for i, h := range first {
		if h.Index != i {
			t.Errorf("tied documents came back as %v; the input order is the tiebreak", first)
			break
		}
	}
}
