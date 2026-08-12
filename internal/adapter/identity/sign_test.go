package identity

import "testing"

// A signature travels with the key that made it, and the fingerprint the admitted list compares is
// the hash of exactly those bytes.
func TestSignAndVerifyAndFingerprintAgree(t *testing.T) {
	id, err := Load(t.TempDir(), "laptop")
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("host=buildbox socket=/s/api.sock")
	sig := id.Sign(msg)
	if sig == "" {
		t.Fatal("nothing was signed")
	}
	if !VerifyBy(id.PublicKey(), sig, msg) {
		t.Error("a signature did not verify against its own key")
	}
	if VerifyBy(id.PublicKey(), sig, []byte("host=buildbox socket=/s/OTHER.sock")) {
		t.Error("a changed record still verified — the signature covers nothing")
	}
	// The key in a record and the name in the admitted list have to be the same identity, or a
	// verified record could belong to a machine nobody admitted.
	if FingerprintOfKey(id.PublicKey()) != id.Fingerprint() {
		t.Errorf("a record's key hashes to %q and the list holds %q",
			FingerprintOfKey(id.PublicKey()), id.Fingerprint())
	}
	// Another machine's key does not verify it.
	other, err := Load(t.TempDir(), "buildbox")
	if err != nil {
		t.Fatal(err)
	}
	if VerifyBy(other.PublicKey(), sig, msg) {
		t.Error("somebody else's key verified this signature")
	}
}
