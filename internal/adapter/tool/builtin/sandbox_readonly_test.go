//go:build darwin

package builtin

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The transcript stays read-only inside a writable cache directory.
//
// The profile denies writes and then re-allows the build caches so toolchains keep working — and
// on macOS the session store lives at ~/Library/Caches/magi, inside one of them. So a confined
// command could rewrite or delete any companion's transcript, including the one recording what it
// was doing at that moment. Seatbelt takes the last matching rule, which is what makes carving a
// subtree back out possible at all.
func TestTheSessionStoreIsNotWritableFromASandboxedCommand(t *testing.T) {
	store := "/Users/somebody/Library/Caches/magi"
	p := seatbeltProfile(port.SandboxSpec{Mode: "workspace-write", Workdir: "/w/api", ReadOnly: []string{store}})

	deny := "(deny file-write* (subpath \"" + store + "\"))"
	if !strings.Contains(p, deny) {
		t.Fatalf("the profile does not carve the store out:\n%s", p)
	}
	// AFTER the allow block, or it means nothing: seatbelt's last match wins.
	if strings.Index(p, deny) < strings.LastIndex(p, "(allow file-write*") {
		t.Error("the carve-out is written before the allow it is meant to override")
	}
	// And the workspace is still writable, which is the mode's whole purpose.
	if !strings.Contains(p, "(subpath \"/w/api\")") {
		t.Errorf("the workspace lost its write access:\n%s", p)
	}
}
