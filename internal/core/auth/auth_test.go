package auth

import (
	"reflect"
	"testing"
)

// The package doc offers this: "a test of 'may a responder change the model' is three lines and
// does not need a server". Until now the package had no test file at all, so the offer had never
// been taken up and nobody had found out whether it was true.
//
// It is. Every case below is a Policy literal and one call.

func responderPolicy() Policy {
	return Policy{
		Roles: map[string]Role{
			"responder": {Can: []Capability{Read, Answer}},
			"operator":  {Can: All},
		},
		People: map[string]Person{
			"lee": {Role: "responder"},
			"kim": {Role: "operator"},
		},
	}
}

func TestAResponderMayAnswerAndMayNotConfigure(t *testing.T) {
	p := responderPolicy()
	if !p.Allows("lee", Answer, "", "") {
		t.Error("a responder must be able to unblock a companion — that is the whole role")
	}
	if p.Allows("lee", Configure, "", "") {
		t.Error("a responder changed the model; Answer and Prompt are kept apart on purpose")
	}
	if !p.Allows("kim", Configure, "", "") {
		t.Error("an operator may configure")
	}
}

// The zero Policy is the console as it always was, and that is a decision rather than an omission:
// with no gateway in front, one person can reach a loopback port.
func TestAnUnconfiguredConsoleIsTheOperatorsOwn(t *testing.T) {
	var p Policy
	if p.Configured() {
		t.Fatal("nobody has been given a role, so there is nobody to check")
	}
	for _, c := range All {
		if !p.Allows("", c, "anything", "anywhere") {
			t.Errorf("%s refused on an unconfigured console — a login screen for a house with one occupant", c)
		}
	}
	if got := p.Can(""); len(got) != len(All) {
		t.Errorf("Can returned %d capabilities, want all %d", len(got), len(All))
	}
}

// Once somebody IS configured, a caller the gateway did not name is a refusal and not a fallback.
func TestAnUnnamedCallerOnAConfiguredConsoleGetsNothing(t *testing.T) {
	p := responderPolicy()
	for _, c := range All {
		if p.Allows("", c, "", "") {
			t.Errorf("an unnamed caller was granted %s — that is the hole this exists to close", c)
		}
	}
	if g := p.GrantTo("", nil); g.Named {
		t.Error("an unnamed caller must not resolve to a named grant: the difference between " +
			"'allowed nothing' and 'not here' is which of the operator and the gateway to go fix")
	}
}

// A role that was deleted grants nothing rather than everything. The dangerous direction is the
// other one: a person whose role name no longer resolves, read as unrestricted.
func TestAPersonWhoseRoleWasDeletedGrantsNothing(t *testing.T) {
	p := Policy{
		Roles:  map[string]Role{"operator": {Can: All}},
		People: map[string]Person{"lee": {Role: "auditor"}}, // the role is gone
	}
	if p.Allows("lee", Read, "", "") {
		t.Error("a dangling role name granted Read")
	}
	if g := p.GrantTo("lee", nil); g.Named {
		t.Error("a person pointing at a role that does not exist is not a named grant")
	}
}

// Additive, the way RBAC is everywhere else. Intersecting would make joining a second team TAKE
// something away.
func TestGroupsAndNamesUnion(t *testing.T) {
	p := Policy{
		Roles: map[string]Role{
			"reader":    {Can: []Capability{Read}},
			"responder": {Can: []Capability{Answer}},
		},
		Groups: map[string]Person{"support": {Role: "reader", Companions: []string{"docs"}}},
		People: map[string]Person{"lee": {Role: "responder", Companions: []string{"build"}}},
	}
	if !p.AllowsWith("lee", []string{"support"}, Read, "docs", "") {
		t.Error("what the group gives was lost")
	}
	if !p.AllowsWith("lee", []string{"support"}, Answer, "build", "") {
		t.Error("what the name gives was lost")
	}
	if p.AllowsWith("lee", []string{"support"}, Answer, "release", "") {
		t.Error("a companion in neither scope was allowed; the union is of what was granted, not of everything")
	}
	// Case and surrounding space come from a gateway header, not from a person typing carefully.
	if !p.AllowsWith("  LEE  ", []string{" Support "}, Read, "DOCS", "") {
		t.Error("a name, group, or companion the gateway sent padded or capitalised must still " +
			"resolve; the companion is folded by withinScope, the other two on the way in")
	}
}

// An unscoped match anywhere means unscoped. The flag is not there to tell an empty scope from an
// open one — withinScope reads empty as every companion, so those are one state. It is there for
// the union below: lee's own entry names one companion, so the resolved list is NOT empty, and
// without the flag the list check would narrow a person the unscoped group opened up.
func TestAnUnscopedMatchOpensTheScope(t *testing.T) {
	p := Policy{
		Roles:  map[string]Role{"reader": {Can: []Capability{Read}}},
		Groups: map[string]Person{"everyone": {Role: "reader"}}, // no companions named = all of them
		People: map[string]Person{"lee": {Role: "reader", Companions: []string{"docs"}}},
	}
	g := p.GrantTo("lee", []string{"everyone"})
	if !g.Everywhere {
		t.Fatal("a person in an unscoped group is unscoped")
	}
	if !p.AllowsWith("lee", []string{"everyone"}, Read, "release", "") {
		t.Error("the unscoped group did not open the scope")
	}
	if got := p.ScopeWith("lee", []string{"everyone"}); got != nil {
		t.Errorf("ScopeWith returned %v for an unscoped caller; a screen reading that list would "+
			"draw a narrowing that is not there", got)
	}
	// And without the group, the narrowing stands.
	if p.Allows("lee", Read, "release", "") {
		t.Error("the person's own entry names one companion and was not narrowed to it")
	}
}

