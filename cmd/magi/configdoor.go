package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
)

// The settings door.
//
// One door for the settings rather than a door per setting. Seven places decide which model runs
// and exactly two of them had a method (set-model, use-backend), so a client without a config file
// to edit — the IDE plugin holds a socket and nothing else — had to write "not editable here" where
// a field belongs, and every new setting meant the same conversation again. The console reached the
// rest by editing config.toml itself, which is not a thing a socket client can do.
//
// The keys are a WHITELIST. Arbitrary TOML writing down a socket is a hole and not a door: the same
// file holds the permission posture, the hooks that run, and the addresses prompts are sent to.

// settingKey is one editable setting: where it lives in the file, how the engine reads it, and what
// a screen has to say about it.
type settingKey struct {
	key     string // the TOML path a client names
	section string // the table it sits in ("" = the file's root)
	name    string // its key within that table
	env     string // an environment variable that beats every file, if one does
	applies string // "now" or "next start" — a property of THIS key, not one sentence for all
	doc     string
	// profile marks a key whose value is the name of an [llm.profiles.*] entry, so a screen knows
	// to offer the `profiles` list rather than a free-text box.
	profile bool
	// stranger marks a key an UNTRUSTED workspace file cannot set: asStranger drops these before
	// the merge, so writing one there would land in the file and change nothing.
	stranger bool
	get      func(config.Config) string
}

// settingKeys is the whitelist. Kept small and explicit: every entry is a key somebody asked to be
// able to change from a screen, and adding one is a deliberate act rather than a consequence of a
// struct field appearing in internal/config.
var settingKeys = []settingKey{
	{
		key: "embed_model", name: "embed_model", env: "MAGI_EMBED_MODEL", applies: "next start",
		doc: "the model that embeds for recall; empty means recall stays at the string level",
		get: func(c config.Config) string { return c.EmbedModel },
	},
	{
		key: "autocomplete.code_profile", section: "autocomplete", name: "code_profile",
		applies: "next start", profile: true, stranger: true,
		doc: "the [llm.profiles.*] used for code completion; empty turns it off",
		get: func(c config.Config) string { return c.Autocomplete.CodeProfile },
	},
	{
		key: "autocomplete.composer_profile", section: "autocomplete", name: "composer_profile",
		applies: "next start", profile: true, stranger: true,
		doc: "the [llm.profiles.*] used for composer suggestions; empty turns it off",
		get: func(c config.Config) string { return c.Autocomplete.ComposerProfile },
	},
	{
		key: "templates.commit", section: "templates", name: "commit",
		applies: "next start", stranger: true,
		doc: "the template a commit message is written to",
		get: func(c config.Config) string { return c.Templates.Commit },
	},
	{
		key: "templates.pr", section: "templates", name: "pr",
		applies: "next start", stranger: true,
		doc: "the template a pull request body is written to",
		get: func(c config.Config) string { return c.Templates.PR },
	},
}

func settingKeyOf(key string) (settingKey, bool) {
	for _, k := range settingKeys {
		if k.key == strings.TrimSpace(key) {
			return k, true
		}
	}
	return settingKey{}, false
}

// configTiers are the files this daemon actually reads, in the order they are merged: later wins.
// Named the way the merge names them so a screen's word for a layer is the engine's word for it.
func (d daemonEngine) configTiers() []struct{ tier, dir string } {
	return []struct{ tier, dir string }{
		{"global", d.configDir},
		{"project", filepath.Join(d.workdir, ".magi")},
		{"companion", config.CompanionDir(d.configDir, daemon.WorkspaceKey(d.workdir))},
	}
}

// resolve answers a key the way the engine will read it: the environment beats every file, and
// among the files the last one merged wins. An untrusted workspace file is skipped for the keys
// the stranger-merge drops, because a value the engine will not use is not the effective value.
func (d daemonEngine) resolve(k settingKey) daemon.ConfigItem {
	item := daemon.ConfigItem{Key: k.key, Applies: k.applies, Doc: k.doc}
	trusted := config.Trusted(d.configDir, d.workdir)
	for _, t := range d.configTiers() {
		if k.stranger && t.tier == "project" && !trusted {
			continue
		}
		cfg, err := config.Load(t.dir)
		if err != nil {
			// Said, not skipped. A file with a typo in it and a file that says nothing look the
			// same to a reader shown only values, and this is the door whose whole promise is
			// that the screen sees what the daemon read. Kept going, because the layers below it
			// still hold values the engine would use if this one were fixed.
			item.Unreadable = strings.TrimSpace(item.Unreadable + " " +
				fmt.Sprintf("%s (%s) will not parse: %v;", filepath.Join(t.dir, "config.toml"), t.tier, err))
			continue
		}
		if v := strings.TrimSpace(k.get(cfg)); v != "" {
			item.Value, item.Source = v, t.tier
			item.Tier, item.File = t.tier, filepath.Join(t.dir, "config.toml")
		}
	}
	if k.env != "" {
		// Tested the way the engine tests it (env(): a non-empty string wins), so a value of one
		// space is reported as what will actually be used rather than trimmed away here.
		if v := os.Getenv(k.env); v != "" {
			// Said as the effective value with its source, because a companion embedding with
			// something the file does not name is exactly the case a screen has to be able to
			// explain. The file half is left in Tier/File, which is still where a write goes.
			item.Value, item.Source = v, "env"
		}
	}
	if item.Tier == "" {
		// Nothing sets it: a write lands in the workspace unless the key cannot live there.
		item.Tier = "project"
		if k.stranger && !trusted {
			item.Tier = "global"
		}
		item.File = filepath.Join(d.tierDir(item.Tier), "config.toml")
	}
	return item
}

