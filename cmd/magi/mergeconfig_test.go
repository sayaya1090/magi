package main

import (
	"reflect"
	"testing"

	"github.com/sayaya1090/magi/internal/config"
)

// mergeProjectConfig overlays a project .magi/config.toml onto the global config.
// The contract: scalars override only when the project set them; slices append;
// maps merge with the project value winning on a key collision. These tests pin
// each of those three behaviors so a future edit to the overlay can't silently
// flip precedence.

func TestMergeProjectConfig_ScalarsOverrideOnlyWhenSet(t *testing.T) {
	base := config.Config{
		Model:         "global-model",
		BaseURL:       "http://global",
		Permission:    "ask",
		Profile:       "safe",
		Sandbox:       "ro",
		ExperienceDir: "/global/exp",
	}

	// An all-empty project must leave every global scalar untouched.
	if got := mergeProjectConfig(base, config.Config{}); !reflect.DeepEqual(got, base) {
		t.Fatalf("empty project changed the config:\n got=%+v\nwant=%+v", got, base)
	}

	// A project that sets scalars overrides exactly those, leaving others intact.
	proj := config.Config{Model: "proj-model", Permission: "auto"}
	got := mergeProjectConfig(base, proj)
	if got.Model != "proj-model" {
		t.Errorf("Model: got %q, want proj-model", got.Model)
	}
	if got.Permission != "auto" {
		t.Errorf("Permission: got %q, want auto", got.Permission)
	}
	if got.BaseURL != "http://global" {
		t.Errorf("BaseURL should be untouched, got %q", got.BaseURL)
	}
	if got.Profile != "safe" || got.Sandbox != "ro" || got.ExperienceDir != "/global/exp" {
		t.Errorf("unset project scalars leaked: %+v", got)
	}
}

func TestMergeProjectConfig_SlicesAppend(t *testing.T) {
	base := config.Config{
		Allow:        []string{"g-allow"},
		Deny:         []string{"g-deny"},
		AllowDomains: []string{"g.example"},
		Hooks:        []config.Hook{{Event: "pre", Command: "g-hook"}},
	}
	proj := config.Config{
		Allow:        []string{"p-allow"},
		Deny:         []string{"p-deny"},
		AllowDomains: []string{"p.example"},
		Hooks:        []config.Hook{{Event: "post", Command: "p-hook"}},
	}
	got := mergeProjectConfig(base, proj)
	if want := []string{"g-allow", "p-allow"}; !eqStr(got.Allow, want) {
		t.Errorf("Allow: got %v, want %v", got.Allow, want)
	}
	if want := []string{"g-deny", "p-deny"}; !eqStr(got.Deny, want) {
		t.Errorf("Deny: got %v, want %v", got.Deny, want)
	}
	if want := []string{"g.example", "p.example"}; !eqStr(got.AllowDomains, want) {
		t.Errorf("AllowDomains: got %v, want %v", got.AllowDomains, want)
	}
	if len(got.Hooks) != 2 || got.Hooks[0].Command != "g-hook" || got.Hooks[1].Command != "p-hook" {
		t.Errorf("Hooks did not append in order: %+v", got.Hooks)
	}
}

func TestMergeProjectConfig_MapsMergeProjectWins(t *testing.T) {
	base := config.Config{
		Routing: map[string]string{"a": "g", "keep": "g"},
		LLM:     config.LLMConfig{Headers: map[string]string{"X": "g", "Keep": "g"}},
	}
	proj := config.Config{
		Routing: map[string]string{"a": "p", "add": "p"},
		LLM:     config.LLMConfig{Headers: map[string]string{"X": "p", "Add": "p"}},
	}
	got := mergeProjectConfig(base, proj)
	if got.Routing["a"] != "p" || got.Routing["keep"] != "g" || got.Routing["add"] != "p" {
		t.Errorf("Routing merge wrong: %v", got.Routing)
	}
	if got.LLM.Headers["X"] != "p" || got.LLM.Headers["Keep"] != "g" || got.LLM.Headers["Add"] != "p" {
		t.Errorf("LLM.Headers merge wrong: %v", got.LLM.Headers)
	}
}

