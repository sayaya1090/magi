package main

import (
	"fmt"
	"io"
	"os"
	osuser "os/user"
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
type accessOpts struct {
	list      bool
	grant     string
	revoke    string
	role      string
	scope     string
	configDir string
	out       io.Writer
}

func runAccessCmd(o accessOpts) int {
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
		return listAccess(o.configDir, o.out)
	}
}

func listAccess(configDir string, out io.Writer) int {
	p, err := config.LoadAuth(configDir)
	if err != nil {
		fmt.Fprintln(out, "magi:", err)
		return 1
	}
	// Whose list this is, first. In a terminal the question is sharper than on a screen: MAGI_CONFIG_DIR
	// may be set, the shell may be somebody else's through sudo, and both make this command edit a
	// policy other than the one the person meant. The account, the machine, and the directory the
	// answer came out of.
	fmt.Fprintf(out, "%s  %s\n\n", whoAndWhere(), configDir)
	if !p.Configured() {
		// Not an empty table. A console with nobody listed is not a console with no answer — it is
		// the one-operator console, and saying which of the two this is saves somebody wondering
		// whether their file was read.
		fmt.Fprintln(out, "nobody is listed: this console has one operator and no policy.")
		fmt.Fprintln(out, "adding the first person turns the gate on for everybody —")
		fmt.Fprintln(out, "  magi --grant you@example.com --role operator")
		return 0
	}
	// Groups above the people, because on a console wired to a directory the groups ARE the roster —
	// membership is maintained where somebody is hired and let go — and the people below them are
	// the exceptions to it. A listing that showed only the exceptions would say "three people" about
	// a console the whole team can reach.
	if len(p.Groups) > 0 {
		groups := make([]string, 0, len(p.Groups))
		for name := range p.Groups {
			groups = append(groups, name)
		}
		sort.Strings(groups)
		for _, name := range groups {
			g := p.Groups[name]
			line := "@" + name + "  " + g.Role
			if len(g.Companions) > 0 {
				line += "  (" + strings.Join(g.Companions, ", ") + ")"
			}
			line += "  [" + capWords(p.Roles[g.Role].Can) + "]"
			fmt.Fprintln(out, line)
		}
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

// whoAndWhere is the account and the machine, which together are what a policy belongs to.
//
// Not an address: an IP says how to REACH a machine and a machine has several. The pair is what
// the person already calls this magi, and it is the trust boundary too — two accounts on one host
// read two config directories and enforce two policies, and neither can write the other's files.
func whoAndWhere() string {
	user, host := "", ""
	if u, err := osuser.Current(); err == nil {
		user = u.Username
	}
	if h, err := os.Hostname(); err == nil {
		host = h
	}
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case host != "":
		return host
	case user != "":
		return user
	default:
		return "this machine"
	}
}