// A bare entry matches that companion on any machine. Two consoles set up by one person run
// companions with the same names, so the qualified form is how somebody says otherwise.
func TestBareScopeMatchesAnywhereAndQualifiedMatchesOneMachine(t *testing.T) {
	p := Policy{
		Roles: map[string]Role{"reader": {Can: []Capability{Read}}},
		People: map[string]Person{
			"lee": {Role: "reader", Companions: []string{"docs"}},
			"kim": {Role: "reader", Companions: []string{"west/docs"}},
		},
	}
	if !p.Allows("lee", Read, "docs", "east") || !p.Allows("lee", Read, "docs", "") {
		t.Error("a bare entry must match that name on every machine, including this one")
	}
	if !p.Allows("kim", Read, "docs", "west") {
		t.Error("the qualified entry did not match its own machine")
	}
	if p.Allows("kim", Read, "docs", "east") {
		t.Error("a qualified entry matched another machine's companion of the same name — the " +
			"exact confusion the peer half exists to prevent")
	}
	// A request about no companion in particular (the console's own screens) is not narrowed.
	if !p.Allows("kim", Read, "", "") {
		t.Error("a scoped person was refused a screen that is about no companion")
	}
}

// Two predicates answer "is this in scope" and they DISAGREE about a caller the policy does not
// know. Pinned rather than reconciled here, because reconciling them changes what the console
// admits and that is not this package's call to make quietly.
//
//   - Grant.InScope is the scope half ALONE. A caller with no name resolves to the zero Grant,
//     whose empty Companions list reads as "not narrowed", so it answers true.
//   - Policy.InScopeWith asks both halves and refuses an unnamed caller outright.
//
// The live filter — magi-web's seen(), the one behind every route that answers with a LIST — is
// built on the first, so on a configured console an unnamed caller passes it. What keeps that from
// being an open door today is the capability gate in front: Grant.Allows checks Can first, and an
// unnamed caller has none. It is a single point of failure standing where the comment above
// Policy.InScope claims a shared predicate stands.
func TestTheTwoScopePredicatesDisagreeAboutAnUnnamedCaller(t *testing.T) {
	p := responderPolicy()
	unnamed := p.GrantTo("", nil)

	if !unnamed.InScope("docs", "") {
		t.Error("Grant.InScope is the scope half on its own; an empty list is every companion, and " +
			"this test exists to notice if that changes")
	}
	if p.InScopeWith("", nil, "docs", "") {
		t.Error("Policy.InScopeWith asks whether the policy knows the caller at all, and must refuse")
	}
	// The capability gate is what actually stops the unnamed caller. If this ever passes, the
	// disagreement above stops being latent.
	if unnamed.Allows(Read, "docs", "") {
		t.Error("an unnamed caller was allowed Read: the one check standing in front of the scope " +
			"filter has gone, and the filter alone says yes")
	}
}

func TestKnownRejectsACapabilityThisBuildDoesNotEnforce(t *testing.T) {
	for _, c := range All {
		if !Known(c) {
			t.Errorf("%s is in All and not Known", c)
		}
	}
	if Known("deploy") {
		t.Error("an unknown capability read as known; a role naming it would be a promise the " +
			"console cannot keep")
	}
}

// The scope a screen is handed is a copy. It comes from the policy's own Person entry, and a
// caller that sorted or appended to it in place would be editing the permission model of everybody
// who asks next — a narrowing that nobody wrote and nothing records.
func TestTheScopeHandedOutIsACopy(t *testing.T) {
	p := Policy{
		Roles:  map[string]Role{"reader": {Can: []Capability{Read}}},
		People: map[string]Person{"lee": {Role: "reader", Companions: []string{"docs", "ops"}}},
	}
	got := p.Scope("lee")
	if want := []string{"docs", "ops"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Scope = %v, want %v", got, want)
	}
	got[0] = "release"
	if again := p.Scope("lee"); again[0] != "docs" {
		t.Errorf("Scope = %v after the first caller wrote to the slice it was given", again)
	}
	if again := p.People["lee"].Companions; again[0] != "docs" {
		t.Errorf("the policy itself now says %v: a screen rewrote the permission model", again)
	}
}

