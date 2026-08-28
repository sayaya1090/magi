package auth

import "testing"

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
		t.Error("a name or group the gateway sent padded or capitalised must still resolve")
	}
}

// An unscoped match anywhere means unscoped. Everywhere is kept apart from an empty Companions
// list because those mean the same thing and are reached differently: folding them would make
// "narrowed to nothing" indistinguishable from "narrowed to everything".
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
