package builtin

import (
	"reflect"
	"testing"
)

// A file that DEFINES the queried symbol outranks one that only mentions it,
// and camelCase queries are split into subtokens.
func TestFindContextDefinitionBoost(t *testing.T) {
	got, isErr := runJSON(t, FindContext{}, findCtxArgs{Query: "parseConfig"}, func(d string) {
		writeFile(d, "core/loader.go", "package core\n\nfunc parseConfig(path string) error { return nil }\n")
		writeFile(d, "app/run.go", "package app\n\nfunc Run() { parseConfig(\"x\"); parseConfig(\"y\") }\n")
	})
	if isErr || len(got) == 0 {
		t.Fatalf("findcontext: got %v err=%v", got, isErr)
	}
	top := got[0].(map[string]any)["path"].(string)
	if top != "core/loader.go" {
		t.Errorf("definer should rank first; got %q", top)
	}
}

// The top result pinpoints the symbol's definition: name + 1-based line.
func TestFindContextReturnsDefinitionSite(t *testing.T) {
	got, isErr := runJSON(t, FindContext{}, findCtxArgs{Query: "parseConfig"}, func(d string) {
		writeFile(d, "core/loader.go", "package core\n\nfunc parseConfig(path string) error { return nil }\n")
	})
	if isErr || len(got) == 0 {
		t.Fatalf("findcontext: got %v err=%v", got, isErr)
	}
	top := got[0].(map[string]any)
	if ln, _ := top["line"].(float64); int(ln) != 3 {
		t.Errorf("definition line should be 3; got %v", top["line"])
	}
	if sym, _ := top["symbol"].(string); sym != "parseConfig" {
		t.Errorf("symbol should be parseConfig; got %q", top["symbol"])
	}
}

// An exact symbol-name definition outranks a file that merely defines an
// unrelated symbol while mentioning the term in a comment/body.
func TestFindContextExactSymbolOutranksMention(t *testing.T) {
	got, isErr := runJSON(t, FindContext{}, findCtxArgs{Query: "retryBackoff"}, func(d string) {
		writeFile(d, "net/retry.go", "package net\n\nfunc retryBackoff(n int) {}\n")
		// Defines a different symbol but name-drops retryBackoff in a comment + call.
		writeFile(d, "net/client.go", "package net\n\n// uses retryBackoff for retries\nfunc Do() { retryBackoff(3) }\n")
	})
	if isErr || len(got) == 0 {
		t.Fatalf("findcontext: got %v err=%v", got, isErr)
	}
	if top := got[0].(map[string]any)["path"].(string); top != "net/retry.go" {
		t.Errorf("exact symbol definer should rank first; got %q", top)
	}
}

func TestDefNames(t *testing.T) {
	cases := map[string]string{
		"func parseConfig(path string) error {": "parseConfig",
		"func (r *Repo) Save() error {":         "Save", // Go method: skip receiver
		"def handle_request(self):":             "handle_request",
		"class HttpClient:":                     "HttpClient",
		"export const apiTimeout = 30":          "apiTimeout",
		"pub fn compute_hash() {}":              "compute_hash",
	}
	for line, want := range cases {
		got := defNames(line)
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("defNames(%q)=%v, want to contain %q", line, got, want)
		}
	}
}

func TestKeywordsTokenization(t *testing.T) {
	// camelCase + snake_case split into subtokens; stopwords dropped; <3 dropped.
	got := keywords("parseConfig from the user_id value")
	want := map[string]bool{"parseconfig": true, "parse": true, "config": true, "user": true}
	for w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Errorf("expected term %q in %v", w, got)
		}
	}
	for _, g := range got {
		if g == "the" || g == "from" || g == "value" {
			t.Errorf("stopword %q should be dropped: %v", g, got)
		}
	}
}

func TestSplitCamel(t *testing.T) {
	if g := splitCamel("parseConfigV2"); !reflect.DeepEqual(g, []string{"parse", "Config", "V", "2"}) {
		t.Errorf("splitCamel=%v", g)
	}
}
