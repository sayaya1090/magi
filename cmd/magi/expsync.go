// exp-sync — the fleet door method that replicates a team's shared experience between machines.
//
// The team tier is a directory every companion ON ONE MACHINE already shares; across machines it
// shared nothing, so a page written on the laptop never reached the desk. The unit of replication
// is an immutable, content-addressed file (a wiki revision, a memory), so syncing is a SET UNION:
// each side tells the other what it has, each sends what the other lacks, and nothing is ever
// overwritten — the store derives "current" deterministically from the set (see the wiki store),
// which is what lets two replicas converge with no coordinator, no lock, and no conflict at the
// transport. Deletions do not exist on the wire; retirement is a stale revision, which is itself
// a file that replicates.
//
// It is handled at the DOOR, not relayed to a companion socket: team knowledge belongs to the
// machine (every companion of that team here reads the same directory), and a companion daemon
// has nothing to add but a hop. The ssh door does not carry it — that door exists for a person's
// narrow errands; machine-to-machine replication rides the TLS fleet door, where both ends hold
// each other's pinned key.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/adapter/identity"
	"github.com/sayaya1090/magi/internal/atomicfile"
)

// expSyncAsk is one exchange: "for this team, here is what I hold, and here are the files you
// said you wanted". Both directions of the union ride the same shape.
type expSyncAsk struct {
	Method string            `json:"method"`
	Team   string            `json:"team"`
	Have   []string          `json:"have,omitempty"`
	Files  map[string]string `json:"files,omitempty"`
}

// expSyncReply answers with the other half: what the caller holds that this machine lacks (Want),
// and what this machine holds that the caller lacks (Files, size-capped — the rest goes next
// round; the union converges, it does not have to finish in one).
type expSyncReply struct {
	OK    bool              `json:"ok"`
	Err   string            `json:"error,omitempty"`
	Want  []string          `json:"want,omitempty"`
	Files map[string]string `json:"files,omitempty"`
}

const (
	expSyncBytesCap = 2 << 20         // per-reply file payload bound; the request body cap is 4 MB
	expSyncWantCap  = 400             // per-reply want-list bound
	expSyncEvery    = 5 * time.Minute // anti-entropy cadence; a sync is two tiny digests when quiet
)

// expSyncPathOK admits exactly the two replicable shapes — a wiki revision and a memory — and
// nothing else. Everything is checked structurally (segments, no dot-names, .md only) rather than
// cleaned: a path that needs cleaning is a path somebody constructed, and the door does not write
// where constructed paths point.
func expSyncPathOK(p string) bool {
	seg := strings.Split(p, "/")
	clean := func(s string) bool {
		return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `\:`) && s[0] != '.'
	}
	switch {
	case len(seg) == 4 && seg[0] == "wiki" && seg[1] == "revisions":
		return clean(seg[2]) && clean(seg[3]) && strings.HasSuffix(seg[3], ".md")
	case len(seg) == 2 && seg[0] == "memories":
		return clean(seg[1]) && strings.HasSuffix(seg[1], ".md")
	}
	return false
}

// expSyncInventory lists the replicable files under a team experience dir, as wire paths.
func expSyncInventory(dir string) []string {
	var out []string
	if revRoot := filepath.Join(dir, "wiki", "revisions"); true {
		if pages, err := os.ReadDir(revRoot); err == nil {
			for _, pg := range pages {
				if !pg.IsDir() {
					continue
				}
				if revs, err := os.ReadDir(filepath.Join(revRoot, pg.Name())); err == nil {
					for _, r := range revs {
						p := "wiki/revisions/" + pg.Name() + "/" + r.Name()
						if !r.IsDir() && expSyncPathOK(p) {
							out = append(out, p)
						}
					}
				}
			}
		}
	}
	if mems, err := os.ReadDir(filepath.Join(dir, "memories")); err == nil {
		for _, m := range mems {
			p := "memories/" + m.Name()
			if !m.IsDir() && expSyncPathOK(p) {
				out = append(out, p)
			}
		}
	}
	return out
}

