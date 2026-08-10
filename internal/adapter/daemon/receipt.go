package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
)

// What proves somebody is asking about work they handed over, rather than about a session.
//
// # The hole this closes
//
// The way back across a machine used to be "tell me about session S from position N". Both of those
// are just numbers, and the door had no way to tell the companion that handed work in from anybody
// else who named the same session — so the answer to somebody else's request would come back
// looking exactly like the answer to yours.
//
// Not primarily an attack. A session id is 24 hex characters and does not travel with a sighting,
// so guessing one is not the realistic case. The realistic case is a caller holding two of these
// pairs and using the wrong one, and getting a plausible answer attributed to the wrong request —
// which is worse than an error, because nothing about it looks wrong.
//
// # A receipt is the handle, and the permission
//
// Handing work in mints one and gives it to the caller. Asking about the work means presenting it.
// There is nothing else to present and no way to ask about a session directly, so a caller can only
// read back what it actually handed over — not because it is checked against an identity, but
// because it has no way to name anything else.
//
// # It decays, like everything else runtime here
//
// Kept beside the daemon records rather than in config, for the reason membership is: this
// describes something that happened, not something that is required. The asking side gives up after
// two hours; these outlive that by a good margin so a person diagnosing a lost answer still finds
// the record, and are dropped after a day so the file cannot grow without bound.

// receiptLife is how long a receipt can be presented for.
const receiptLife = 24 * time.Hour

// Receipt is one piece of work handed to a companion on this machine.
type Receipt struct {
	// ID is what the caller presents. Random rather than derived from the session and position:
	// derived, anybody who could work out those two could mint their own.
	ID      string    `json:"id"`
	Session string    `json:"session"`
	Since   int64     `json:"since"`
	Who     string    `json:"who,omitempty"` // who handed it over, for a person reading the file
	To      string    `json:"to,omitempty"`  // and which companion took it
	At      time.Time `json:"at"`
}

func receiptFile(configDir string) string { return filepath.Join(configDir, "handoffs.json") }

// Give records a piece of handed-over work and returns the receipt for it.
func Give(configDir string, r Receipt) (Receipt, error) {
	id, err := newReceiptID()
	if err != nil {
		return Receipt{}, err
	}
	r.ID, r.At = id, time.Now()
	kept := append(liveReceipts(configDir, time.Now()), r)
	if err := writeReceipts(configDir, kept); err != nil {
		// Refused rather than handed over unrecorded. A receipt nobody can present is work that
		// went across and can never be asked about — the failure the way back exists to prevent.
		return Receipt{}, err
	}
	return r, nil
}

// Claim looks up a receipt. An unknown or expired one is simply not found: there is no distinction
// worth drawing for the caller, and drawing it would say whether a session exists.
func Claim(configDir, id string) (Receipt, bool) {
	if id == "" {
		return Receipt{}, false
	}
	for _, r := range liveReceipts(configDir, time.Now()) {
		if r.ID == id {
			return r, true
		}
	}
	return Receipt{}, false
}

func newReceiptID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("daemon: cannot mint a receipt: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// liveReceipts is what has not expired. Pruning happens on read and is written back on the next
// Give, so the file shrinks without anything having to sweep it.
func liveReceipts(configDir string, now time.Time) []Receipt {
	b, err := os.ReadFile(receiptFile(configDir))
	if err != nil {
		return nil
	}
	var all []Receipt
	if json.Unmarshal(b, &all) != nil {
		return nil // a corrupt file loses outstanding receipts, which the asking side reports
	}
	out := all[:0]
	for _, r := range all {
		if now.Sub(r.At) < receiptLife {
			out = append(out, r)
		}
	}
	return out
}

func writeReceipts(configDir string, rs []Receipt) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rs, "", "  ")
	if err != nil {
		return err
	}
	// 0600: this names sessions and who is working with whom. Nothing in it is a secret, and it is
	// nobody else's business either.
	return atomicfile.Write(receiptFile(configDir), append(b, '\n'), 0o600)
}
