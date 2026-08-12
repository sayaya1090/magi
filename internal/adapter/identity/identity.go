// Package identity is what one magi is, to another magi.
//
// # Why a key and not a name
//
// Everything crossing between machines has been self-declared: a handover carries the label the
// asker chose, a member record carries the name its daemon wrote. Between one person's own
// machines that is fine. Between people it is decoration — anybody who can reach the door can say
// any name — so the things built on top of a name (who asked for this, whose companion is that,
// what may this caller do) are worth exactly what the name is worth.
//
// A key pair fixes that and nothing else: it makes "the same party as last time" checkable. It
// does not say who the party IS. That is a label a person attaches once, out of band, which is
// also what ssh does with known_hosts and what kubeadm does with --discovery-token-ca-cert-hash.
//
// # Why no certificate authority
//
// A CA is the other way to do this and it means running one: issuing, renewing, revoking,
// distributing a root, and keeping the signing key somewhere better than the machines it signs
// for. The two-party case does not need it — ssh has never needed it — and the cost lands on the
// person setting up two laptops.
//
// So the certificate here is self-signed and carries no authority at all. It is an envelope,
// because TLS wants one; what is compared is the PUBLIC KEY inside it. A renewed certificate over
// the same key keeps the same identity, which is the property a CA-less scheme needs to have.
//
// # Bound to the host it was made on
//
// A config directory can be shared — this tree already warns that two machines can point at one,
// and MAGI_CONFIG_DIR exists partly for that. Two daemons holding one key are one identity, so
// admitting either admits both. The key records where it was made and is regenerated when that
// stops matching, which turns a silent merge of two identities into a new one somebody has to
// admit.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// File names under the config directory. Three files rather than one so the private half can be
// 0600 and the rest can be read by anything that needs to show a fingerprint.
const (
	keyFile  = "fleet-key.pem"
	certFile = "fleet-cert.pem"
	hostFile = "fleet-host"
)

// Identity is this magi's key pair and the certificate that carries it.
type Identity struct {
	Cert tls.Certificate
	// Host is the machine the key was made on, kept so a shared config directory is noticed.
	Host string
}

// Load returns this config directory's identity, creating one the first time.
//
// Regenerated when the recorded host no longer matches: see the note on host binding above. The
// old key is not kept — an identity nobody can present is not an identity, and leaving it would
// invite a "which key was that" question with no good answer.
func Load(configDir, host string) (*Identity, error) {
	if configDir == "" {
		return nil, fmt.Errorf("identity: no config directory")
	}
	made := strings.TrimSpace(readFile(filepath.Join(configDir, hostFile)))
	if made != "" && host != "" && !strings.EqualFold(made, host) {
		if err := clear(configDir); err != nil {
			return nil, err
		}
	}
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(configDir, certFile), filepath.Join(configDir, keyFile))
	if err == nil {
		return withLeaf(&Identity{Cert: cert, Host: made})
	}
	return create(configDir, host)
}

// Fingerprint names this identity in the one form that is safe to compare by eye: the whole hash
// of the public key, in the shape ssh prints.
//
// The public key and not the certificate, so renewing the certificate keeps the identity. Whole,
// because a truncated fingerprint is a prefix somebody can grind — and because the entire point of
// printing it is that two people compare it over a channel neither of them controls.
func (i *Identity) Fingerprint() string { return FingerprintOf(i.Cert.Leaf) }

// FingerprintOf is Fingerprint for somebody else's certificate — the far side of a handshake.
func FingerprintOf(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func create(configDir, host string) (*Identity, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	// A hundred years. The expiry of a self-signed certificate says nothing — there is no
	// authority behind it to have meant anything by the date — and a key that stops working on a
	// Tuesday in three years is an outage with no security benefit. Rotation here is a person
	// making a new key and being admitted again, which is a decision and not a clock.
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "magi " + host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(100, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, err
	}
	// 0600 on the private half, and written before the certificate: a certificate with no key
	// beside it is loaded, fails, and regenerates, which is recoverable. The other order leaves a
	// key nothing points at.
	if err := os.WriteFile(filepath.Join(configDir, keyFile),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(configDir, certFile),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(configDir, hostFile), []byte(host+"\n"), 0o644); err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(
		filepath.Join(configDir, certFile), filepath.Join(configDir, keyFile))
	if err != nil {
		return nil, err
	}
	return withLeaf(&Identity{Cert: cert, Host: host})
}

// withLeaf parses the certificate once, so every fingerprint after this is a hash and not a parse.
func withLeaf(id *Identity) (*Identity, error) {
	if id.Cert.Leaf == nil {
		if len(id.Cert.Certificate) == 0 {
			return nil, fmt.Errorf("identity: the certificate is empty")
		}
		leaf, err := x509.ParseCertificate(id.Cert.Certificate[0])
		if err != nil {
			return nil, err
		}
		id.Cert.Leaf = leaf
	}
	return id, nil
}

func clear(configDir string) error {
	for _, f := range []string{keyFile, certFile, hostFile} {
		if err := os.Remove(filepath.Join(configDir, f)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return big.NewInt(1)
	}
	return n
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// Sign vouches for bytes with this machine's key.
//
// Ed25519, so a signature is 64 bytes and verifying one is a hash and a curve operation — cheap
// enough to do for every record in a gossip round without thinking about it.
func (i *Identity) Sign(msg []byte) string {
	key, ok := i.Cert.PrivateKey.(ed25519.PrivateKey)
	if !ok {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(ed25519.Sign(key, msg))
}

// PublicKey is what a record carries so anybody can check its signature: the SPKI, base64, which
// is the same bytes Fingerprint hashes. Carrying the key rather than the fingerprint is not a
// weakening — a fingerprint cannot verify anything — and what the admitted list compares is still
// the hash of exactly these bytes.
func (i *Identity) PublicKey() string {
	if i.Cert.Leaf == nil {
		return ""
	}
	return base64.RawStdEncoding.EncodeToString(i.Cert.Leaf.RawSubjectPublicKeyInfo)
}

// VerifyBy checks a signature against a public key carried in a record.
//
// Self-certifying on its own: anybody can mint a key and sign with it, so this proves the record
// was not TAMPERED with and came from whoever holds that key — not that the key is anybody in
// particular. The second half is the caller's, by remembering which key a member had the first
// time it was seen; see the fleet-seen store in the daemon package.
func VerifyBy(publicKey, sig string, msg []byte) bool {
	spki, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		return false
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return false
	}
	pub, err := x509.ParsePKIXPublicKey(spki)
	if err != nil {
		return false
	}
	ed, ok := pub.(ed25519.PublicKey)
	if !ok {
		return false
	}
	return ed25519.Verify(ed, msg, raw)
}

// FingerprintOfKey is the name the admitted list uses, computed from the key a record carries.
func FingerprintOfKey(publicKey string) string {
	spki, err := base64.RawStdEncoding.DecodeString(strings.TrimSpace(publicKey))
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(spki)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}
