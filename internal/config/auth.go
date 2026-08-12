package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/sayaya1090/magi/internal/core/auth"
)

// AuthFile is where who-may-do-what lives.
//
// Its own file, and NOT config.toml, for one reason that decides it: config.toml is merged with a
// project's .magi/config.toml, so a repository could grant itself a role by checking one in. A
// permission model a cloned repository can edit is not one.
const AuthFile = "auth.toml"

// LoadAuth reads the policy from a config directory.
//
// A missing file is the ordinary case and not an error: it is a console with one operator, which
// is what every console is until somebody puts a gateway in front of it.
//
// A file that is THERE and wrong is a hard error, and the caller is expected to refuse to start on
// it. The two ways of being wrong both end somewhere worse than stopping: a policy that fails to
// parse would leave nobody able to do anything, and one naming a capability this build does not
// have would leave somebody believing they had granted something. Both are discovered at the
// moment the operator is looking at what they just wrote, which is the only good time.
func LoadAuth(dir string) (auth.Policy, error) {
	p := auth.Policy{Roles: auth.Builtin(), People: map[string]auth.Person{}}
	b, err := os.ReadFile(filepath.Join(dir, AuthFile))
	if err != nil {
		if os.IsNotExist(err) {
			return p, nil
		}
		return auth.Policy{}, fmt.Errorf("reading %s: %w", AuthFile, err)
	}
	var got auth.Policy
	if _, err := toml.Decode(string(b), &got); err != nil {
		return auth.Policy{}, fmt.Errorf("%s: %w", AuthFile, err)
	}
	// The file's roles win over the built-in ones of the same name, and the rest stay: an operator
	// who narrowed "responder" has not thereby deleted "viewer".
	for name, r := range got.Roles {
		p.Roles[name] = r
	}
	var bad []string
	for name, r := range p.Roles {
		for _, c := range r.Can {
			if !auth.Known(c) {
				bad = append(bad, fmt.Sprintf("%s may %q", name, c))
			}
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return auth.Policy{}, fmt.Errorf("%s names capabilities this build does not have (%s) — "+
			"the ones it has are %s", AuthFile, strings.Join(bad, ", "), capList())
	}
	// Lower-cased on the way in, once. An address is not case-sensitive and a gateway may spell it
	// either way; comparing case-insensitively at every call site instead would be four places
	// that have to remember.
	// Groups first, and validated the same way: a group naming a role this build does not have is
	// the same mistake as a person naming one, and finding out at the moment it matters — somebody
	// from that group being refused everything — is the worst time.
	for name, person := range got.Groups {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			return auth.Policy{}, fmt.Errorf("%s: a group is listed with no name", AuthFile)
		}
		if _, ok := p.Roles[person.Role]; !ok {
			return auth.Policy{}, fmt.Errorf("%s: the group %s is given the role %q, which is not "+
				"defined here or built in", AuthFile, key, person.Role)
		}
		if p.Groups == nil {
			p.Groups = map[string]auth.Person{}
		}
		p.Groups[key] = person
	}
	for who, person := range got.People {
		key := strings.ToLower(strings.TrimSpace(who))
		if key == "" {
			return auth.Policy{}, fmt.Errorf("%s: somebody is listed with no name", AuthFile)
		}
		if _, ok := p.Roles[person.Role]; !ok {
			return auth.Policy{}, fmt.Errorf("%s: %s is given the role %q, which is not defined "+
				"here or built in", AuthFile, key, person.Role)
		}
		p.People[key] = person
	}
	// Somebody has to be able to grant a role, or the console is locked in whatever shape it is in
	// now — including with nobody able to fix it.
	if p.Configured() && !anyAdmin(p) {
		return auth.Policy{}, fmt.Errorf("%s lists people and none of them may %q — nobody could "+
			"change that afterwards", AuthFile, auth.Admin)
	}
	return p, nil
}

