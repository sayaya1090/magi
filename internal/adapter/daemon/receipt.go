package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// What proves somebody is asking about work they handed over, rather than about a session.
//
// # The hole this closes
//
// The way back used to be "tell me about session S from position N". Both of those are just
// numbers, and the door had no way to tell the companion that handed work in from anybody else who
// named the same session — so the answer to somebody else's request came back looking exactly like
// the answer to yours.
//
// Not primarily an attack. The realistic case is a caller holding two of these and using the wrong
// one, and getting a plausible answer attributed to the wrong request — which is worse than an
// error, because nothing about it looks wrong.
//
// # A receipt is the handle, and the permission
//
// Handing work in mints one and gives it to the caller. Asking about the work means presenting it.
// There is nothing else to present and no way to ask about a position directly, so a caller can only
// read back what it actually handed over — not because it is checked against an identity, but
// because it has no way to name anything else.
//
// # Held by the daemon that minted it, and only in memory
//
// It used to be a file, because the thing that answered was a fresh process each time and had
// nowhere else to look. Now the daemon answers, and the daemon is still running — a file would be
// state written down so that the process which already has it can read it back.
//
// So a restart loses the outstanding ones. That costs nothing real: a daemon that restarted did not
// finish the turn it was in, so the answer those receipts pointed at was never written. Keeping them
// across a restart would have preserved the ability to ask a question whose answer is permanently
// "not yet".
//
// They still decay, because a daemon can be up for weeks and a caller can walk away.

// receiptLife is how long a receipt can be presented for. The asking side gives up long before
// this; the margin is so a person diagnosing a lost answer can still present one by hand.
const receiptLife = 24 * time.Hour

// Receipts is what one daemon has taken from other companions.
//
// Safe for concurrent use: the socket serves each connection in its own goroutine, so two machines
// handing work in at the same moment is ordinary rather than exotic.
type Receipts struct {
	mu   sync.Mutex
	kept map[string]receipt
}

type receipt struct {
	// session is where the work went. One per asker, not one per daemon: a request must not land
	// in the conversation somebody is attached to, and two askers must not read each other's.
	session string
	// since is where that session's log stood when the work arrived. The answer is the first turn
	// that finishes past it — which is why the position is taken before the work goes in, and why
	// it is kept here instead of being handed back to the caller to send again.
	since int64
	at    time.Time
}

func NewReceipts() *Receipts { return &Receipts{kept: map[string]receipt{}} }

// Give records a piece of handed-over work and returns the receipt for it.
func (r *Receipts) Give(sid string, since int64) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Refused rather than handed over unrecorded. A receipt nobody can present is work that
		// crossed and can never be asked about — the failure the way back exists to prevent.
		return "", fmt.Errorf("daemon: cannot mint a receipt: %w", err)
	}
	// Random rather than derived from the position: derived, anybody who could work out that
	// number could mint their own.
	id := hex.EncodeToString(b)
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	for old, rec := range r.kept {
		if now.Sub(rec.at) >= receiptLife {
			delete(r.kept, old)
		}
	}
	r.kept[id] = receipt{session: sid, since: since, at: now}
	return id, nil
}

// Where looks up the session a receipt stands for and the position its log stood at. An unknown or
// expired one is simply not found: there is no distinction worth drawing for the caller, and
// drawing it would say whether a piece of work exists.
func (r *Receipts) Where(id string) (sid string, since int64, ok bool) {
	if id == "" {
		return "", 0, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, found := r.kept[id]
	if !found || time.Since(rec.at) >= receiptLife {
		return "", 0, false
	}
	return rec.session, rec.since, true
}
