package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/text"
)

// Joining a team: what the newcomer needs before it can be useful.
//
// A companion set up this morning knows nothing its team has agreed on — which experience store
// they share, which MCP servers they all talk to, what the standing instructions in their AGENTS.md
// say. Somebody copies those by hand today, or more likely does not, and the new companion spends a
// week being told things the others already know.
//
// # It proposes; a person applies
//
// Nothing here writes config.toml. The proposal lands beside it as its own file, and the person
// reads it and moves what they want.
//
// That is not caution for its own sake. An [mcp] entry is a COMMAND with arguments — a config
// merged from another workspace is arbitrary code this process would later run, and "the companion
// I joined to told me to" is not a sentence anybody should find in an incident report. The same
// goes for hooks. So the proposal is a file: readable, diffable, and inert until somebody decides.
//
// # Same machine, plain files
//
// It reads the other companion's project config off disk, because on one machine every workspace is
// already readable by the same user — the isolation between companions is context hygiene, not a
// security boundary, and pretending otherwise with a protocol would buy nothing. Across machines
// there is no join: that is the operator's peer list, deliberately.
func joinTeam(w io.Writer, configDir, myWorkdir, want string) int {
	if strings.TrimSpace(want) == "" {
		fmt.Fprintln(w, "magi: --join needs the name of a companion to join, e.g. `magi --join design`")
		return 2
	}
	found, err := daemon.List(configDir)
	if err != nil {
		fmt.Fprintln(w, "magi:", err)
		return 1
	}
	var them []daemon.Info
	for _, in := range found {
		if strings.EqualFold(in.Name, want) || strings.EqualFold(in.Team, want) ||
			strings.EqualFold(filepath.Base(in.Workdir), want) {
			them = append(them, in)
		}
	}
	switch len(them) {
	case 0:
		fmt.Fprintf(w, "magi: nobody here is called %q. Published companions: %s\n", want, published(found))
		return 1
	case 1:
	default:
		// A team name matching several is the ordinary case — a team is several companions — and
		// the newcomer should join the one whose setup it means to copy.
		fmt.Fprintf(w, "magi: %q matches %s — name one of them\n", want, published(them))
		return 1
	}
	source := them[0]
	if source.Workdir == myWorkdir {
		fmt.Fprintln(w, "magi: that is this workspace")
		return 1
	}

	theirs, err := config.Load(filepath.Join(source.Workdir, ".magi"))
	if err != nil {
		fmt.Fprintf(w, "magi: cannot read %s's project config: %v\n", nameOf(source), err)
		return 1
	}
	proposal := proposeJoin(source, theirs)
	dir := filepath.Join(myWorkdir, ".magi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(w, "magi:", err)
		return 1
	}
	path := filepath.Join(dir, "joined-"+sanitizeName(nameOf(source))+".toml")
	if err := os.WriteFile(path, []byte(proposal), 0o644); err != nil {
		fmt.Fprintln(w, "magi:", err)
		return 1
	}
	fmt.Fprintf(w, "Wrote %s — what %s's workspace shares, as a proposal.\n\n"+
		"Nothing has been applied. Read it, then move the parts you want into "+
		"%s.\nAn [mcp] entry is a command this magi would run, so that is a decision "+
		"rather than a copy.\n", path, nameOf(source), filepath.Join(dir, "config.toml"))
	return 0
}

