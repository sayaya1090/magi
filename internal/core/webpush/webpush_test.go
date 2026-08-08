package webpush

import (
	"crypto/ecdh"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The RFC's own worked example, byte for byte.
//
// This is the only test here that proves anything about interoperating. Encryption of this shape
// round-trips against itself with the info strings swapped, the salt and the IKM the wrong way
// round, or the delimiter omitted — every one of those produces a self-consistent implementation
// that no browser can read, and a decrypt-what-I-encrypted test passes all of them. So the inputs
// are the RFC's and the output has to be the RFC's.
//
// Values: RFC 8291 §5 and Appendix A.
func TestTheRFCsOwnExample(t *testing.T) {
	const (
		auth      = "BTBZMqHH6r4Tts7J_aSIgg"
		uaPublic  = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
		asPrivate = "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
		salt      = "DGv6ra1nlYgDCS1FRnbzlw"
		plaintext = "When I grow up, I want to be a watermelon"
		want      = "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
			"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
			"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	)
	seed, err := b64.DecodeString(asPrivate)
	if err != nil {
		t.Fatal(err)
	}
	as, err := ecdh.P256().NewPrivateKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	saltB, err := b64.DecodeString(salt)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Encrypt(Subscription{P256dh: uaPublic, Auth: auth}, []byte(plaintext), as, saltB)
	if err != nil {
		t.Fatalf("the example did not encrypt: %v", err)
	}
	if g := b64.EncodeToString(got); g != want {
		// Split so a failure says WHERE it diverged: a wrong header is a layout mistake and a wrong
		// ciphertext with a right header is the key derivation.
		const headerLen = 86
		gh, wh := g[:min(headerLen, len(g))], want[:headerLen]
		if gh != wh {
			t.Errorf("header:\n got %s\nwant %s", gh, wh)
		}
		if len(g) > headerLen && g[headerLen:] != want[headerLen:] {
			t.Errorf("ciphertext:\n got %s\nwant %s", g[headerLen:], want[headerLen:])
		}
		if gh == wh && len(g) > headerLen && g[headerLen:] == want[headerLen:] {
			t.Errorf("body differs in length only: got %d, want %d", len(g), len(want))
		}
	}
}

// A subscription this console cannot use is refused where it arrives, not where it is sent.
//
// These three all come off the wire from a browser and any of them can be malformed by a proxy, a
// truncated store, or a hand-edited file. Left to fail inside the ECDH they surface as "invalid
// point", which says nothing about which field was wrong.
func TestAMalformedSubscriptionSaysWhichPartIsWrong(t *testing.T) {
	as, err := ecdh.P256().GenerateKey(strings.NewReader(strings.Repeat("k", 200)))
	if err != nil {
		t.Fatal(err)
	}
	good := Subscription{
		P256dh: "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:   "BTBZMqHH6r4Tts7J_aSIgg",
	}
	salt := make([]byte, 16)
	for _, c := range []struct {
		name, want string
		sub        Subscription
	}{
		{"key is not base64", "base64url", Subscription{P256dh: "not base64!", Auth: good.Auth}},
		{"key is not a point", "P-256", Subscription{P256dh: "AAAA", Auth: good.Auth}},
		{"auth is not base64", "base64url", Subscription{P256dh: good.P256dh, Auth: "not base64!"}},
	} {
		_, err := Encrypt(c.sub, []byte("x"), as, salt)
		if err == nil {
			t.Errorf("%s: encrypted anyway", c.name)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %q does not say %q", c.name, err, c.want)
		}
	}
	// And the salt, which is ours rather than the browser's: the header reserves exactly 16 octets
	// for it, so a shorter one would silently shift every field after it.
	if _, err := Encrypt(good, []byte("x"), as, []byte("short")); err == nil {
		t.Error("a five-byte salt was accepted; the header has room for sixteen")
	}
}

// The signature is r||s, and the claims say who and how long.
//
// JWS ES256 is a fixed 64 bytes. ecdsa.SignASN1 returns DER — the same two numbers, a different
// encoding, accepted by nothing — and the two are easy to reach for interchangeably.
func TestTheTokenIsSignedTheWayJWSSaysAndScopedToTheOrigin(t *testing.T) {
	k, err := NewKeys("mailto:nobody@example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	h, err := k.authHeader("https://push.example.net/send/abc?token=secret", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "vapid t=") || !strings.Contains(h, ", k=") {
		t.Fatalf("not a vapid header: %q", h)
	}
	tok := strings.TrimPrefix(strings.SplitN(h, ", k=", 2)[0], "vapid t=")
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("a JWT has three parts; this has %d", len(parts))
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 64 {
		t.Errorf("the signature is %d bytes; ES256 is 64 (DER would be about 70)", len(sig))
	}
	raw, err := b64.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatal(err)
	}
	// The origin, not the endpoint: the path names one subscription and the token does not need to.
	if claims.Aud != "https://push.example.net" {
		t.Errorf("audience %q — it must be the origin, and it must not carry the path", claims.Aud)
	}
	if claims.Sub != "mailto:nobody@example.com" {
		t.Errorf("subject %q", claims.Sub)
	}
	if d := time.Unix(claims.Exp, 0).Sub(now); d <= 0 || d > 24*time.Hour {
		t.Errorf("the token lives %v; the spec's ceiling is 24h and an expired one is refused", d)
	}
}

// A subscription that is gone is told apart from a push service having a bad minute.
//
// They want opposite handling — forget one, retry the other — and a caller that cannot tell either
// deletes live subscriptions on a blip or keeps dead ones forever.
func TestADeadSubscriptionIsDistinguishedFromAFailure(t *testing.T) {
	k, err := NewKeys("mailto:nobody@example.com")
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscription{
		P256dh: "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4",
		Auth:   "BTBZMqHH6r4Tts7J_aSIgg",
	}
	for _, c := range []struct {
		code int
		gone bool
	}{{404, true}, {410, true}, {201, false}, {500, false}, {429, false}} {
		var gotAuth, gotEnc, gotTTL string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth, gotEnc, gotTTL = r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.Header.Get("TTL")
			w.WriteHeader(c.code)
		}))
		sub.Endpoint = srv.URL + "/send/x"
		err := k.Send(srv.Client(), sub, []byte("hello"), 5*time.Minute)
		srv.Close()
		switch {
		case c.gone && err != Gone:
			t.Errorf("%d: %v — a subscription that is gone must say so", c.code, err)
		case !c.gone && c.code < 300 && err != nil:
			t.Errorf("%d: %v", c.code, err)
		case !c.gone && c.code >= 300 && (err == nil || err == Gone):
			t.Errorf("%d: %v — a bad minute is not a dead subscription", c.code, err)
		}
		if c.code == 201 {
			if !strings.HasPrefix(gotAuth, "vapid t=") {
				t.Errorf("the request went unsigned: %q", gotAuth)
			}
			if gotEnc != "aes128gcm" {
				t.Errorf("content encoding %q", gotEnc)
			}
			if gotTTL != "300" {
				t.Errorf("TTL %q; the caller asked for five minutes", gotTTL)
			}
		}
	}
}

// A key survives being written down and read back.
func TestAKeyRoundTripsThroughItsSeed(t *testing.T) {
	k, err := NewKeys("mailto:a@b.c")
	if err != nil {
		t.Fatal(err)
	}
	back, err := KeysFromSeed(k.PrivateSeed(), "mailto:a@b.c")
	if err != nil {
		t.Fatal(err)
	}
	// The public key is what a browser recorded when it subscribed. A restart that changes it
	// breaks every subscription already handed out, silently — the browser simply drops messages.
	if back.PublicKey() != k.PublicKey() {
		t.Errorf("the identity changed across a restart:\n was %s\nnow %s", k.PublicKey(), back.PublicKey())
	}
	if _, err := KeysFromSeed("nope!", ""); err == nil {
		t.Error("a seed that is not base64url was accepted")
	}
	if _, err := KeysFromSeed(b64.EncodeToString([]byte("short")), ""); err == nil {
		t.Error("a five-byte scalar was accepted as P-256")
	}
}
