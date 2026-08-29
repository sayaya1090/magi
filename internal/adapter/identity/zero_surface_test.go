package identity

import (
	"strings"
	"testing"
)

// TokenLife is how long an invitation stands — printed beside every token, so it must be a real
// duration and not a zero somebody forgot.
func TestTokenLifeIsAStandingWindow(t *testing.T) {
	if TokenLife() <= 0 {
		t.Fatal("an invitation that stands for no time invites nobody")
	}
}

// With nothing admitted and no invitation open, a stranger's handshake fails — and PeerOf names
// nobody for an empty chain.
func TestClosedDoorRefusesStrangers(t *testing.T) {
	dir := t.TempDir()
	if Inviting(dir) {
		t.Fatal("an empty directory holds no open invitation")
	}
	verify := VerifyAdmittedOrInviting(dir)
	if err := verify(nil, nil); err == nil {
		t.Fatal("no admission and no invitation: the handshake must fail")
	} else if strings.TrimSpace(err.Error()) == "" {
		t.Fatal("the refusal should say something a log can carry")
	}
	if _, ok := PeerOf(dir, nil); ok {
		t.Fatal("an empty chain names nobody")
	}
}
