package identity

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdmitAndRefuse(t *testing.T) {
	dir := t.TempDir()
	fp := "SHA256:AAAAbbbbCCCCddddEEEEffffGGGGhhhhIIIIjjjjKKK"
	if _, ok := AdmittedPeer(dir, fp); ok {
		t.Fatal("an empty list admitted somebody")
	}
	if already, err := Admit(dir, Peer{Fingerprint: fp, Label: "lee", Addr: "build.local:7777"}); err != nil || already {
		t.Fatalf("Admit: already=%v err=%v", already, err)
	}
	p, ok := AdmittedPeer(dir, fp)
	if !ok || p.Label != "lee" || p.Addr != "build.local:7777" {
		t.Fatalf("read back %+v (found=%v)", p, ok)
	}
	if already, _ := Admit(dir, Peer{Fingerprint: fp, Label: "lee"}); !already {
		t.Error("admitting twice wrote a second line")
	}
	// A typo'd fingerprint is refused with the command that produces a real one, rather than
	// written down and quietly never matching.
	if _, err := Admit(dir, Peer{Fingerprint: "abc123", Label: "x"}); err == nil {
		t.Error("something that is not a fingerprint was admitted")
	} else if !strings.Contains(err.Error(), "--whoami") {
		t.Errorf("the refusal does not say where to get one: %v", err)
	}
	if was, err := Refuse(dir, fp); err != nil || !was {
		t.Fatalf("Refuse: was=%v err=%v", was, err)
	}
	if _, ok := AdmittedPeer(dir, fp); ok {
		t.Error("a refused party is still admitted")
	}
}

// The list is the operator's: comments survive a removal.
func TestRefuseKeepsWhatAPersonWrote(t *testing.T) {
	dir := t.TempDir()
	fp := "SHA256:zzzz"
	body := "# the build box\n" + fp + " buildbox\n\n# (nobody else yet)\n"
	if err := os.WriteFile(filepath.Join(dir, AdmittedFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if was, err := Refuse(dir, fp); err != nil || !was {
		t.Fatalf("Refuse: was=%v err=%v", was, err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, AdmittedFile))
	if !strings.Contains(string(got), "# the build box") {
		t.Errorf("removing an entry rewrote the file:\n%s", got)
	}
}

// The handshake is where an unknown party stops.
//
// Not the door, not a queue, not a screen with a button on it: an admission list fed by whoever
// connects is a list strangers can fill, and "somebody wants in, allow?" is an attack shaped like
// a prompt. So the check runs during TLS, and a stranger learns nothing — no protocol, no version,
// no list of companions.
func TestAnUnadmittedPartyFailsTheHandshake(t *testing.T) {
	dir := t.TempDir()
	id, err := Load(dir, "laptop")
	if err != nil {
		t.Fatal(err)
	}
	verify := VerifyAdmitted(dir, nil)

	if err := verify([][]byte{id.Cert.Certificate[0]}, nil); err == nil {
		t.Fatal("an unadmitted certificate passed")
	} else {
		if !strings.Contains(err.Error(), id.Fingerprint()) {
			t.Errorf("the refusal does not carry the fingerprint to compare: %v", err)
		}
		if !strings.Contains(err.Error(), "--admit") {
			t.Errorf("the refusal does not say how to allow it: %v", err)
		}
	}
	if _, err := Admit(dir, Peer{Fingerprint: id.Fingerprint(), Label: "me"}); err != nil {
		t.Fatal(err)
	}
	if err := verify([][]byte{id.Cert.Certificate[0]}, nil); err != nil {
		t.Errorf("an admitted certificate was refused: %v", err)
	}
	// And a party that IS admitted but is not the one this connection expected — the client's
	// side of the same hook, where "somebody I know" is not "the machine I dialled".
	other := VerifyAdmitted(dir, func(p Peer) bool { return p.Label == "somebody-else" })
	if err := other([][]byte{id.Cert.Certificate[0]}, nil); err == nil {
		t.Error("a connection accepted the wrong admitted party")
	}
	// Nothing here walks a chain: there is no authority to walk to, and a self-signed leaf is all
	// that ever arrives.
	if _, perr := x509.ParseCertificate(id.Cert.Certificate[0]); perr != nil {
		t.Fatal(perr)
	}
	_ = tls.Certificate{}
}
