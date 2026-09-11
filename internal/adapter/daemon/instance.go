package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// instanceID is THIS process, and it is the only thing that can tell a replacement from a reuse.
//
// ⚠ **A PID cannot.** Two facts wear the same disguise when a client is holding a number and
// checking whether the thing it started is still there:
//
//   - the daemon updated itself and re-executed, so the socket now belongs to a DIFFERENT process
//     that is nonetheless the same companion, carrying the same work forward;
//   - the daemon died and the operating system handed its number to something unrelated.
//
// In both the published PID stops matching, or matches something that is not magi. A client that
// reads only the number either kills a stranger or adopts one — docs/CLIENT_LIFECYCLE §4 names
// both ("클라이언트는 발견한 새 PID를 임의로 자기 자식으로 채택하지 않습니다") and asks for this
// instead: readiness is confirmed when the child it spawned, the workspace it resolved, and the
// instance id in the published record and in `about` all agree.
//
// ⚠ **Fresh per process, and never inherited.** It is generated here and read from nowhere — no
// environment variable, no file. That is the whole property: a successor that carried its
// predecessor's id would report a replacement as a continuation, which is the one thing this is
// for. The lineage that DOES survive a replacement is the owner's, and it travels by a different
// road (an inherited pipe, docs/CLIENT_LIFECYCLE §4) because it carries authority and this does
// not. This id is for telling processes apart; it proves nothing.
var (
	instanceOnce sync.Once
	instanceVal  string
)

// InstanceID returns this process's instance id, minting it on first use.
//
// Stable for the life of the process, because a client compares what `about` says now against what
// the record said a moment ago; an id that changed between two reads would make every check fail.
func InstanceID() string {
	instanceOnce.Do(func() {
		var b [12]byte
		// A failed read would give every daemon on the machine the same all-zero id, which is worse
		// than no id: identical ids make DIFFERENT processes look like one. Say so by leaving it
		// empty instead — absent is a fact a reader can act on, and the field is omitempty.
		if _, err := rand.Read(b[:]); err != nil {
			return
		}
		instanceVal = hex.EncodeToString(b[:])
	})
	return instanceVal
}
