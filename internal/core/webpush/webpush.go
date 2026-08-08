// Package webpush sends a notification to a browser that is not looking at the page.
//
// # Why this exists
//
// The console has exactly one way to tell somebody that a companion is blocked: the tab title,
// which reads "(2) magi · companions". That reaches a person who has the tab open on the screen in
// front of them, and nobody else. A supervisor who stepped away — which is the whole reason the
// fleet page exists — finds out when they come back, and the agent has been waiting for an answer
// the entire time.
//
// # Why the standard one and not a webhook
//
// A webhook to a notification service is cheaper: one outbound POST and no cryptography. It also
// puts a third party's account between a person and their own machines, and it needs an app on the
// phone. Web Push needs neither. The page is already installable — there is a manifest and an icon
// — so a phone that has added it to its home screen can be woken by the console it already has.
//
// # What is encrypted, and from whom
//
// The push service (Google's, Mozilla's, Apple's) relays the message and MUST NOT be able to read
// it. So the payload is encrypted to a key the browser generated and never sent anywhere but here,
// and the request is signed with a key this console generated and never sends at all. The push
// service can see that a message went to a subscription, and its size. That is the whole of what it
// learns.
//
// # Why it is implemented rather than imported
//
// The two specs are small — RFC 8291 is one ECDH, two HKDF expansions and one AES-GCM record; RFC
// 8292 is a JWT with one signature — and every primitive is in the standard library as of Go 1.24
// (crypto/ecdh, crypto/hkdf). A dependency here would be a supply chain for eighty lines. The
// arithmetic is pinned to the RFC's own worked example in webpush_test.go, which is the only way to
// know an implementation of this interoperates: a round trip against itself passes just as happily
// with the wrong info string.
package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// b64 is base64url without padding, which is what both specs use everywhere: subscription keys, the
// JWT's three parts, and the VAPID header's k parameter.
var b64 = base64.RawURLEncoding

// Subscription is what a browser hands the page when somebody turns notifications on. All three
// fields come from PushSubscription.toJSON() and none of them are ours to invent.
type Subscription struct {
	// Endpoint is the push service's URL for this browser. It identifies the subscription; anyone
	// holding it can send to it, which is why it is stored like a credential and never rendered.
	Endpoint string `json:"endpoint"`
	// P256dh is the browser's public key, uncompressed X9.62, base64url.
	P256dh string `json:"p256dh"`
	// Auth is the 16-octet shared secret the browser generated. It is not a key; it is salt for the
	// key derivation, and its job is to stop anyone who learns the endpoint and the public key from
	// reading messages.
	Auth string `json:"auth"`
}

// Keys is the application server's identity: one P-256 pair, generated once and kept.
//
// Not per message and not per subscription. A browser records which key subscribed it and refuses
// a message signed by a different one, so rotating this key silently breaks every subscription
// already handed out — the reason it lives on disk rather than in memory.
type Keys struct {
	Private *ecdsa.PrivateKey
	// Subject is the mailto: or https: the push service can complain to. RFC 8292 requires it and
	// some services reject a JWT without one; nothing reads it in normal operation.
	Subject string
}

// NewKeys makes a fresh application-server identity.
func NewKeys(subject string) (*Keys, error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return &Keys{Private: k, Subject: subject}, nil
}

// PublicKey is the key a browser subscribes with, base64url. The page passes it to
// PushManager.subscribe as applicationServerKey.
func (k *Keys) PublicKey() string {
	return b64.EncodeToString(elliptic.Marshal(elliptic.P256(), k.Private.X, k.Private.Y)) //nolint:staticcheck // ecdh has no marshal for an ecdsa key
}

// PrivateSeed is the private scalar, base64url, for writing to disk.
func (k *Keys) PrivateSeed() string {
	return b64.EncodeToString(k.Private.D.FillBytes(make([]byte, 32)))
}

// KeysFromSeed rebuilds an identity from what PrivateSeed wrote.
func KeysFromSeed(seed, subject string) (*Keys, error) {
	d, err := b64.DecodeString(seed)
	if err != nil {
		return nil, fmt.Errorf("push key is not base64url: %w", err)
	}
	if len(d) != 32 {
		return nil, fmt.Errorf("push key is %d bytes; a P-256 scalar is 32", len(d))
	}
	k := new(ecdsa.PrivateKey)
	k.Curve = elliptic.P256()
	k.D = new(big.Int).SetBytes(d)
	k.X, k.Y = elliptic.P256().ScalarBaseMult(d)
	return &Keys{Private: k, Subject: subject}, nil
}

