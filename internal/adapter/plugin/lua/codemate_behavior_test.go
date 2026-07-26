package lua

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// These tests exercise the *shipped* codemate plugin's network-free surface
// through the real host: the per-request LLM-headers callback, the cached-token
// doctor probe, the /login·/logout commands, and the embedded client-key
// deobfuscation. The gateway-touching paths (adsso/gateway/context probes,
// ensure_default_model, find_context_window) all go through magi.http, which the
// bridge gates on net:<host> for the samsungds hosts — a loopback test server is
// a different host and would be denied — so they are intentionally NOT driven
// here; only the token flows that run entirely in-process are asserted.
//
// Token injection: store_get's read precedence is persisted-store >
// config.toml[plugins.<name>] > default, so seeding PluginConfigs["codemate"]
// stands in for a cached token/config without touching disk.

const codematePluginDir = "../../../../plugins/codemate"

// requireCodematePlugin skips when the plugin tree is not on disk. codemate is untracked on
// purpose — it carries internal-network URLs and credentials — so a clean checkout (CI, or any
// machine that has not been handed a copy) simply has no plugins/codemate. These tests assert what
// the *shipped* plugin does; with no plugin there is nothing to assert, and failing would report an
// absent file as a broken plugin. The cost is that CI cannot cover them, which is the price of
// keeping the plugin out of the repository.
func requireCodematePlugin(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(codematePluginDir, "plugin.toml")); err != nil {
		t.Skipf("codemate plugin not present, skipping: %v", err)
	}
}

// makeJWT builds a structurally valid header.payload.signature token whose
// payload carries the given exp (unix seconds). base64url, no padding — exactly
// what jwt_struct_ok / jwt_exp parse.
func makeJWT(exp int64) string {
	b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
	header := b64([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := b64([]byte(fmt.Sprintf(`{"exp":%d,"preferred_username":"tester"}`, exp)))
	sig := b64([]byte("sig"))
	return header + "." + payload + "." + sig
}

// loadCodemate loads the shipped plugin with the given [plugins.codemate] config
// and (optional) data dir, wiring a fakeLLMReg so the header callback is captured.
func loadCodemate(t *testing.T, section map[string]any, dataDir string) (*Host, *fakeLLMReg) {
	t.Helper()
	requireCodematePlugin(t)
	llm := &fakeLLMReg{}
	cfg := HostConfig{
		ToolSink: builtin.NewRegistry(),
		LLMReg:   llm,
		Runtime:  RuntimeInfo{Workdir: t.TempDir()},
		Logf:     func(string) {},
		DataDir:  dataDir,
	}
	if section != nil {
		cfg.PluginConfigs = map[string]map[string]any{"codemate": section}
	}
	h := NewHostWithConfig(cfg)
	if _, err := h.Load(context.Background(), codematePluginDir); err != nil {
		t.Fatalf("codemate load: %v", err)
	}
	if len(llm.fns) != 1 {
		t.Fatalf("expected exactly one llm-headers fn, got %d", len(llm.fns))
	}
	return h, llm
}

func probeByName(t *testing.T, h *Host, name string) port.DoctorProbe {
	t.Helper()
	for _, p := range h.DoctorProbes() {
		if p.Name() == name {
			return p
		}
	}
	t.Fatalf("doctor probe %q not registered", name)
	return nil
}

// The per-request header callback attaches Authorization only for a valid,
// unexpired token, and always carries the client API key. It re-reads the token
// each call (saved_token), so structurally-broken or expired tokens fall back to
// key-only headers rather than sending a bad bearer.
func TestCodemateHeaderCallbackTokenStates(t *testing.T) {
	const far = 4102444800  // 2100-01-01
	const past = 1000000000 // 2001-09
	cases := []struct {
		name     string
		token    any // nil = key absent from config entirely
		wantAuth bool
	}{
		{"valid future-exp", makeJWT(far), true},
		{"expired", makeJWT(past), false},
		{"malformed two-segment", "aaaa.bbbb", false},
		{"empty string", "", false},
		{"absent", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			section := map[string]any{}
			if tc.token != nil {
				section["token"] = tc.token
			}
			_, llm := loadCodemate(t, section, "")
			h := llm.fns[0]()

			if got := h["X-Client-API-Key"]; got == "" {
				t.Errorf("client API key must always be present, got empty headers %v", h)
			}
			_, hasAuth := h["Authorization"]
			if hasAuth != tc.wantAuth {
				t.Errorf("Authorization present=%v, want %v (headers=%v)", hasAuth, tc.wantAuth, h)
			}
			if tc.wantAuth {
				if want := "Bearer " + tc.token.(string); h["Authorization"] != want {
					t.Errorf("Authorization=%q, want %q", h["Authorization"], want)
				}
			}
		})
	}
}

// The embedded client key is obfuscated in init.lua (xor8 + hex blob). With no
// user override it must deobfuscate to a stable, non-empty value; a user-supplied
// [plugins.codemate] client_api_key overrides it verbatim.
func TestCodemateClientKeyDeobfuscationAndOverride(t *testing.T) {
	// Default: deobfuscation yields a non-empty key, stable across calls.
	_, llm := loadCodemate(t, nil, "")
	k1 := llm.fns[0]()["X-Client-API-Key"]
	k2 := llm.fns[0]()["X-Client-API-Key"]
	if k1 == "" {
		t.Fatal("default (obfuscated) client key deobfuscated to empty")
	}
	if k1 != k2 {
		t.Errorf("client key not stable across calls: %q vs %q", k1, k2)
	}

	// Override: an explicit config key flows through unchanged.
	_, llm2 := loadCodemate(t, map[string]any{"client_api_key": "known-test-key"}, "")
	if got := llm2.fns[0]()["X-Client-API-Key"]; got != "known-test-key" {
		t.Errorf("override client key = %q, want known-test-key", got)
	}
}

// The "codemate token" doctor probe is a pure local check of the cached token's
// health — it must map each token state to the right (status, detail) without
// touching the network.
func TestCodemateTokenProbeHealth(t *testing.T) {
	const far = 4102444800
	const past = 1000000000
	cases := []struct {
		name        string
		token       any
		wantStatus  string
		detailMatch string
	}{
		{"no token", nil, "info", "not logged in"},
		{"valid", makeJWT(far), "ok", "valid"},
		{"expired", makeJWT(past), "warn", "expired"},
		{"malformed", "aaaa.bbbb", "fail", "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			section := map[string]any{}
			if tc.token != nil {
				section["token"] = tc.token
			}
			h, _ := loadCodemate(t, section, "")
			status, detail := probeByName(t, h, "codemate token").Run(context.Background())
			if status != tc.wantStatus {
				t.Errorf("status=%q, want %q (detail=%q)", status, tc.wantStatus, detail)
			}
			if !strings.Contains(detail, tc.detailMatch) {
				t.Errorf("detail=%q, want it to contain %q", detail, tc.detailMatch)
			}
		})
	}
}

