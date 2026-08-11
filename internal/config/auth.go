package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	if len(p.People) > 0 && !anyAdmin(p) {
		return auth.Policy{}, fmt.Errorf("%s lists people and none of them may %q — nobody could "+
			"change that afterwards", AuthFile, auth.Admin)
	}
	return p, nil
}

func anyAdmin(p auth.Policy) bool {
	for who := range p.People {
		if p.Allows(who, auth.Admin, "") {
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