func TestMergeProjectConfig_MapsMergeIntoNilBase(t *testing.T) {
	// A global config with no maps at all must still absorb project maps rather
	// than panic on a nil-map write.
	proj := config.Config{
		Routing: map[string]string{"a": "p"},
		MCP:     map[string]config.MCPServer{"m": {}},
		Plugins: map[string]map[string]any{"pl": {"k": 1}},
		LLM:     config.LLMConfig{Headers: map[string]string{"H": "p"}},
	}
	got := mergeProjectConfig(config.Config{}, proj)
	if got.Routing["a"] != "p" {
		t.Errorf("Routing not absorbed into nil base: %v", got.Routing)
	}
	if _, ok := got.MCP["m"]; !ok {
		t.Errorf("MCP not absorbed into nil base: %v", got.MCP)
	}
	if got.Plugins["pl"]["k"] != 1 {
		t.Errorf("Plugins not absorbed into nil base: %v", got.Plugins)
	}
	if got.LLM.Headers["H"] != "p" {
		t.Errorf("LLM.Headers not absorbed into nil base: %v", got.LLM.Headers)
	}
}

func TestMergeProjectConfig_Council(t *testing.T) {
	enabled := true
	base := config.Config{Council: config.CouncilConfig{
		Rule: "majority",
	}}
	proj := config.Config{Council: config.CouncilConfig{
		Enabled: &enabled,
		Rule:    "unanimous",
		Preset:  "light",
		Members: []config.CouncilMember{{}, {}},
	}}
	got := mergeProjectConfig(base, proj)
	c := got.Council
	if c.Enabled == nil || *c.Enabled != true {
		t.Errorf("Enabled not applied: %v", c.Enabled)
	}
	if c.Rule != "unanimous" || c.Preset != "light" {
		t.Errorf("scalar council fields wrong: %+v", c)
	}
	if len(c.Members) != 2 {
		t.Errorf("Members should be replaced by project's 2, got %d", len(c.Members))
	}

	// An empty project council must not clobber global council settings.
	got2 := mergeProjectConfig(base, config.Config{})
	if got2.Council.Rule != "majority" || got2.Council.Enabled != nil {
		t.Errorf("empty project council clobbered global: %+v", got2.Council)
	}
}

