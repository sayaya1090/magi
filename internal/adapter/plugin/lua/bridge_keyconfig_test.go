package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configHost loads one plugin with a config.toml at a temp path and returns the host, the
// logged output, and the config path (so a test can inspect the written file).
func configHost(t *testing.T, manifest, initLua, seed string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if seed != "" {
		if err := os.WriteFile(cfgPath, []byte(seed), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, out := loadHost(t, HostConfig{ConfigPath: cfgPath}, manifest, initLua)
	return out, cfgPath
}

// A plugin may always read/write its OWN [plugins.<name>] section; the value round-trips
// and is written to config.toml preserving the file.
func TestConfigKeySelfSection(t *testing.T) {
	out, cfgPath := configHost(t,
		`name="myplug"`+"\n"+`capabilities=["tool"]`,
		`magi.set_config_key("plugins.myplug.token", "abc123")
local v = magi.get_config_key("plugins.myplug.token")
magi.log("v=" .. tostring(v))`,
		"# user config\nmodel = \"x\"\n",
	)
	if !strings.Contains(out, "v=abc123") {
		t.Errorf("self-section write→read should round-trip, got: %q", out)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "[plugins.myplug]") || !strings.Contains(string(b), `token = "abc123"`) {
		t.Errorf("config.toml should hold the written key:\n%s", b)
	}
	if !strings.Contains(string(b), "# user config") {
		t.Errorf("the surgical edit must preserve existing content/comments:\n%s", b)
	}
}

// Writing outside the self section needs an explicit grant.
func TestConfigKeyNeedsGrant(t *testing.T) {
	out, _ := configHost(t,
		`name="p"`+"\n"+`capabilities=["tool"]`,
		`local r, e = magi.set_config_key("routing.model", "fast")
magi.log("denied=" .. tostring(r == nil) .. " err=" .. tostring(e))`,
		"",
	)
	if !strings.Contains(out, "denied=true") || !strings.Contains(out, "permission denied: config:write:routing.model") {
		t.Errorf("write outside self-section should need a grant: %q", out)
	}
}

// A config:write:<prefix>.* grant authorizes writes under that prefix.
func TestConfigKeyGrantedWildcard(t *testing.T) {
	out, cfgPath := configHost(t,
		`name="p"`+"\n"+`permissions=["config:write:routing.*","config:read:routing.*"]`,
		`magi.set_config_key("routing.model", "fast")
magi.log("got=" .. tostring(magi.get_config_key("routing.model")))`,
		"",
	)
	if !strings.Contains(out, "got=fast") {
		t.Errorf("granted wildcard write+read should work: %q", out)
	}
	b, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(b), "[routing]") || !strings.Contains(string(b), `model = "fast"`) {
		t.Errorf("routing.model should be written:\n%s", b)
	}
}

// The fixed deny-list blocks sensitive sections even WITH a matching grant.
func TestConfigKeyDenyList(t *testing.T) {
	for _, key := range []string{"mcp.servers", "hooks.stop", "allow", "deny"} {
		out, _ := configHost(t,
			`name="p"`+"\n"+`permissions=["config:write:*","config:read:*"]`,
			`local r, e = magi.set_config_key("`+key+`", "x")
magi.log("err=" .. tostring(e))`,
			"",
		)
		if !strings.Contains(out, "sensitive section") {
			t.Errorf("deny-listed key %q should be refused even with config:write:*, got: %q", key, out)
		}
	}
}

// get_config_key returns the caller default for a missing key, and reads a seeded value
// when granted.
func TestConfigKeyReadDefaultAndGrant(t *testing.T) {
	out, _ := configHost(t,
		`name="p"`+"\n"+`permissions=["config:read:model"]`,
		`magi.log("model=" .. tostring(magi.get_config_key("model")))
magi.log("missing=" .. tostring(magi.get_config_key("plugins.p.nope", "fallback")))`,
		"model = \"gpt-x\"\n",
	)
	if !strings.Contains(out, "model=gpt-x") {
		t.Errorf("granted top-level read should return the value: %q", out)
	}
	if !strings.Contains(out, "missing=fallback") {
		t.Errorf("a missing self-section key should return the default: %q", out)
	}
}

// A key containing a newline / brackets (a TOML-injection attempt to open another table)
// is rejected by the charset check before it can reach config.SetKey — even for the
// plugin's own section, where no grant is needed.
func TestConfigKeyRejectsInjection(t *testing.T) {
	out, cfgPath := configHost(t,
		`name="x"`+"\n"+`capabilities=["tool"]`,
		"local r, e = magi.set_config_key(\"plugins.x.k = 0\\n[hooks]\\nstop = \\\"evil\\\"\", \"v\")\n"+
			"magi.log(\"err=\" .. tostring(e))",
		"",
	)
	if !strings.Contains(out, "invalid key") {
		t.Errorf("a key with a newline/bracket must be rejected: %q", out)
	}
	if b, _ := os.ReadFile(cfgPath); strings.Contains(string(b), "[hooks]") {
		t.Errorf("injection must not write a [hooks] table:\n%s", b)
	}
}

// Whitespace/case variants of a deny-listed section are still blocked (TOML normalizes
// both on decode, so they'd otherwise reach the real section).
func TestConfigKeyDenyCaseAndSpace(t *testing.T) {
	for _, key := range []string{"MCP.servers", "Hooks.stop"} {
		out, _ := configHost(t,
			`name="p"`+"\n"+`permissions=["config:write:*"]`,
			`local r, e = magi.set_config_key("`+key+`", "x")
magi.log("err=" .. tostring(e))`,
			"",
		)
		if !strings.Contains(out, "sensitive section") {
			t.Errorf("case variant %q should be deny-listed, got: %q", key, out)
		}
	}
	// A key with an internal space is rejected as malformed (not a valid dotted key).
	out, _ := configHost(t,
		`name="p"`+"\n"+`permissions=["config:write:*"]`,
		`local r, e = magi.set_config_key("mcp .servers", "x")
magi.log("err=" .. tostring(e))`,
		"",
	)
	if !strings.Contains(out, "invalid key") {
		t.Errorf("a key with a space should be rejected: %q", out)
	}
}

// Security-posture keys are deny-listed so a plugin can't relax confinement.
func TestConfigKeyDenyPosture(t *testing.T) {
	for _, key := range []string{"profile", "sandbox", "permission", "allow_domains"} {
		out, _ := configHost(t,
			`name="p"`+"\n"+`permissions=["config:write:*"]`,
			`local r, e = magi.set_config_key("`+key+`", "x")
magi.log("err=" .. tostring(e))`,
			"",
		)
		if !strings.Contains(out, "sensitive section") {
			t.Errorf("posture key %q should be deny-listed, got: %q", key, out)
		}
	}
}

func TestConfigKeyUnitHelpers(t *testing.T) {
	if s, l := splitConfigKey("a.b.c"); s != "a.b" || l != "c" {
		t.Errorf("splitConfigKey(a.b.c) = (%q,%q)", s, l)
	}
	if s, l := splitConfigKey("model"); s != "" || l != "model" {
		t.Errorf("splitConfigKey(model) = (%q,%q)", s, l)
	}
	p := parsePerms([]string{"config:write:routing.*", "config:read:llm.headers"})
	if !p.allowConfigWrite("routing.model") || p.allowConfigWrite("other.x") {
		t.Error("wildcard config:write grant matched wrong keys")
	}
	if !p.allowConfigRead("llm.headers") || p.allowConfigRead("llm.profiles") {
		t.Error("exact config:read grant matched wrong keys")
	}
	for _, k := range []string{"mcp", "mcp.servers", "hooks.stop", "allow", "deny"} {
		if !configKeyDenied(k) {
			t.Errorf("%q should be deny-listed", k)
		}
	}
	if configKeyDenied("routing.model") {
		t.Error("routing.model should not be deny-listed")
	}
}

// A corrupt config.toml (unparseable) surfaces to get_config_key as (nil, err) —
// distinct from a missing key, which returns the caller's default. This lets a
// plugin detect "config is broken" and back off instead of blindly re-writing.
func TestConfigKeyReadParseError(t *testing.T) {
	out, _ := configHost(t,
		`name="p"`+"\n"+`permissions=["config:read:model"]`,
		`local v, e = magi.get_config_key("model", "fallback")
magi.log("v=" .. tostring(v))
magi.log("e=" .. tostring(e))`,
		"model = \"a\"\nmodel = \"b\"\n", // duplicate top-level key → parse error
	)
	if !strings.Contains(out, "v=nil") {
		t.Errorf("a parse error must NOT return the default value: %q", out)
	}
	if !strings.Contains(out, "cannot parse config") {
		t.Errorf("a parse error should be surfaced as an error string: %q", out)
	}
}