// Nobody configured means nobody narrowed, and an unscoped person is not narrowed either. Both
// answer nil rather than an empty non-nil list, because a screen drawing "scoped to: (nothing)"
// tells the reader they have been shut out of a fleet they can see all of.
func TestNobodyNarrowedIsNoScopeAtAll(t *testing.T) {
	if got := (Policy{}).Scope("lee"); got != nil {
		t.Errorf("an unconfigured console answered %v", got)
	}
	p := Policy{
		Roles:  map[string]Role{"reader": {Can: []Capability{Read}}},
		People: map[string]Person{"lee": {Role: "reader"}},
	}
	if got := p.Scope("lee"); got != nil {
		t.Errorf("a person named with no companions answered %v", got)
	}
}

// A companion is named AND placed, and the two are not interchangeable. "west/docs" means the docs
// on west, and a caller that handed the pair over the other way round would let somebody through to
// another machine's companion of the same name — which is the whole reason the qualified form
// exists.
func TestOneCompanionIsNamedAndPlaced(t *testing.T) {
	p := Policy{
		Roles:  map[string]Role{"reader": {Can: []Capability{Read}}},
		People: map[string]Person{"kim": {Role: "reader", Companions: []string{"west/docs"}}},
	}
	if !p.InScope("kim", "docs", "west") {
		t.Error("the qualified entry did not match its own machine")
	}
	if p.InScope("kim", "west", "docs") {
		t.Error("the companion and the console it was reported by were read the wrong way round")
	}
	if p.InScope("kim", "docs", "east") {
		t.Error("a qualified entry matched another machine's companion of the same name")
	}
	if p.InScope("", "docs", "west") {
		t.Error("a configured console must refuse a caller it does not know, not fall through")
	}
	if !(Policy{}).InScope("", "docs", "west") {
		t.Error("an unconfigured console is the operator's own")
	}
}

// The three roles a console starts with. Their contents are pinned because they are what somebody
// gets before they have read anything about permissions, and because operator carrying Shell is a
// deliberate statement — what this person is trusted with, as against what an -exposed console
// offers anybody — that reads like an oversight to anyone tidying up.
func TestTheConsoleStartsWithThreeRolesAndOperatorMayRunACommand(t *testing.T) {
	want := map[string][]Capability{
		"operator":  {Read, Answer, Prompt, Curate, Configure, Admin, Shell},
		"responder": {Read, Answer},
		"viewer":    {Read},
	}
	got := Builtin()
	if len(got) != len(want) {
		t.Fatalf("Builtin has %d roles: %v", len(got), got)
	}
	for name, cans := range want {
		if !reflect.DeepEqual(got[name].Can, cans) {
			t.Errorf("%s may %v, want %v", name, got[name].Can, cans)
		}
		for _, c := range got[name].Can {
			if !Known(c) {
				t.Errorf("%s names %q, which this build does not enforce — the loader would "+
					"reject magi's own defaults", name, c)
			}
		}
	}
}

// Editable and replaceable, says the doc: an operator renaming a builtin role or taking a
// capability off one is editing their own console's copy, not the next caller's.
func TestEditingTheBuiltinRolesEditsOnlyYourCopy(t *testing.T) {
	mine := Builtin()
	delete(mine, "viewer")
	mine["operator"] = Role{Can: []Capability{Read}}
	mine["responder"].Can[0] = Admin

	again := Builtin()
	if len(again) != 3 {
		t.Fatalf("Builtin has %d roles after a caller edited an earlier one: %v", len(again), again)
	}
	if got := again["responder"].Can; !reflect.DeepEqual(got, []Capability{Read, Answer}) {
		t.Errorf("responder may %v: the capability slice was shared with an earlier caller", got)
	}
	if got := again["operator"].Can; len(got) != 7 {
		t.Errorf("operator may %v", got)
	}
}

// An unconfigured console answers "everything", and it must answer with a copy. All is a
// package-level slice that Known walks on every capability check in the process, so a screen that
// sorted or truncated what it was handed here would not be narrowing one caller — it would be
// changing what this build enforces, for everybody, with nothing written down.
func TestAnUnconfiguredConsoleHandsOutACopyOfEveryCapability(t *testing.T) {
	p := Policy{}
	got := p.Can("anyone")
	if !reflect.DeepEqual(got, All) {
		t.Fatalf("Can = %v, want every capability %v", got, All)
	}
	got[0] = "nonsense"
	if All[0] != Read {
		t.Fatalf("All is now %v: one caller rewrote the capability list of the whole build", All)
	}
	if again := p.Can("anyone"); again[0] != Read {
		t.Errorf("Can = %v after the first caller wrote to the slice it was given", again)
	}
}

// On a configured console the same question answers with what the role gives and nothing else.
// This is what a page draws itself from, so an answer that fell back to All when somebody happened
// to be configured would draw a responder every control they may not use.
func TestAConfiguredConsoleAnswersWithWhatTheRoleGives(t *testing.T) {
	p := responderPolicy()
	if got, want := p.Can("lee"), []Capability{Read, Answer}; !reflect.DeepEqual(got, want) {
		t.Errorf("Can = %v, want %v", got, want)
	}
	if got := p.Can("stranger"); len(got) != 0 {
		t.Errorf("Can = %v for somebody the policy does not know", got)
	}
}