// proposeJoin renders what one workspace shares, as TOML somebody can move across by hand.
//
// Only the parts that are a TEAM's business. A model, a permission posture, a sandbox setting are
// this workspace's own choices and copying them is how one person's laptop settings become the
// team's — the very drift the two-tier experience store exists to prevent.
func proposeJoin(source daemon.Info, c config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# What %s's workspace shares with its team.\n", nameOf(source))
	fmt.Fprintf(&b, "# Read from %s, %s.\n", filepath.Join(source.Workdir, ".magi", "config.toml"),
		nowStamp())
	b.WriteString("#\n# NOTHING HERE IS IN EFFECT. Move what you want into .magi/config.toml\n" +
		"# yourself — an [mcp] entry is a command this magi would run, and a hook is a shell\n" +
		"# line, so those are decisions rather than copies.\n\n")

	if c.Companion.Team != "" {
		fmt.Fprintf(&b, "[companion]\n# team = %q            # the group they are in\n"+
			"# name = \"...\"        # yours, and it must differ from theirs\n"+
			"# role = \"...\"        # what YOUR workspace is for\n\n", c.Companion.Team)
	}
	if c.ExperienceDir != "" {
		b.WriteString("# The shared brain: point at the same directory and you retrieve what the\n" +
			"# team has learned, and contribute back to it. This is the one line that makes a\n" +
			"# newcomer start knowing things.\n")
		fmt.Fprintf(&b, "# experience_dir = %q\n\n", c.ExperienceDir)
	}
	if len(c.MCP) > 0 {
		names := make([]string, 0, len(c.MCP))
		for n := range c.MCP {
			names = append(names, n)
		}
		sort.Strings(names)
		b.WriteString("# External tool servers they use. Each is a command with arguments that\n" +
			"# this process would start — check what it is before you enable it.\n")
		for _, n := range names {
			s := c.MCP[n]
			fmt.Fprintf(&b, "# [mcp.%s]\n", n)
			if s.URL != "" {
				fmt.Fprintf(&b, "#   url = %q\n", s.URL)
			}
			if s.Command != "" {
				fmt.Fprintf(&b, "#   command = %q\n", s.Command)
				if len(s.Args) > 0 {
					fmt.Fprintf(&b, "#   args = [%s]\n", quoteList(s.Args))
				}
			}
			// Env is named and never valued: these carry tokens, and a token copied into a second
			// workspace is a token in two places, which is one more than anybody counted on.
			if len(s.Env) > 0 {
				fmt.Fprintf(&b, "#   env = [%s]   # values NOT copied — set your own\n",
					quoteList(envNames(s.Env)))
			}
		}
		b.WriteString("\n")
	}
	if agents := filepath.Join(source.Workdir, ".magi", "AGENTS.md"); exists(agents) {
		fmt.Fprintf(&b, "# Their standing instructions are in %s.\n"+
			"# Read it; whatever is the TEAM's rather than their workspace's belongs in yours too.\n\n",
			agents)
	}
	if len(c.Hooks) > 0 {
		fmt.Fprintf(&b, "# They run %d hook(s) — shell commands on tool events. Not copied: read\n"+
			"# their config and decide.\n\n", len(c.Hooks))
	}
	if b.Len() < 400 { // only the header got written
		b.WriteString("# They share nothing beyond this: no experience directory, no MCP servers,\n" +
			"# no hooks. Joining them is just declaring your own [companion] name and role.\n")
	}
	return b.String()
}

// nowStamp dates the proposal: a file that says where it came from and not when is one nobody can
// tell is stale.
func nowStamp() string { return time.Now().Format("2006-01-02 15:04") }

func nameOf(in daemon.Info) string {
	if in.Name != "" {
		return in.Name
	}
	return filepath.Base(in.Workdir)
}

func published(list []daemon.Info) string {
	if len(list) == 0 {
		return "(none)"
	}
	out := make([]string, 0, len(list))
	for _, in := range list {
		n := nameOf(in)
		if in.Team != "" {
			n += " [" + in.Team + "]"
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func envNames(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		out = append(out, name)
	}
	return out
}

func quoteList(xs []string) string {
	out := make([]string, len(xs))
	for i, x := range xs {
		out[i] = fmt.Sprintf("%q", text.Clip(x, 120))
	}
	return strings.Join(out, ", ")
}

// sanitizeName keeps a companion's name usable as a file name without losing which one it was.
func sanitizeName(s string) string {
	var keep []rune
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			keep = append(keep, r)
		case r > 127:
			keep = append(keep, r)
		default:
			keep = append(keep, '-')
		}
	}
	if out := strings.Trim(string(keep), "-"); out != "" {
		return out
	}
	return "companion"
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
