// Package auth decides what a person may do with a console.
//
// # Why roles rather than a list of people
//
// One console, one operator, and the question never came up. A second person makes it real, and
// the shape it takes is not "who may log in" — that is the gateway's answer — but "what may this
// one do once they are in". A list of people with their permissions written out beside each is a
// list that drifts: the third person is granted whatever the second happens to have, and nobody
// can say afterwards what "the same as Lee" was supposed to mean. A role is the intent, written
// once, and a person is a name pointing at it.
//
// # Capabilities are read off the routes, not invented
//
// Every one of these corresponds to something the console can actually do. A capability with no
// route behind it is a word in a config file that reads like a promise, and the first person to
// rely on it finds out it never meant anything.
//
// # Pure
//
// No files, no HTTP, no clock. What is here is the decision, so a test of "may a responder change
// the model" is three lines and does not need a server — and the gate that calls it (magi-web's
// mayDo) has nothing left to get wrong but the wiring, which has its own test.
package auth

import "strings"

// Capability is one kind of thing a person may do.
type Capability string

const (
	// Read is everything that only looks: the fleet, a transcript, history, search, the loop map.
	Read Capability = "read"
	// Answer is resolving what the agent is blocked on, and stopping a turn. Deliberately apart
	// from Prompt: somebody trusted to unblock a companion is not necessarily somebody who decides
	// what it works on, and that is the most common shape of a second person.
	Answer Capability = "answer"
	// Prompt is giving the agent work: submitting, steering, dispatching, compacting, and moving a
	// companion to another of its conversations.
	Prompt Capability = "prompt"
	// Curate is what the agent learns from: the experience store and the report format.
	Curate Capability = "curate"
	// Configure is how it runs: the model, the approval mode, the schedule, the tool servers.
	Configure Capability = "configure"
	// Admin is people, roles, and admitting companions — the capability that can grant the others.
	Admin Capability = "admin"
	// Shell is running a command in the workspace, outside the permission policy a tool call goes
	// through. Never granted on a console started with -exposed, where the route does not exist.
	Shell Capability = "shell"
)

// All is every capability, in the order a person reading a settings screen would want them:
// widening, roughly, from looking to owning.
var All = []Capability{Read, Answer, Prompt, Curate, Configure, Admin, Shell}

// Known reports whether c is a capability this build enforces. A role naming something else is a
// role whose author believed something that is not true, so the loader says so rather than
// silently granting nothing.
func Known(c Capability) bool {
	for _, k := range All {
		if k == c {
			return true
		}
	}
	return false
}

// Role is a named set of capabilities.
type Role struct {
	Can []Capability `toml:"can"`
}

// Person is somebody the gateway can name, and what they may do here.
type Person struct {
	Role string `toml:"role"`
	// Companions narrows them to some of the fleet, by name. Empty means every companion — which
	// is the ordinary case and must stay the one that needs no configuration.
	Companions []string `toml:"companions"`
}

// Policy is the whole of it: the roles, and who has which.
//
// The zero value is the console as it has always been — nobody configured, so nobody to check, and
// the operator in front of the machine does what they always did. That is not a loophole: with no
// gateway in front there is exactly one person who can reach a loopback port, and inventing a
// permission model for them would be a login screen for a house with one occupant.
type Policy struct {
	Roles  map[string]Role   `toml:"roles"`
	People map[string]Person `toml:"people"`
}

// Configured reports whether anybody has been given a role. Until somebody has, the console is a
// single-operator console and every request is theirs.
func (p Policy) Configured() bool { return len(p.People) > 0 }

// Allows answers the whole question: may this person do this, to this companion.
//
// `who` is the name the gateway gave, which is empty when nothing in front named anybody. On a
// configured console that is a refusal and not a fallback — an unnamed caller getting the
// operator's powers is exactly the hole this exists to close.
//
// `companion` is the name of the companion the request is about and `peer` the console it was
// reported by — empty for the ones that are about the console itself.
func (p Policy) Allows(who string, c Capability, companion, peer string) bool {
	if !p.Configured() {
		return true // one operator, one machine; see Policy
	}
	person, ok := p.People[strings.ToLower(strings.TrimSpace(who))]
	if !ok {
		return false
	}
	role, ok := p.Roles[person.Role]
	if !ok {
		return false // a role that was deleted grants nothing, rather than everything
	}
	if !has(role.Can, c) {
		return false
	}
	return withinScope(person.Companions, companion, peer)
}

// Can is what this person may do at all, for a page deciding what to draw. It is NOT the check —
// see the note on hiding in the console's own gate.
func (p Policy) Can(who string) []Capability {
	if !p.Configured() {
		return append([]Capability(nil), All...)
	}
	person, ok := p.People[strings.ToLower(strings.TrimSpace(who))]
	if !ok {
		return nil
	}
	return append([]Capability(nil), p.Roles[person.Role].Can...)
}

// Scope is the companions this person may act on, empty for all of them.
func (p Policy) Scope(who string) []string {
	if !p.Configured() {
		return nil
	}
	return append([]string(nil), p.People[strings.ToLower(strings.TrimSpace(who))].Companions...)
}

// InScope reports whether this person may see or act on one companion, named and placed.
//
// Exported because a scope is not one check. The gate can ask it for a request that NAMES its
// companion, but the routes that answer "what else is there" — the fleet, the interventions — have
// nothing for a gate to inspect and must filter what they produce instead. One predicate, three
// callers, rather than three ideas of what a scope means.
//
// `peer` is the console a companion was reported by, empty for this machine. An entry may be a
// bare name, which matches that name anywhere, or "peer/name", which matches one machine's. Bare
// is the common case and the dangerous one: two machines set up by one person run companions with
// the same names, so `docs` means docs EVERYWHERE and the qualified form is how somebody says
// otherwise.
func (p Policy) InScope(who, companion, peer string) bool {
	if !p.Configured() {
		return true
	}
	person, ok := p.People[strings.ToLower(strings.TrimSpace(who))]
	if !ok {
		return false
	}
	return withinScope(person.Companions, companion, peer)
}

// withinScope: an empty list is every companion, and a request about no companion in particular
// (the console's own screens) is not narrowed by one.
func withinScope(only []string, companion, peer string) bool {
	if len(only) == 0 || companion == "" {
		return true
	}
	for _, n := range only {
		if strings.EqualFold(n, companion) {
			return true
		}
		if here, want, found := strings.Cut(n, "/"); found &&
			strings.EqualFold(here, peer) && strings.EqualFold(want, companion) {
			return true
		}
	}
	return false
}

func has(cs []Capability, want Capability) bool {
	for _, c := range cs {
		if c == want {
			return true
		}
	}
	return false
}

// Builtin is the three roles a console starts with, so a fleet of one does not have to design a
// permission model before anybody can log in. They are ordinary roles: editable, and replaceable.
//
// operator carries Shell even though an -exposed console refuses that route outright. The two are
// different statements — what this person is trusted with, and what this console offers anybody —
// and collapsing them would silently rewrite somebody's role when a flag changed.
func Builtin() map[string]Role {
	return map[string]Role{
		"operator":  {Can: []Capability{Read, Answer, Prompt, Curate, Configure, Admin, Shell}},
		"responder": {Can: []Capability{Read, Answer}},
		"viewer":    {Can: []Capability{Read}},
	}
}
