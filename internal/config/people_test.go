package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/auth"
)

func withAuth(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, AuthFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

const twoPeople = `
[people."kim@corp.com"]
role = "operator"

[people."lee@corp.com"]
role = "responder"
`

// Somebody's role and scope can be changed without rewriting their file.
func TestSetPersonEditsInPlace(t *testing.T) {
	dir := withAuth(t, "# who works here\n"+twoPeople)
	if err := SetPerson(dir, "Lee@corp.com", auth.Person{Role: "operator", Companions: []string{"docs"}}); err != nil {
		t.Fatal(err)
	}
	p, err := LoadAuth(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Lower-cased on the way in, the way the loader compares them.
	got := p.People["lee@corp.com"]
	if got.Role != "operator" || len(got.Companions) != 1 || got.Companions[0] != "docs" {
		t.Errorf("read back %+v", got)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, AuthFile))
	if !strings.Contains(string(raw), "# who works here") {
		t.Errorf("the comment was lost:\n%s", raw)
	}
	// A scope taken away means every companion again, which is a different thing from an empty
	// list and is the ordinary case.
	if err := SetPerson(dir, "lee@corp.com", auth.Person{Role: "operator"}); err != nil {
		t.Fatal(err)
	}
	p, _ = LoadAuth(dir)
	if len(p.People["lee@corp.com"].Companions) != 0 {
		t.Errorf("the scope did not come off: %+v", p.People["lee@corp.com"])
	}
}

// The change that locks the door is refused before it is written.
//
// LoadAuth refuses a file that lists people and gives none of them admin, and a console started
// against one will not run — so the way somebody would find out is a console that does not come
// back up, with the fix on the far side of the door they just locked.
func TestAChangeThatLeavesNobodyInChargeIsRefused(t *testing.T) {
	dir := withAuth(t, twoPeople)
	err := SetPerson(dir, "kim@corp.com", auth.Person{Role: "viewer"})
	if err == nil {
		t.Fatal("the last admin demoted themselves")
	}
	if !strings.Contains(err.Error(), "admin") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
	if err := RemovePerson(dir, "kim@corp.com"); err == nil {
		t.Error("the last admin was deleted")
	}
	// With somebody else in charge, both are fine.
	if err := SetPerson(dir, "lee@corp.com", auth.Person{Role: "operator"}); err != nil {
		t.Fatal(err)
	}
	if err := SetPerson(dir, "kim@corp.com", auth.Person{Role: "viewer"}); err != nil {
		t.Errorf("demoting one of two admins was refused: %v", err)
	}
	if err := RemovePerson(dir, "kim@corp.com"); err != nil {
		t.Errorf("removing a non-admin was refused: %v", err)
	}
	// And emptying the list entirely is allowed: that is not a lockout, it is going back to a
	// console with one operator and no policy, which this tree supports.
	if err := RemovePerson(dir, "lee@corp.com"); err != nil {
		t.Errorf("removing the last person was refused: %v", err)
	}
	p, err := LoadAuth(dir)
	if err != nil || p.Configured() {
		t.Errorf("the file is not back to unconfigured: %+v %v", p.People, err)
	}
}

// A role nobody defined is a typo, and is refused with the list.
func TestAnUnknownRoleIsRefused(t *testing.T) {
	dir := withAuth(t, twoPeople)
	err := SetPerson(dir, "new@corp.com", auth.Person{Role: "opreator"})
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Errorf("a typo'd role was taken: %v", err)
	}
}
