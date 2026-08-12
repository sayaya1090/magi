package main

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/auth"
)

// Who may use the console, from a terminal.
//
// # Why there is a command as well as a screen
//
// The screen is behind the policy it edits. That is fine every day and wrong exactly twice: when
// there is no policy yet and somebody has to write the first line, and when a policy is wrong in a
// way that keeps its author out. Both are the moments a terminal on that machine is the only way
// in — the same reason a database has a socket for the local superuser.
//
// It edits the same file the console does, through the same two functions, so there is one set of
// rules about what a change may leave behind and not two that agree until they do not.
type peopleOpts struct {
	list      bool
	grant     string
	revoke    string
	role      string
	scope     string
	configDir string
	out       io.Writer
}

func runPeopleCmd(o peopleOpts) int {
	switch {
	case o.grant != "":
		person := auth.Person{Role: strings.TrimSpace(o.role)}
		for _, c := range strings.Split(o.scope, ",") {
			if c = strings.TrimSpace(c); c != "" {
				person.Companions = append(person.Companions, c)
			}
		}
		if person.Role == "" {
			fmt.Fprintln(o.out, "magi: --grant needs --role (try: operator, responder, viewer)")
			return 2
		}
		if err := config.SetPerson(o.configDir, o.grant, person); err != nil {
			fmt.Fprintln(o.out, "magi:", err)
			return 1
		}
		fmt.Fprintf(o.out, "%s is a %s here\n", strings.ToLower(o.grant), person.Role)
		return 0
	case o.revoke != "":
		if err := config.RemovePerson(o.configDir, o.revoke); err != nil {
			fmt.Fprintln(o.out, "magi:", err)
			return 1
		}
		fmt.Fprintf(o.out, "%s can no longer use this console\n", strings.ToLower(o.revoke))
		return 0
	default:
		return listPeople(o.configDir, o.out)
	}
}

func listPeople(configDir string, out io.Writer) int {
	p, err := config.LoadAuth(configDir)
	if err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	if !p.Configured() {
		// Not an empty table. A console with nobody listed is not a console with no answer — it is
		// the one-operator console, and saying which of the two this is saves somebody wondering
		// whether their file was read.
		fmt.Fprintln(out, "nobody is listed: this console has one operator and no policy.")
		fmt.Fprintln(out, "adding the first person turns the gate on for everybody —")
		fmt.Fprintln(out, "  magi --grant you@example.com --role operator")
		return 0
	}
	names := make([]string, 0, len(p.People))
	for who := range p.People {
		names = append(names, who)
	}
	sort.Strings(names)
	for _, who := range names {
		person := p.People[who]
		line := who + "  " + person.Role
		if len(person.Companions) > 0 {
			line += "  (" + strings.Join(person.Companions, ", ") + ")"
		}
		// What the role actually buys, spelled out: a role name is a promise and the capabilities
		// are the promise itself, and reading them beside each other is how somebody notices that
		// "responder" does not include what they thought.
		line += "  [" + capWords(p.Can(who)) + "]"
		fmt.Fprintln(out, line)
	}
	return 0
}

func capWords(cs []auth.Capability) string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, string(c))
	}
	return strings.Join(out, " ")
}
