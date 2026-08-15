package identity

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// redeemMu serializes Redeem's read-modify-write of the invitations file. Without it two concurrent
// joins bearing the SAME token both read it present, both match, and both write the file without it —
// admitting two different keys on a one-use invitation. The TLS server handles joins concurrently
// (a goroutine per request), so this race is reachable; the mutex makes "one use" hold.
var redeemMu sync.Mutex

// TokenFile holds the invitations this machine has open.
//
// # Why an invitation at all
//
// Admission is a fingerprint somebody carried over a channel they trust and typed in. That is
// correct and it is four steps across two machines, which is three more than anybody does twice.
// An invitation collapses it: the side that may admit mints a secret with a short life, the other
// side presents it once, and both ends record each other's key in the same exchange.
//
// The secret is the authority. It is not "somebody wants in, allow?" on a screen — it is proof
// that whoever is connecting was given something by the person who may admit them, which is the
// same reasoning kubeadm's bootstrap token uses and the same reason it expires.
//
// # What is stored is the hash
//
// A file of live invitations is a file of passwords. What is written here is a digest, so the file
// is worth nothing to anybody who reads it — the same reason a password file holds hashes, and it
// costs nothing because the token only ever has to be RECOGNISED, never reproduced.
//
// # The file is also the window
//
// An unadmitted party may complete a handshake only while an invitation is open. There is no
// separate switch and no listener to start and stop: an expired or spent line is no line, and with
// none left the door is closed again to everybody it has not already admitted.
const TokenFile = "fleet-invitations"

// tokenLife is how long an invitation stands. Long enough to walk to another desk, short enough
// that one forgotten in a terminal is not a way in tomorrow.
const tokenLife = 15 * time.Minute

// Mint writes an invitation and returns the secret, which is not stored and cannot be recovered.
func Mint(configDir, label string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(filepath.Join(configDir, TokenFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	line := fmt.Sprintf("%s %s %d\n", hashToken(token), orWord(label, "unnamed"),
		time.Now().Add(tokenLife).Unix())
	if _, err := f.WriteString(line); err != nil {
		return "", err
	}
	return token, nil
}

// Redeem checks an invitation and spends it, returning the name it was minted for.
//
// One use. An invitation that stayed valid after being taken would be a password with a
// fifteen-minute life rather than an introduction, and the difference matters on a shared terminal.
func Redeem(configDir, token string) (label string, ok bool) {
	redeemMu.Lock()
	defer redeemMu.Unlock()
	want := hashToken(token)
	lines := tokenLines(configDir)
	var kept []string
	for _, l := range lines {
		h, name, until, bad := splitToken(l)
		switch {
		case bad || time.Now().Unix() > until:
			continue // expired or unreadable: dropped, which is also how the file stays short
		case subtle.ConstantTimeCompare([]byte(h), []byte(want)) == 1 && label == "":
			// Constant time, because this is a secret being compared and the loop above tells an
			// attacker how many invitations are open either way.
			label, ok = name, true
		default:
			kept = append(kept, l)
		}
	}
	if !ok {
		return "", false
	}
	if err := writeTokens(configDir, kept); err != nil {
		return "", false
	}
	return label, true
}

// Inviting reports whether any invitation is still open, which is the only time an unadmitted
// party may finish a handshake.
func Inviting(configDir string) bool {
	for _, l := range tokenLines(configDir) {
		if _, _, until, bad := splitToken(l); !bad && time.Now().Unix() <= until {
			return true
		}
	}
	return false
}

// TokenLife is how long an invitation stands, for the message that prints one.
func TokenLife() time.Duration { return tokenLife }

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(t)))
	return base64.RawStdEncoding.EncodeToString(sum[:])
}

func tokenLines(configDir string) []string {
	f, err := os.Open(filepath.Join(configDir, TokenFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func splitToken(line string) (hash, label string, until int64, bad bool) {
	f := strings.Fields(line)
	if len(f) < 3 {
		return "", "", 0, true
	}
	n, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return "", "", 0, true
	}
	return f[0], f[1], n, false
}

func writeTokens(configDir string, lines []string) error {
	path := filepath.Join(configDir, TokenFile)
	if len(lines) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