// Encrypt produces the aes128gcm body of one push message, per RFC 8291.
//
// as is the ephemeral key pair for THIS message and salt is 16 fresh octets; both are arguments
// rather than generated inside so the RFC's worked example can be reproduced exactly. Send passes
// fresh ones, which is what the spec requires of a real message: a repeated pair leaks the
// plaintext of both.
func Encrypt(sub Subscription, plaintext []byte, as *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	uaRaw, err := b64.DecodeString(sub.P256dh)
	if err != nil {
		return nil, fmt.Errorf("subscription key is not base64url: %w", err)
	}
	ua, err := ecdh.P256().NewPublicKey(uaRaw)
	if err != nil {
		return nil, fmt.Errorf("subscription key is not a P-256 point: %w", err)
	}
	auth, err := b64.DecodeString(sub.Auth)
	if err != nil {
		return nil, fmt.Errorf("subscription auth is not base64url: %w", err)
	}
	if len(salt) != 16 {
		return nil, fmt.Errorf("salt is %d bytes; the record header has room for 16", len(salt))
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, fmt.Errorf("the two keys do not share a curve: %w", err)
	}

	// The auth secret is the SALT of the first extraction and the shared secret is the input key
	// material — the opposite way round from the second extraction below, and the single easiest
	// thing to get backwards here. Getting it backwards produces a perfectly self-consistent
	// implementation that no browser can read.
	asRaw := as.PublicKey().Bytes()
	keyInfo := append([]byte("WebPush: info\x00"), uaRaw...)
	keyInfo = append(keyInfo, asRaw...)
	ikm, err := hkdf.Key(sha256.New, shared, auth, string(keyInfo), 32)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}

	// One record, so the nonce is used as derived: RFC 8188 XORs it with the record sequence
	// number, and this record's is zero.
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// 0x02 is the delimiter that marks the last record. A receiver that finds anything else here
	// must discard the message, so it is not padding and not optional.
	ct := gcm.Seal(nil, nonce, append(append([]byte{}, plaintext...), 0x02), nil)

	// salt | record size | keyid length | keyid | ciphertext, which is RFC 8188's header followed
	// by the one record. The record size only has to exceed this record, since there is one.
	body := make([]byte, 0, 16+4+1+len(asRaw)+len(ct))
	body = append(body, salt...)
	body = binary.BigEndian.AppendUint32(body, 4096)
	body = append(body, byte(len(asRaw)))
	body = append(body, asRaw...)
	return append(body, ct...), nil
}

// authHeader is the VAPID token for one endpoint, per RFC 8292.
//
// The audience is the endpoint's ORIGIN, not the endpoint: a token scoped to the full URL would
// still work but would say more about which subscription is being written to than it needs to.
func (k *Keys) authHeader(endpoint string, now time.Time) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"aud": u.Scheme + "://" + u.Host,
		// Twelve hours. The spec's ceiling is 24 and a shorter life means a token found in a log is
		// worth less; shorter still would start to depend on the clocks agreeing.
		"exp": now.Add(12 * time.Hour).Unix(),
		"sub": k.Subject,
	})
	if err != nil {
		return "", err
	}
	signing := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`)) + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, k.Private, sum[:])
	if err != nil {
		return "", err
	}
	// Fixed-width r||s, which is what JWS ES256 is. DER — what ecdsa.SignASN1 returns — is a
	// different encoding of the same numbers and every push service rejects it.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + k.PublicKey(), nil
}

// Gone reports that a subscription is dead and should be forgotten.
//
// A browser that was uninstalled, cleared, or had notifications revoked leaves the push service
// answering 404 or 410 forever. Told apart from a transient failure because the two want opposite
// responses: retry one, delete the other. Storing dead subscriptions indefinitely is how a console
// ends up spending a request per stale phone on every alert.
var Gone = errors.New("the subscription no longer exists")

// Send encrypts one message and posts it.
//
// ttl is how long the push service should hold the message for a phone that is offline. Short by
// intent for this use: "somebody is waiting for you" is worth nothing four hours later, and a
// notification that arrives after the question was answered trains a person to ignore them.
func (k *Keys) Send(client *http.Client, sub Subscription, payload []byte, ttl time.Duration) error {
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return err
	}
	body, err := Encrypt(sub, payload, as, salt)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	auth, err := k.authHeader(sub.Endpoint, time.Now())
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", fmt.Sprint(int(ttl.Seconds())))
	// Urgent, because the whole point is a phone that is asleep. The alternative is a message the
	// service is free to hold until the device wakes for something else.
	req.Header.Set("Urgency", "high")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Read to the end whatever the status: an undrained body keeps the connection from being
	// reused, and when the service is refusing, what it wrote is the only account of why. A refusal
	// with no reason has cost this tree hours before.
	said, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return Gone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push service answered %s: %s",
			strings.TrimSpace(resp.Status), strings.TrimSpace(string(said)))
	case readErr != nil:
		return fmt.Errorf("push service accepted the message but the reply was cut off: %w", readErr)
	}
	return nil
}
