package main

import (
	"net/http"
	"os"
	osuser "os/user"
	"sort"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/version"
)

// Which console this is.
//
// # Why a screen needs this at all
//
// A supervisor watching three machines has three of these open, and they are the same page drawn
// the same way. The tab title says "magi" in all three. Before this, the only way to tell which
// window was the laptop and which was the build box was to recognise the companions in the list —
// which fails at exactly the moment it matters, when two machines are running the same work.
//
// # What it is not
//
// It is not an account, and there is nobody to log in. magi has no users: the console is reachable
// by whoever can reach the port, and who that is belongs to whatever put the port there. Two facts
// are published, both of them about the MACHINE rather than about a person:
//
//   - the host it runs on, which is the name the operator already uses for it;
//   - the config directory it reads, which is what decides the whole rest of the page — every
//     companion listed is one this directory knows about, so a console reading the wrong directory
//     shows an empty fleet and no reason for it.
//
// Nothing here is a secret. The hostname is already on every fleet row that came from this machine
// and the directory is a path the reader owns; a token or a credential would not go in a JSON
// answer served to a browser, which is the same rule the MCP screen follows for env values.
type consoleInfo struct {
	Host      string `json:"host,omitempty"`
	ConfigDir string `json:"configDir,omitempty"`
	// User is the OS account this process runs as, because that — not the machine — is the unit
	// everything else here is scoped to. Two accounts on one host read two config directories,
	// enforce two policies and cannot write to each other's sessions; one account on two hosts is
	// two of everything as well. So "which magi am I looking at" is answered by the pair, and the
	// screen says it as user@host.
	User string `json:"user,omitempty"`
	// Peers is the other consoles this one federates, by name. The page does not draw them yet;
	// they are here because "which machine am I looking at" and "which machines can this one see"
	// are the same question asked from either end, and answering only half of it invites a second
	// endpoint later that disagrees with this one.
	Peers []string `json:"peers,omitempty"`
	// Version is the build this console is, and Daemons the builds the companions on this machine
	// are running.
	//
	// Two lines rather than one because they come apart exactly when it matters: upgrading replaces
	// the binary and every daemon already running keeps the old one until it is restarted. The
	// console is the process a person just reloaded, so it is always the new number — and "why
	// hasn't the thing I shipped shown up" is answered by the other one.
	Version string `json:"version,omitempty"`
	// Daemons is every distinct version among the companions published here, newest first is not
	// meaningful — they are sorted, and a machine where everything agrees has one entry.
	Daemons []string `json:"daemons,omitempty"`
	// EmbedModel is what searches on this machine turn text into vectors with, or empty for none.
	// It is here rather than on a settings screen because it is a COMPATIBILITY fact about the
	// team: vectors from two models are not comparable, so two companions on different ones share
	// no search at all, and the symptom is a search that quietly stops matching.
	EmbedModel string `json:"embedModel,omitempty"`
}

func (s *server) console(w http.ResponseWriter, r *http.Request) {
	if s.forwarded(w, r, s.proxy) {
		return
	}
	out := consoleInfo{ConfigDir: s.cfgDir, EmbedModel: s.embedModel, Version: version.Version}
	// The companions' builds, from the records they published. A daemon too old to write one
	// contributes nothing rather than "unknown": the line is about what IS running, and a machine
	// where everything agrees should read as one number and not as a list with a hole in it.
	if list, err := daemon.List(s.cfgDir); err == nil {
		seen := map[string]bool{}
		for _, in := range list {
			if in.Version == "" || seen[in.Version] {
				continue
			}
			seen[in.Version] = true
			out.Daemons = append(out.Daemons, in.Version)
		}
		sort.Strings(out.Daemons)
	}
	// A host that cannot be read is left empty rather than filled with a guess: the page draws the
	// lines it has, and "unknown" on a screen answering "which machine is this" is worse than the
	// line not being there. Same for the account.
	out.User, out.Host = whereWeAre()
	for _, p := range s.peers {
		out.Peers = append(out.Peers, p.Name)
	}
	writeJSON(w, "console", out)
}

// whereWeAre names this instance: the account and the machine.
//
// # Why not an address
//
// An IP says how to REACH a machine, which is a different question and a worse answer to this one:
// a host has several, they change under DHCP, a tunnel makes every peer look like 127.0.0.1, and
// behind NAT two machines share one. A hostname is what the person already calls it.
//
// # Why the account is half of it
//
// The trust boundary here is the OS user, not the box. Two accounts on one machine have separate
// config directories, separate policies, separate sessions, and neither can write the other's
// files — so a permission list belongs to user@host and never to a host. The config directory is
// carried beside this rather than folded into it, because MAGI_CONFIG_DIR can give one account two
// instances, and then the directory is the only thing that tells them apart.
func whereWeAre() (user, host string) {
	if u, err := osuser.Current(); err == nil {
		user = u.Username
	}
	if h, err := os.Hostname(); err == nil {
		host = h
	}
	return user, host
}

// instanceOf is user@host, or whichever half could be read.
func instanceOf(user, host string) string {
	switch {
	case user != "" && host != "":
		return user + "@" + host
	case host != "":
		return host
	default:
		return user
	}
}
