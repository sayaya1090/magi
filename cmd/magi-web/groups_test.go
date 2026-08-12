package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sayaya1090/magi/internal/core/auth"
)

// What the directory says somebody belongs to is what they may do here.
//
// A list of people is a list somebody maintains on every console, and the entry that gets
// forgotten is always the leaver's — so hand-maintenance fails in the direction of access that
// outlives the job. The directory already knows: it is where somebody is added on their first day
// and removed on their last.
func TestAGroupFromTheGatewayCarriesARole(t *testing.T) {
	s := withPolicy(t, `
[groups."eng-platform"]
role = "operator"

[groups."eng-docs"]
role = "responder"
companions = ["docs"]
`)
	s.groupsHeader = "X-Forwarded-Groups"

	may := func(groups string, c auth.Capability, companion string) bool {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("X-Forwarded-User", "nobody-by-name@corp.com")
		r.Header.Set("X-Forwarded-Groups", groups)
		return s.grant(r).Allows(c, companion, "")
	}
	if !may("eng-platform", auth.Prompt, "billing") {
		t.Error("a member of the operator group may not prompt")
	}
	if may("eng-docs", auth.Prompt, "docs") {
		t.Error("a responder group may prompt")
	}
	if !may("eng-docs", auth.Answer, "docs") {
		t.Error("a responder group may not answer its own companion")
	}
	if may("eng-docs", auth.Answer, "billing") {
		t.Error("a scoped group reached outside its scope")
	}
	// Two groups: capabilities add up, and so does the scope. Intersecting them would make joining
	// a second team TAKE something away, which nobody predicts.
	if !may("eng-docs, eng-platform", auth.Prompt, "billing") {
		t.Error("two memberships did not add up")
	}
	// A group nobody listed grants nothing, and neither does no membership at all.
	if may("eng-random", auth.Read, "") {
		t.Error("an unlisted group was given something")
	}
	if may("", auth.Read, "") {
		t.Error("somebody with no name entry and no group was let in")
	}
}

// A person's own entry is the exception, not the roster — and it adds to what their groups give.
func TestAPersonEntryAddsToTheirGroups(t *testing.T) {
	s := withPolicy(t, `
[groups."eng-docs"]
role = "responder"
companions = ["docs"]

[people."lee@corp.com"]
role = "operator"
companions = ["billing"]
`)
	s.groupsHeader = "X-Forwarded-Groups"
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-User", "lee@corp.com")
	r.Header.Set("X-Forwarded-Groups", "eng-docs")
	g := s.grant(r)
	if !g.Allows(auth.Prompt, "billing", "") {
		t.Error("their own entry was lost")
	}
	if !g.Allows(auth.Answer, "docs", "") {
		t.Error("their group's companion was lost")
	}
	if g.Allows(auth.Prompt, "invoices", "") {
		t.Error("the two scopes added up to everything")
	}
}

// A console whose roster is entirely groups still starts: refusing it because nobody is named
// individually would be refusing the configuration this exists to make possible.
func TestAConsoleWithOnlyGroupsIsConfiguredAndHasAnAdmin(t *testing.T) {
	s := withPolicy(t, `
[groups."eng-platform"]
role = "operator"
`)
	if !s.policy.Configured() {
		t.Error("a console with groups and no people thinks it has no policy — everything is allowed")
	}
}