func (d daemonEngine) tierDir(tier string) string {
	for _, t := range d.configTiers() {
		if t.tier == tier {
			return t.dir
		}
	}
	return ""
}

// ConfigHere satisfies daemon.ConfigKeeper: every editable key as the engine reads it.
func (d daemonEngine) ConfigHere(context.Context) ([]daemon.ConfigItem, error) {
	out := make([]daemon.ConfigItem, 0, len(settingKeys))
	for _, k := range settingKeys {
		out = append(out, d.resolve(k))
	}
	return out, nil
}

// ConfigSet writes one key and answers with the key as it now stands — read back rather than
// echoed, so a screen redraws from the daemon's own reading instead of from what it hoped happened.
func (d daemonEngine) ConfigSet(ctx context.Context, key, value, tier string) (daemon.ConfigItem, error) {
	k, ok := settingKeyOf(key)
	if !ok {
		return daemon.ConfigItem{}, fmt.Errorf("%q is not a setting this door changes — `config-get` lists the ones it does", key)
	}
	if tier = strings.TrimSpace(tier); tier == "" {
		// Written back where it was read from. Defaulting to the workspace instead would mint a
		// project override of a setting somebody meant to change for the whole account.
		tier = d.resolve(k).Tier
	}
	dir := d.tierDir(tier)
	if dir == "" {
		return daemon.ConfigItem{}, fmt.Errorf("%q is not a config layer — global, project or companion", tier)
	}
	if k.stranger && tier == "project" && !config.Trusted(d.configDir, d.workdir) {
		// Refused rather than written: the file would take the line and the engine would ignore
		// it, which is the worst of the three outcomes — a setting that reads back as changed and
		// does nothing.
		return daemon.ConfigItem{}, fmt.Errorf(
			"this workspace's config.toml is not trusted, so %s set there would be ignored — "+
				"trust it (`magi --trust`) or set this one at the global or companion layer", k.key)
	}
	// 0700 for the account's own directories and 0755 for the workspace's: .magi sits in a
	// checked-out repository beside the files everybody on the project reads, and every other
	// creator of it (config.SetKey's own MkdirAll) makes it 0755.
	mode := os.FileMode(0o700)
	if tier == "project" {
		mode = 0o755
	}
	if err := os.MkdirAll(dir, mode); err != nil {
		return daemon.ConfigItem{}, err
	}
	before, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	path := filepath.Join(dir, "config.toml")
	// The bytes a %q-rendered TOML value cannot survive are dropped at the one gate that knows
	// them (config.StripControl), which the console's writer for these same four keys has always
	// had and this door was written without.
	if err := config.SetKey(path, k.section, k.name, config.StripControl(strings.TrimSpace(value))); err != nil {
		return daemon.ConfigItem{}, err
	}
	// Read the FILE back, not the value: a write that leaves a config.toml which will not parse
	// is a daemon that does not start next time — for every workspace on this machine when the
	// file is the global one. Put back what was there and say so, rather than answering with the
	// empty reading a broken file gives.
	if _, lerr := config.Load(dir); lerr != nil {
		if before == nil {
			os.Remove(path)
		} else {
			os.WriteFile(path, before, 0o600)
		}
		return daemon.ConfigItem{}, fmt.Errorf("%s would no longer parse with that value (%v) — nothing was changed", path, lerr)
	}
	item := d.resolve(k)
	if item.Source == "env" && strings.TrimSpace(value) != "" {
		// The write landed and something else still wins. Said here, because a screen that showed
		// the new value without this would be telling a person their change took effect.
		item.Doc = "written, but " + k.env + " in this daemon's environment is what it will use — " + item.Doc
	}
	return item, nil
}

// ProfilesHere lists the [llm.profiles.*] a profile-shaped key may name, with the layer that
// defines each. A name defined in two layers is reported as the one that WINS, or the picker says
// "global" about a definition the daemon will not use.
func (d daemonEngine) ProfilesHere(context.Context) ([]daemon.ProfileChoice, error) {
	tier := map[string]string{}
	trusted := config.Trusted(d.configDir, d.workdir)
	for _, t := range d.configTiers() {
		if t.tier == "project" && !trusted {
			continue // asStranger drops a stranger's profiles before the merge
		}
		cfg, err := config.Load(t.dir)
		if err != nil {
			continue
		}
		for name := range cfg.LLM.Profiles {
			tier[name] = t.tier
		}
	}
	out := make([]daemon.ProfileChoice, 0, len(tier))
	for name, t := range tier {
		out = append(out, daemon.ProfileChoice{Name: name, Tier: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