func eqStr(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Who a companion is comes from its workspace, and it used to come from nowhere.
//
// MANUAL §13 says to write `[companion]` into `.magi/config.toml` "so it travels with the repo".
// Nothing merged it: a workspace declaring a name, a role and a team published under its
// directory's basename with neither — so teams, roles and hubs, the whole of §13, were unreachable
// the documented way. The only way that worked was the global config, which gives every workspace
// on the machine one identity.
func TestMergeProjectConfig_TheWorkspaceSaysWhoTheCompanionIs(t *testing.T) {
	global := config.Config{Companion: config.CompanionConfig{Name: "machine", Role: "everything"}}
	proj := config.Config{Companion: config.CompanionConfig{
		Name: "invoices", Role: "the billing API", Team: "backend", Hub: true, MCPPeers: true,
	}}
	got := mergeProjectConfig(global, proj).Companion
	if got != proj.Companion {
		t.Errorf("the workspace's identity did not survive the merge:\n got %+v\nwant %+v",
			got, proj.Companion)
	}

	// Wholesale, not field by field: `hub = false` and an unset hub are the same value, so a
	// per-field merge could never let a workspace turn off what the global config turned on.
	on := config.Config{Companion: config.CompanionConfig{Name: "machine", Hub: true, MCPPeers: true}}
	off := config.Config{Companion: config.CompanionConfig{Name: "invoices"}}
	switch after := mergeProjectConfig(on, off).Companion; {
	case after.Hub:
		t.Error("a workspace that does not declare itself a hub inherited hub = true")
	case after.MCPPeers:
		t.Error("a workspace that did not ask for peer MCP inherited it")
	case after.Name != "invoices":
		t.Errorf("name %q", after.Name)
	}

	// And a workspace that declares nothing keeps whatever the machine said, which is what a
	// single-companion setup does.
	if got := mergeProjectConfig(global, config.Config{}).Companion; got != global.Companion {
		t.Errorf("an empty [companion] block wiped the machine's: %+v", got)
	}
}

// TestEveryConfigFieldIsEitherMergedOrKnowinglyNotMerged is the guard the tests above cannot be.
//
// Each of them names a field. That works until somebody adds a field — and then the overlay is
// silently missing it, a project sets it in .magi/config.toml, and nothing happens. The failure is
// invisible: no error, no warning, the value simply does not arrive. That shape (a mechanism whose
// counterpart was never written) is the most common defect in this tree, and enumerating fields by
// hand is what lets it in.
//
// So this walks Config by reflection: set exactly one field to a non-zero value, overlay it onto an
// empty global config, and require the result to differ. A field that changes nothing is either
// merged and broken, or never merged at all.
//
// It compares at top-level granularity. A struct field whose sub-fields are merged one by one (LLM,
// Council) passes as long as ANY of them lands, so it cannot catch a missing sub-field — the named
// tests above cover those.
func TestEveryConfigFieldIsEitherMergedOrKnowinglyNotMerged(t *testing.T) {
	// Fields that do not cross from a project file, with the reason. A new entry here should be a
	// decision somebody made, not a place to put a field to make this test quiet.
	//
	// Everything below was already unmerged when this guard was written. None of it has been
	// reviewed: the guard's job today is to stop the list GROWING by accident, and the list itself
	// is the record of what to look at. Several look like plain omissions — a repo that pins its
	// model cannot pin the sampling that model needs.
	notMerged := map[string]string{
		"APIKey": "a key in a committed file is a leaked key — plausibly deliberate, unreviewed",
		// Deliberate, and the one entry here that would be a security hole rather than a surprise.
		// A reach is a COMMAND LINE this machine runs to open a pipe. A project config is committed
		// and shared, so merging it would let a cloned repo choose what its reader executes — the
		// same thing --join refuses for [mcp] and gossip refuses for reaches. How this machine gets
		// to another one is this machine's business, said in its own global config.
		"Reach":           "a command line this machine runs; a shared file must not choose it",
		"EmbedModel":      "unreviewed",
		"Subagents":       "written by /subagents for this user, not for the repo — plausibly deliberate",
		"Limits":          "unreviewed",
		"Sampling":        "unreviewed",
		"MaxOutputTokens": "unreviewed",
		"ContextTokens":   "unreviewed",
		"CompactRatio":    "unreviewed",
		"Temperature":     "unreviewed",
		"TopP":            "unreviewed",
		"TopK":            "unreviewed",
	}

	base := config.Config{}
	ct := reflect.TypeOf(config.Config{})
	for i := range ct.NumField() {
		f := ct.Field(i)
		if !f.IsExported() {
			continue
		}
		proj := config.Config{}
		reflect.ValueOf(&proj).Elem().Field(i).Set(nonZeroValue(t, f.Type))

		got := mergeProjectConfig(base, proj)
		landed := !reflect.DeepEqual(got, base)

		if why, exempt := notMerged[f.Name]; exempt {
			if landed {
				t.Errorf("%s is listed as not merged (%s) but mergeProjectConfig does merge it — "+
					"delete the entry", f.Name, why)
			}
			continue
		}
		if !landed {
			t.Errorf("a project config setting %s changes nothing: mergeProjectConfig has no case "+
				"for it. Add one, or add it to notMerged with the reason.", f.Name)
		}
	}
}

// nonZeroValue builds a value of t that is distinguishable from t's zero value, deeply enough that
// a struct with only merged sub-fields still comes out non-zero.
func nonZeroValue(t *testing.T, typ reflect.Type) reflect.Value {
	t.Helper()
	switch typ.Kind() {
	case reflect.Bool:
		return reflect.ValueOf(true).Convert(typ)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return reflect.ValueOf(int64(7)).Convert(typ)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(uint64(7)).Convert(typ)
	case reflect.Float32, reflect.Float64:
		return reflect.ValueOf(0.75).Convert(typ)
	case reflect.String:
		return reflect.ValueOf("probe").Convert(typ)
	case reflect.Pointer:
		p := reflect.New(typ.Elem())
		p.Elem().Set(nonZeroValue(t, typ.Elem()))
		return p
	case reflect.Slice:
		s := reflect.MakeSlice(typ, 1, 1)
		s.Index(0).Set(nonZeroValue(t, typ.Elem()))
		return s
	case reflect.Map:
		m := reflect.MakeMap(typ)
		m.SetMapIndex(nonZeroValue(t, typ.Key()), nonZeroValue(t, typ.Elem()))
		return m
	case reflect.Interface:
		// Only reached for map[string]any plugin settings.
		return reflect.ValueOf("probe")
	case reflect.Struct:
		v := reflect.New(typ).Elem()
		for i := range typ.NumField() {
			if typ.Field(i).IsExported() {
				v.Field(i).Set(nonZeroValue(t, typ.Field(i).Type))
			}
		}
		return v
	default:
		t.Fatalf("nonZeroValue: no case for %s (%s)", typ, typ.Kind())
		return reflect.Value{}
	}
}
