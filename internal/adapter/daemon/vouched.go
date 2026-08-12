package daemon

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/identity"
	"github.com/sayaya1090/magi/internal/core/cluster"
)

// What a machine will believe about companions it cannot see.
//
// # The gap this closes
//
// A gossip round is transitive: what one machine sends includes what it heard about a third. Until
// now a record arrived as plain JSON and was merged, so a peer could invent a companion on
// somebody else's host, or move an existing one to a socket of its choosing, and what came out the
// other side was indistinguishable from a sighting. Everything downstream reads those fields —
// which host to reach, which socket to open — so the invention is not cosmetic.
//
// # Signed by the machine it describes
//
// Each record now carries the public key of the machine it is about and that machine's signature
// over it, so a relay can carry a record and cannot write one. Verification is arithmetic and
// needs nobody's permission, which is why it can happen for every record of every round.
//
// # And bound to the key it first arrived with
//
// A signature alone is self-certifying: anybody can mint a key. What makes it an identity is
// remembering — the first time a companion is seen, its key is written down beside it, and a later
// record for the same companion signed by a different key is refused. That is ssh's known_hosts,
// and it has the same two properties: nothing to set up, and a loud failure exactly when somebody
// is trying to be somebody else.
//
// A key that changes for an honest reason — a machine rebuilt, a config directory moved — is
// therefore a refusal a person has to clear, by deleting the line. That is the intended cost. The
// alternative is trusting whatever arrived most recently, which is not a check at all.
const seenKeysFile = "fleet-seen"

// Vouched drops what a machine cannot show it was told by the machine it is about.
//
// Unsigned records are dropped rather than trusted-because-old: a peer that has not upgraded is
// indistinguishable, on the wire, from one that is inventing members — and the failure of dropping
// is a roster that is short, which is visible, while the failure of accepting is a roster that is
// wrong, which is not.
func Vouched(configDir string, heard []cluster.Member, logf func(string, ...any)) []cluster.Member {
	if logf == nil {
		logf = log.Printf
	}
	known := readSeen(configDir)
	changed := false
	out := make([]cluster.Member, 0, len(heard))
	for _, m := range heard {
		if m.By == "" || m.Sig == "" {
			logf("magi: dropping an unsigned record for %s on %s — a relay may carry a record and "+
				"may not write one", m.Name, m.Host)
			continue
		}
		if !identity.VerifyBy(m.By, m.Sig, cluster.Signable(m)) {
			logf("magi: dropping a record for %s on %s whose signature does not check out — it was "+
				"altered on the way, or made by somebody who does not hold that key", m.Name, m.Host)
			continue
		}
		fp := identity.FingerprintOfKey(m.By)
		key := seenKey(m)
		if was, ok := known[key]; ok && was != fp {
			logf("magi: refusing %s on %s: it was %s and now signs as %s. If that machine was "+
				"rebuilt, remove its line from %s; otherwise somebody is claiming to be it",
				m.Name, m.Host, was, fp, filepath.Join(configDir, seenKeysFile))
			continue
		} else if !ok {
			known[key] = fp
			changed = true
		}
		out = append(out, m)
	}
	if changed {
		if err := writeSeen(configDir, known); err != nil {
			logf("magi: could not remember which key a companion signs with: %v", err)
		}
	}
	return out
}

// Vouch signs this machine's own records, so the machines it talks to can check them.
//
// The key is loaded from the config directory and made there the first time, the way sshd makes a
// host key on first start. A daemon that could not make one signs nothing and its companions are
// dropped by everybody it talks to — loud, and the right way round: the failure of not signing is
// a roster that is short, and short is visible.
func Vouch(configDir string, mine []cluster.Member) []cluster.Member {
	if len(mine) == 0 {
		return mine
	}
	id, err := identity.Load(configDir, Host())
	if err != nil {
		log.Printf("magi: this machine has no key to sign its own records with (%v), so other "+
			"machines will not accept them: %v", err, err)
		return mine
	}
	pub := id.PublicKey()
	out := make([]cluster.Member, 0, len(mine))
	for _, m := range mine {
		m.By, m.Sig = pub, id.Sign(cluster.Signable(m))
		out = append(out, m)
	}
	return out
}

// seenKey names a companion the way the roster does: host and socket, which is the pair no two
// companions share. The NAME is deliberately not part of it — a companion may be renamed, and
// renaming one is not becoming a different machine.
func seenKey(m cluster.Member) string {
	return strings.ToLower(m.Host) + "\x00" + m.Socket
}

func readSeen(configDir string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(filepath.Join(configDir, seenKeysFile))
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		out[strings.ToLower(f[0])+"\x00"+f[1]] = f[2]
	}
	return out
}

// writeSeen keeps one line per companion: host, socket, fingerprint. A file a person can read and
// delete a line from, because clearing a refusal is exactly that and nothing more.
func writeSeen(configDir string, m map[string]string) error {
	var b strings.Builder
	b.WriteString("# host socket fingerprint — the key each companion first signed with.\n")
	b.WriteString("# Delete a line to accept a new one; see the note in vouched.go.\n")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		host, socket, _ := strings.Cut(k, "\x00")
		fmt.Fprintf(&b, "%s %s %s\n", host, socket, m[k])
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, seenKeysFile), []byte(b.String()), 0o600)
}