// expSyncAbsorb writes incoming files into the team dir. Immutable set semantics: a path that
// already exists is left exactly as it is — the names are content-addressed, so "already have it"
// and "already have this content" are the same statement, and an overwrite could only ever
// replace a fact with an impostor.
//
// "Content-addressed" is VERIFIED, not assumed: the hash in the filename must match the body, or
// the file is dropped. Without the check it was a one-honest-peer assumption — an impostor body
// under a plausible name would land once and, by the no-overwrite rule, be permanent.
func expSyncAbsorb(dir string, files map[string]string) int {
	n := 0
	for p, content := range files {
		if !expSyncPathOK(p) || content == "" || !expSyncContentOK(p, content) {
			continue
		}
		abs := filepath.Join(dir, filepath.FromSlash(p))
		if _, err := os.Stat(abs); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			continue
		}
		if atomicfile.Write(abs, []byte(content), 0o644) == nil {
			n++
		}
	}
	return n
}

// expSyncContentOK checks the filename's content hash against the body, for the shapes that carry
// one: a wiki revision's name ends "-<hash>.md" over the revision BODY (the text after the
// frontmatter), and a memory is "mem-<hash>.md" over its whole text. An unrecognized name shape
// already failed expSyncPathOK.
func expSyncContentOK(p, content string) bool {
	base := strings.TrimSuffix(p[strings.LastIndexByte(p, '/')+1:], ".md")
	hashed := content
	if strings.HasPrefix(p, "wiki/revisions/") {
		// The body is what the writer hashed (see the store's wikiWrite); frontmatter carries
		// per-write metadata and is excluded there, so it is excluded here.
		if _, tail, ok := strings.Cut(content, "\n---\n"); ok {
			hashed = strings.TrimSpace(tail)
		} else {
			return false
		}
	}
	i := strings.LastIndexByte(base, '-')
	if i < 0 {
		return false
	}
	return base[i+1:] == expgit.ContentID(hashed)
}

// expSyncDiff answers both halves of the union for one exchange.
func expSyncDiff(dir string, theirHave []string) (want []string, files map[string]string) {
	mine := map[string]bool{}
	for _, p := range expSyncInventory(dir) {
		mine[p] = true
	}
	theirs := map[string]bool{}
	for _, p := range theirHave {
		theirs[p] = true
		if !mine[p] && expSyncPathOK(p) && len(want) < expSyncWantCap {
			want = append(want, p)
		}
	}
	files = map[string]string{}
	total := 0
	for p := range mine {
		if theirs[p] {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			continue
		}
		if total += len(b); total > expSyncBytesCap {
			break // the rest goes next round; the union converges, it need not finish in one
		}
		files[p] = string(b)
	}
	return want, files
}

// expSyncHandle serves one exchange at the door. The team must already have a footprint on this
// machine — a machine no companion of that team runs on holds no replica, so a stranger with a
// valid certificate cannot seed arbitrary directories by naming teams.
//
// Trust here is MACHINE-level, deliberately: any admitted machine may sync any team that has a
// footprint on both ends, because admission means "this is my account's machine" — the same
// grant the ssh door extends. Teams are addressing, not tenancy (config.go says the same). If
// that ever changes — machines admitted that should see only SOME teams — this is the seam a
// per-team allowlist belongs in.
func expSyncHandle(w http.ResponseWriter, configDir string, body []byte) {
	var ask expSyncAsk
	if json.Unmarshal(body, &ask) != nil || strings.TrimSpace(ask.Team) == "" {
		answerExpSync(w, expSyncReply{Err: "exp-sync names a team"})
		return
	}
	dir := filepath.Join(configDir, "teams", sanitizeTeam(ask.Team), "experience")
	if _, err := os.Stat(dir); err != nil {
		answerExpSync(w, expSyncReply{Err: "no companion of this account is on that team here"})
		return
	}
	if expSyncAbsorb(dir, ask.Files) > 0 {
		// The pages/ cache is the human/git view; without this it kept showing the pre-sync
		// winner until somebody happened to edit locally. Readers derive from revisions either
		// way — this is for the people and the diffs.
		expgit.New(dir).WikiRefreshPages()
	}
	want, files := expSyncDiff(dir, ask.Have)
	answerExpSync(w, expSyncReply{OK: true, Want: want, Files: files})
}

func answerExpSync(w http.ResponseWriter, r expSyncReply) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(r); err != nil {
		// The status is gone and a partial body is on the wire; the one useful act left is a
		// trace of WHICH answer broke, same as every other fleet reply.
		fmt.Fprintf(os.Stderr, "magi: writing an exp-sync answer: %v\n", err)
	}
}

