package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// A skill written before headers existed parses exactly as it did.
//
// Every skill on disk is "first line is the description, the rest is the body". If a header parser
// changed that, upgrading would not break loudly — the skills would simply stop being retrieved,
// and nothing anywhere would say why.
func TestASkillWithNoHeaderIsReadTheOldWay(t *testing.T) {
	h, body := parseSkill("Run the release checklist\nStep one.\nStep two.\n")
	if h.Description != "Run the release checklist" {
		t.Errorf("description = %q", h.Description)
	}
	if body != "Step one.\nStep two." {
		t.Errorf("body = %q", body)
	}
	if h.AgentGroups != nil {
		t.Errorf("an unheadered skill claimed groups: %v", h.AgentGroups)
	}
}

// A header is read when it is there.
func TestAHeaderGivesTheDescriptionAndTheGroups(t *testing.T) {
	h, body := parseSkill(`---
description: Review a diff through a security lens
agent-groups: [review, security]
---
Look for injection first.
`)
	if h.Description != "Review a diff through a security lens" {
		t.Errorf("description = %q", h.Description)
	}
	if want := []string{"review", "security"}; !reflect.DeepEqual(h.AgentGroups, want) {
		t.Errorf("agent-groups = %v, want %v", h.AgentGroups, want)
	}
	if !strings.Contains(body, "Look for injection first.") {
		t.Errorf("body = %q", body)
	}
	if strings.Contains(body, "agent-groups") {
		t.Errorf("the header leaked into the body: %q", body)
	}
}

// Three spellings of a list, because a header is written by hand and being strict about brackets
// would only produce empty lists nobody notices.
func TestAListIsReadHoweverItWasWritten(t *testing.T) {
	for _, in := range []string{"[review, security]", "review, security", "review security", `["review", 'security']`} {
		if got := parseList(in); !reflect.DeepEqual(got, []string{"review", "security"}) {
			t.Errorf("%q parsed as %v", in, got)
		}
	}
	if got := parseList(""); got != nil {
		t.Errorf("an empty value produced %v", got)
	}
}

// An opening --- with no closing one is a BODY that starts with a rule, not a header. Reading it
// as a header would swallow the whole skill.
func TestAnUnclosedMarkerIsNotAHeader(t *testing.T) {
	h, body := parseSkill("---\nthis is just a document that starts with a rule\nand keeps going\n")
	if strings.Contains(h.Description, "document") && body == "" {
		t.Fatalf("the whole skill was eaten as a header: desc=%q body=%q", h.Description, body)
	}
	if !strings.Contains(h.Description+body, "keeps going") {
		t.Errorf("content was lost: desc=%q body=%q", h.Description, body)
	}
}

// A line the parser cannot read is skipped, not fatal. A skill is content; losing the content to
// protect the metadata is the wrong trade.
func TestAJunkHeaderLineDoesNotLoseTheSkill(t *testing.T) {
	h, body := parseSkill(`---
description: Still here
this line has no colon
agent-groups: [review]
---
The body survives.
`)
	if h.Description != "Still here" {
		t.Errorf("description = %q", h.Description)
	}
	if !reflect.DeepEqual(h.AgentGroups, []string{"review"}) {
		t.Errorf("groups = %v", h.AgentGroups)
	}
	if !strings.Contains(body, "The body survives.") {
		t.Errorf("body = %q", body)
	}
}

// The two defaults, which are the whole compatibility story and point opposite ways.
func TestWhoSeesWhat(t *testing.T) {
	cases := []struct {
		name         string
		skill, agent []string
		want         bool
	}{
		{"an unlabelled skill is for everyone", nil, nil, true},
		{"…including a grouped agent", nil, []string{"review"}, true},
		{"a labelled skill is hidden from an agent with no groups", []string{"review"}, nil, false},
		{"…and shown to one that shares a group", []string{"review"}, []string{"review"}, true},
		{"…matching on any one of several", []string{"review", "security"}, []string{"security"}, true},
		{"…and hidden when none match", []string{"review"}, []string{"docs"}, false},
		{"case does not decide it", []string{"Review"}, []string{"review"}, true},
		{"a generalist asks for the whole shelf", []string{"review"}, []string{"*"}, true},
	}
	for _, c := range cases {
		if got := visibleTo(c.skill, c.agent); got != c.want {
			t.Errorf("%s: visibleTo(%v, %v) = %v, want %v", c.name, c.skill, c.agent, got, c.want)
		}
	}
}

// The filter is WIRED, not merely parsed.
//
// Every test above exercises the parser. If Retrieve never consulted it, all of them would still
// pass and no agent's shelf would ever narrow — the defect this session has found twenty times
// over. This one goes through the store.
func TestRetrieveOffersEachAgentOnlyItsOwnShelf(t *testing.T) {
	dir := t.TempDir()
	skills := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skills, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(skills, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("sec.md", "---\ndescription: audit a deploy for secrets\nagent-groups: [security]\n---\ncheck the deploy for leaked keys\n")
	write("docs.md", "---\ndescription: write deploy docs\nagent-groups: [docs]\n---\ndocument the deploy\n")
	// No header at all — the shape every skill on disk has today.
	write("old.md", "deploy the service\nrun the deploy script\n")

	s := New(dir)
	names := func(groups []string) []string {
		_, sk, err := s.Retrieve(context.Background(), "deploy", groups)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, k := range sk {
			out = append(out, k.Name)
		}
		sort.Strings(out)
		return out
	}

	if got, want := names([]string{"security"}), []string{"old", "sec"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a security agent was offered %v, want %v", got, want)
	}
	if got, want := names([]string{"docs"}), []string{"docs", "old"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a docs agent was offered %v, want %v", got, want)
	}
	// An agent with no groups sees the unlabelled one and nothing else — that is what makes
	// labelling a skill shrink somebody's context.
	if got, want := names(nil), []string{"old"}; !reflect.DeepEqual(got, want) {
		t.Errorf("an ungrouped agent was offered %v, want %v", got, want)
	}
	if got, want := names([]string{"*"}), []string{"docs", "old", "sec"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a generalist was offered %v, want %v", got, want)
	}
}
