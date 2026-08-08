package main

import (
	"net/http"
	"os"
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
	// Peers is the other consoles this one federates, by name. The page does not draw them yet;
	// they are here because "which machine am I looking at" and "which machines can this one see"
	// are the same question asked from either end, and answering only half of it invites a second
	// endpoint later that disagrees with this one.
	Peers []string `json:"peers,omitempty"`
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
	out := consoleInfo{ConfigDir: s.cfgDir, EmbedModel: s.embedModel}
	// A host that cannot be read is left empty rather than filled with a guess: the page draws the
	// lines it has, and "unknown" on a screen answering "which machine is this" is worse than the
	// line not being there.
	if h, err := os.Hostname(); err == nil {
		out.Host = h
	}
	for _, p := range s.peers {
		out.Peers = append(out.Peers, p.Name)
	}
	writeJSON(w, "console", out)
}