// expSyncWithPeer converges one team with one admitted machine: exchange digests, absorb what
// they hold, send what they want — repeated while either side still owes the other, bounded.
func expSyncWithPeer(ctx context.Context, configDir, host string, peer identity.Peer, team, dir string) (got int, err error) {
	id, err := identity.Load(configDir, host)
	if err != nil {
		return 0, err
	}
	client := &http.Client{
		Timeout: fleetCrossTimeout,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			Certificates:       []tls.Certificate{id.Cert},
			MinVersion:         tls.VersionTLS13,
			InsecureSkipVerify: true, //nolint:gosec // pinned to the admitted fingerprint below
			VerifyPeerCertificate: identity.VerifyAdmitted(configDir, func(p identity.Peer) bool {
				return p.Fingerprint == peer.Fingerprint
			}),
		}},
	}
	send := map[string]string{}
	for round := 0; round < 5; round++ {
		ask := expSyncAsk{Method: "exp-sync", Team: team, Have: expSyncInventory(dir), Files: send}
		body, _ := json.Marshal(ask)
		req, rerr := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+peer.Addr+fleetPath, bytes.NewReader(body))
		if rerr != nil {
			return got, rerr
		}
		req.Header.Set("Content-Type", "application/json")
		resp, rerr := client.Do(req)
		if rerr != nil {
			return got, rerr
		}
		answer, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		var reply expSyncReply
		if json.Unmarshal(answer, &reply) != nil || !reply.OK {
			return got, fmt.Errorf("%s", orWordCLI(reply.Err, "malformed exp-sync answer"))
		}
		if n := expSyncAbsorb(dir, reply.Files); n > 0 {
			got += n
			expgit.New(dir).WikiRefreshPages() // same human-cache refresh the serving side does
		}
		if len(reply.Want) == 0 && len(reply.Files) == 0 {
			return got, nil // both sides hold the same set
		}
		send = map[string]string{}
		total := 0
		for _, p := range reply.Want {
			// Shape-checked BEFORE any filesystem call: a want-list is peer-supplied input, and
			// a read that precedes the check is a read a hostile path got to steer.
			if !expSyncPathOK(p) {
				continue
			}
			b, ferr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
			if ferr != nil {
				continue
			}
			if total += len(b); total > expSyncBytesCap {
				break
			}
			send[p] = string(b)
		}
		if len(send) == 0 && len(reply.Files) == 0 {
			return got, nil
		}
	}
	return got, nil
}

// expSyncLoop is the anti-entropy heartbeat: every team with a footprint on this machine, against
// every admitted machine with an address, on start and on a slow tick. Quiet syncs are two small
// digests; the loop logs only when files actually moved.
func expSyncLoop(ctx context.Context, configDir, host string, errOut io.Writer) {
	sweep := func() {
		teams, err := os.ReadDir(filepath.Join(configDir, "teams"))
		if err != nil {
			return
		}
		for _, t := range teams {
			if !t.IsDir() {
				continue
			}
			dir := filepath.Join(configDir, "teams", t.Name(), "experience")
			if _, err := os.Stat(dir); err != nil {
				continue
			}
			for _, peer := range identity.Admitted(configDir) {
				if peer.Addr == "" {
					continue
				}
				got, err := expSyncWithPeer(ctx, configDir, host, peer, t.Name(), dir)
				switch {
				case err != nil:
					// A peer that is off or unreachable is ordinary fleet weather; the next
					// sweep tries again. Said once per sweep, quietly.
					fmt.Fprintf(errOut, "magi: exp-sync %s with %s: %v\n", t.Name(), orWordCLI(peer.Label, peer.Addr), err)
				case got > 0:
					fmt.Fprintf(errOut, "magi: exp-sync %s with %s: +%d files\n", t.Name(), orWordCLI(peer.Label, peer.Addr), got)
				}
			}
		}
	}
	// First sweep shortly after the door opens, then the slow tick. Anti-entropy is periodic by
	// nature — this is the repair pass, not the delivery path; a page still travels the moment
	// both doors are up and a sweep runs on either side.
	select {
	case <-time.After(10 * time.Second):
		sweep()
	case <-ctx.Done():
		return
	}
	tick := time.NewTicker(expSyncEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			sweep()
		case <-ctx.Done():
			return
		}
	}
}
