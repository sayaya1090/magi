package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
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

// OwnerEnv carries the owning lineage across a process replacement.
//
// The successor of a self-update is the SAME owner's daemon — the window that started it never
// stopped owning it — so the id has to survive a re-exec, and the environment is how anything
// survives one here (graceful.Reexec passes os.Environ() through). It is a name, not a key: see
// OwnerID on why knowing it grants nothing.
const OwnerEnv = "MAGI_OWNER_ID"

var (
	ownerOnce sync.Once
	ownerVal  string
)

// OwnerID is the owning LINEAGE — one window's daemon and every successor that replaced it.
//
// ⚠ **Empty unless somebody owns this daemon.** It is minted by AdoptOwner, which only the owned
// mode calls. A daemon started from a terminal has no owner, and answering with an id anyway would
// claim a lineage that does not exist — the same defect as advertising a feature that is not built.
// Absent is the honest answer and the field is omitempty, so it simply does not travel.
//
// ⚠ **Tracking, not authority.** docs/CLIENT_LIFECYCLE §4 is explicit that the ids prove nothing;
// lifetime control travels only along the inherited pipe, because a pipe cannot be guessed, copied
// out of a file, or read off another process's environment. Anything that ended a daemon because a
// caller knew this string would be a way to stop a stranger's companion from inside a web page.
//
// Distinct from InstanceID in exactly one way, and it is the whole point: this one is inherited and
// that one never is. A replacement keeps the lineage and gets a new instance, which is how a client
// tells "my daemon updated itself" from "my daemon is gone".
func OwnerID() string { return ownerVal }

// AdoptOwner joins this process to an owning lineage: the one it inherited, or a new one.
//
// Called once, from the owned-mode startup only. Re-exports the id so a successor inherits it —
// including the id this process inherited, which is what makes a chain of updates one lineage
// rather than a new owner every time.
func AdoptOwner() string {
	ownerOnce.Do(func() {
		if got := os.Getenv(OwnerEnv); got != "" {
			ownerVal = got
			return
		}
		var b [12]byte
		// Same reasoning as InstanceID: an all-zero id shared by every daemon on the machine is
		// worse than none, because identical ids make different lineages look like one.
		if _, err := rand.Read(b[:]); err != nil {
			return
		}
		ownerVal = hex.EncodeToString(b[:])
	})
	if ownerVal != "" {
		// Set even when inherited: os.Environ() is what the successor gets, and a value read but
		// not re-exported would end the lineage at the first replacement.
		//
		// Said out loud when it fails, because the damage is silent and late: the daemon serves
		// normally, and the lineage quietly ends at the NEXT self-update — a client then reads a
		// replacement as a daemon that went away. Not fatal; an owner that cannot hand its lineage
		// on is still an owner, and the pipe it holds is unaffected.
		if err := os.Setenv(OwnerEnv, ownerVal); err != nil {
			fmt.Fprintf(os.Stderr, "magi: could not hand the owning lineage to a successor (%v) — "+
				"an update restart will look like a new owner\n", err)
		}
	}
	return ownerVal
}
