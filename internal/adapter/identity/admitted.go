package identity

import (
	"bufio"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AdmittedFile lists the other magis this one will speak to.
//
// # The shape is authorized_keys, on purpose
//
// One line per party: a fingerprint, a label somebody chose, and — for a party this machine also
// DIALS — where to find it. A file a person can read, edit and delete a line from, because that is
// what revocation is here and a revocation that needs a tool is one that does not happen.
//
// # Why admission is not a request
//
// Nothing in this file arrives by connecting. A party that is not on it fails the handshake, so
// the first thing an unknown caller learns is nothing at all — no protocol, no version, no list of
// companions. That is the whole reason to check identity at the TLS layer instead of inside the
// door: an admission queue fed by strangers is a queue strangers can fill, and a screen offering
// "somebody wants in, allow?" is an attack surface shaped like a button.
//
// A fingerprint gets here the way ssh host keys and kubeadm's CA hash do: a person carries it over
// a channel they trust and types it in. What that costs in ceremony is exactly what it buys.
const AdmittedFile = "fleet-admitted"

// Peer is one admitted party.
type Peer struct {
	// Fingerprint is "SHA256:…" over the public key, whole. Compared, never truncated.
	Fingerprint string
	// Label is what a person called them. It has no authority — it is not checked against anything
	// the far side says about itself — and exists so a refusal, an audit line or a prompt can name
	// somebody in words rather than in base64.
	Label string
	// Addr is host:port, present only for a party this machine reaches out to. Inbound-only
	// parties have none: knowing how to answer them is the listener's business, not this file's.
	Addr string
}

// Admitted is every party on the list.
func Admitted(configDir string) []Peer {
	f, err := os.Open(filepath.Join(configDir, AdmittedFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Peer
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		p := Peer{Fingerprint: fields[0]}
		if len(fields) > 1 {
			p.Label = fields[1]
		}
		if len(fields) > 2 {
			p.Addr = fields[2]
		}
		out = append(out, p)
	}
	return out
}

// AdmittedPeer finds a party by fingerprint. The comparison is exact and case-sensitive: base64 is
// case-sensitive, and a "helpful" fold here would make two different keys look like one.
func AdmittedPeer(configDir, fingerprint string) (Peer, bool) {
	for _, p := range Admitted(configDir) {
		if p.Fingerprint == fingerprint {
			return p, true
		}
	}
	return Peer{}, false
}

// Admit adds a party, reporting whether it was already there.
//
// Appending rather than rewriting, so a file somebody has organised and commented stays theirs.
func Admit(configDir string, p Peer) (already bool, err error) {
	if !strings.HasPrefix(p.Fingerprint, "SHA256:") {
		return false, fmt.Errorf("a fingerprint looks like SHA256:… and %q does not — "+
			"`magi --whoami` on the other machine prints theirs", p.Fingerprint)
	}
	if _, found := AdmittedPeer(configDir, p.Fingerprint); found {
		return true, nil
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return false, err
	}
	f, err := os.OpenFile(filepath.Join(configDir, AdmittedFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	line := p.Fingerprint + " " + orWord(p.Label, "unnamed")
	if p.Addr != "" {
		line += " " + p.Addr
	}
	_, err = fmt.Fprintln(f, line)
	return false, err
}

// Refuse removes a party, reporting whether it was there.
func Refuse(configDir, fingerprint string) (was bool, err error) {
	path := filepath.Join(configDir, AdmittedFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		if t != "" && !strings.HasPrefix(t, "#") && strings.Fields(t)[0] == fingerprint {
			was = true
			continue
		}
		kept = append(kept, line)
	}
	if !was {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(strings.Join(kept, "\n")), 0o600)
}

// VerifyAdmitted is the TLS hook both sides use: the certificate that arrived carries a public key,
// and the question is whether somebody put its fingerprint on the list.
//
// Written as a hook rather than as certificate verification because there is no authority to
// verify against — see the package note. The chain is deliberately not walked: a self-signed
// certificate has nothing to walk, and walking it would only reintroduce the CA this avoids.
func VerifyAdmitted(configDir string, want func(Peer) bool) func([][]byte, [][]*x509.Certificate) error {
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return fmt.Errorf("the other side presented no certificate")
		}
		leaf, err := x509.ParseCertificate(rawCerts[0])
		if err != nil {
			return fmt.Errorf("the other side's certificate did not parse: %w", err)
		}
		fp := FingerprintOf(leaf)
		p, ok := AdmittedPeer(configDir, fp)
		if !ok {
			// The fingerprint is in the error on purpose: the person reading this log is the one
			// who would admit it, and making them go and find it again is how it gets admitted
			// without being compared.
			return fmt.Errorf("%s is not admitted here — compare it with what they told you, then "+
				"`magi --admit %s <name>`", fp, fp)
		}
		if want != nil && !want(p) {
			return fmt.Errorf("%s is admitted but is not the party this connection expected", fp)
		}
		return nil
	}
}

// VerifyAdmittedOrInviting is the listener's hook while invitations can be open.
//
// An unadmitted party may finish a handshake only while one is, and then only to reach the one
// route that demands the secret — see the fleet door's join handler, which re-checks admission for
// everything else. Without the window, a stranger's handshake fails and it learns nothing at all;
// with it, the exposure is one endpoint that answers "no" to anybody without an invitation.
func VerifyAdmittedOrInviting(configDir string) func([][]byte, [][]*x509.Certificate) error {
	strict := VerifyAdmitted(configDir, nil)
	return func(raw [][]byte, chains [][]*x509.Certificate) error {
		if err := strict(raw, chains); err == nil {
			return nil
		} else if !Inviting(configDir) {
			return err
		}
		return nil
	}
}

// PeerOf names the party on the other end of a finished handshake, if this machine admitted it.
func PeerOf(configDir string, certs []*x509.Certificate) (Peer, bool) {
	if len(certs) == 0 {
		return Peer{}, false
	}
	return AdmittedPeer(configDir, FingerprintOf(certs[0]))
}

func orWord(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return strings.Fields(strings.TrimSpace(s))[0]
}