// anyAdmin: somebody, by name or by membership, may change this policy.
//
// A group counts. On a console where the directory is the roster, there may be no people listed at
// all — and refusing to start that because nobody is named individually would be refusing the
// configuration this feature exists to make possible.
func anyAdmin(p auth.Policy) bool {
	for who := range p.People {
		if p.Allows(who, auth.Admin, "", "") {
			return true
		}
	}
	for name := range p.Groups {
		if p.AllowsWith("", []string{name}, auth.Admin, "", "") {
			return true
		}
	}
	return false
}

func capList() string {
	names := make([]string, 0, len(auth.All))
	for _, c := range auth.All {
		names = append(names, string(c))
	}
	return strings.Join(names, ", ")
}

// HasAdmin reports whether anybody in this policy may change it.
//
// Exported because the writer needs the same answer the loader does: LoadAuth refuses a file that
// lists people and gives none of them `admin`, and a console started against one refuses to run at
// all. So a change that would produce that file has to be refused BEFORE it is written — otherwise
// the way somebody finds out is a console that will not come back up, with the fix on the far side
// of the door they just locked.
func HasAdmin(p auth.Policy) bool { return anyAdmin(p) }

// SetPerson writes one person's role and scope into auth.toml, and reports what stopped it.
//
// Surgical, like every other named-table editor here: the file is somebody's, comments and all,
// and a program that rewrites it wholesale is one they stop editing by hand.
//
// The check is on the RESULT, not on the change: demoting the last admin, deleting them, or
// narrowing them to a companion they cannot use are three different edits with one consequence,
// and the consequence is the thing worth refusing.
func SetPerson(dir, who string, p auth.Person) error {
	who = strings.ToLower(strings.TrimSpace(who))
	if who == "" {
		return fmt.Errorf("no name given")
	}
	now, err := LoadAuth(dir)
	if err != nil {
		return err
	}
	if _, ok := now.Roles[p.Role]; !ok {
		return fmt.Errorf("%q is not a role here — this console has %s", p.Role, roleList(now))
	}
	after := withPerson(now, who, &p)
	after.Groups = now.Groups
	if !HasAdmin(after) {
		return fmt.Errorf("that would leave nobody who may %q, and a console with people and no "+
			"admin refuses to start — give somebody else the role first", auth.Admin)
	}
	path := filepath.Join(dir, AuthFile)
	section := `people."` + who + `"`
	if err := SetKey(path, section, "role", p.Role); err != nil {
		return err
	}
	if len(p.Companions) == 0 {
		// An empty list is not the same as no list: no list means every companion, which is the
		// ordinary case and the one that must need no configuration.
		return SetRawKey(path, section, "companions", "")
	}
	return SetRawKey(path, section, "companions", quotedList(p.Companions))
}

// RemovePerson takes somebody off the list, refusing the removal that locks the door.
func RemovePerson(dir, who string) error {
	who = strings.ToLower(strings.TrimSpace(who))
	now, err := LoadAuth(dir)
	if err != nil {
		return err
	}
	if _, listed := now.People[who]; !listed {
		return fmt.Errorf("%s is not on the list", who)
	}
	after := withPerson(now, who, nil)
	after.Groups = now.Groups
	// One person left is a special case worth naming: removing the LAST person is not a lockout,
	// it is going back to a console with one operator and no policy at all, which is a state this
	// tree supports and somebody may well want.
	if after.Configured() && !HasAdmin(after) {
		return fmt.Errorf("that would leave nobody who may %q — give somebody else the role first",
			auth.Admin)
	}
	return RemoveSection(filepath.Join(dir, AuthFile), `people."`+who+`"`)
}

// withPerson is the policy as it would be after one change. A copy, because the caller is holding
// the current one and a check that mutated it would be a check that had already happened.
func withPerson(p auth.Policy, who string, person *auth.Person) auth.Policy {
	next := auth.Policy{Roles: p.Roles, People: map[string]auth.Person{}}
	for k, v := range p.People {
		if k != who {
			next.People[k] = v
		}
	}
	if person != nil {
		next.People[who] = *person
	}
	return next
}

func roleList(p auth.Policy) string {
	names := make([]string, 0, len(p.Roles))
	for n := range p.Roles {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func quotedList(in []string) string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, strconv.Quote(s))
		}
	}
	return "[" + strings.Join(out, ", ") + "]"
}