// The plugin registers exactly the /login and /logout slash commands, each with a
// description surfaced in the palette.
func TestCodemateCommandsRegistered(t *testing.T) {
	h, _ := loadCodemate(t, nil, "")
	got := map[string]string{}
	for _, c := range h.PluginCommands() {
		got[c.Name()] = c.Description()
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 commands, got %d: %v", len(got), got)
	}
	for _, name := range []string{"login", "logout"} {
		if got[name] == "" {
			t.Errorf("command %q missing or has no description (got %v)", name, got)
		}
	}
}

// /logout clears the cached token and returns the session to the splash. With a
// data dir configured, store_set persists an empty token which — by store_get's
// persisted-over-config precedence — masks the seeded config token, so the header
// callback stops sending Authorization. It also queues the one-shot
// clear_transcript UI effect.
func TestCodemateLogoutClearsToken(t *testing.T) {
	token := makeJWT(4102444800)
	h, llm := loadCodemate(t, map[string]any{"token": token}, t.TempDir())

	// Before logout: the seeded (config) token is live.
	if _, ok := llm.fns[0]()["Authorization"]; !ok {
		t.Fatal("expected Authorization before logout with a valid seeded token")
	}

	handled, err := h.DispatchCommand("logout", nil)
	if !handled || err != nil {
		t.Fatalf("logout dispatch handled=%v err=%v", handled, err)
	}

	// The persisted empty token now overrides the config token: no bearer.
	if got, ok := llm.fns[0]()["Authorization"]; ok {
		t.Errorf("Authorization should be gone after logout, got %q", got)
	}
	// Logout returns to the splash via a single clear_transcript effect.
	effects := h.TakeUIEffects()
	if len(effects) != 1 || effects[0] != "clear_transcript" {
		t.Errorf("UI effects = %v, want [clear_transcript]", effects)
	}
}
