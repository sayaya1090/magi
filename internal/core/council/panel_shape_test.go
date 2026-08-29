package council

import (
	"strings"
	"testing"
)

// A weak model and a strong one judging the same work is the point of that configuration, and one
// request would answer with whichever backend came first — so the panel is one call only when the
// members share a backend. Both the adapter that makes the calls and the reader that tells somebody
// what the council is doing ask this, which is why the predicate is here and not in either of them:
// a reader describing a shape the adapter is not running is a report of a council that did not sit.
func TestAPanelIsOneCallOnlyWhenEverySeatSharesABackend(t *testing.T) {
	for _, tc := range []struct {
		name    string
		members []Member
		want    bool
	}{
		{"three seats on one backend", []Member{
			{Name: "melchior", Provider: "local", Model: "big"},
			{Name: "balthasar", Provider: "local", Model: "big"},
			{Name: "casper", Provider: "local", Model: "big"},
		}, true},
		// The third seat, not the second: a check that stopped comparing after one member would
		// call this one call and send three seats' work to one backend of the two configured.
		{"the third seat is elsewhere", []Member{
			{Name: "melchior", Provider: "local", Model: "big"},
			{Name: "balthasar", Provider: "local", Model: "big"},
			{Name: "casper", Provider: "cloud", Model: "big"},
		}, false},
		{"two seats on one backend", []Member{
			{Name: "melchior", Provider: "local", Model: "big"},
			{Name: "balthasar", Provider: "local", Model: "big"},
		}, true},
		{"same backend, different models", []Member{
			{Name: "melchior", Provider: "local", Model: "big"},
			{Name: "balthasar", Provider: "local", Model: "small"},
		}, false},
		// One seat is not a panel however it is configured: there is nothing to fold together, and
		// calling it one panel would have the reader describe a deliberation that is one opinion.
		{"a single seat", []Member{{Name: "melchior", Provider: "local", Model: "big"}}, false},
		{"no seats at all", nil, false},
	} {
		if got := OnePanel(tc.members); got != tc.want {
			t.Errorf("%s: OnePanel = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The suite-walk clause is the answer to a measured failure — three lenses voted done citing
// "7 passed" while the graded suite failed — and it is addressed to the lens that walks behaviors.
// Appending it to the others would tell a member reading for style or completeness to go looking
// for test output, which is not their question and is the over-demand the council was already
// being pulled back from.
func TestTheSuiteWalkClauseGoesToTheLensThatWalksBehaviours(t *testing.T) {
	with := RouteWith("verification", true)
	if !strings.HasPrefix(with, RouteFor("verification")) {
		t.Error("the clause replaced the route instead of tightening it")
	}
	if !strings.HasSuffix(with, SuiteWalkClause) {
		t.Errorf("verification was asked to walk the suite and was not told how: %q", with)
	}
	for _, tc := range []struct {
		lens string
		on   bool
	}{
		{"verification", false}, // not asked for
		{"correctness", true},   // asked for, but not this lens's question
		{"completeness", true},
		{"a lens nobody configured", true}, // the neutral route, also unchanged
	} {
		if got := RouteWith(tc.lens, tc.on); got != RouteFor(tc.lens) {
			t.Errorf("RouteWith(%q, %v) is not the plain route: %q", tc.lens, tc.on, got)
		}
	}
}
