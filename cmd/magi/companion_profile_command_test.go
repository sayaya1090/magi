package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/companionpolicy"
)

const discoveredProfile = `{"purpose":"Maintain widget library","responsibilities":["Java API"],"tasks":[{"id":"review-widget","description":"Review a named widget against official specification and return evidence","constraints":["No deployment"]}]}`

func TestDiscoveredProfileFillsMissingSectionsWithoutGrantingAuthority(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	var out, errs bytes.Buffer
	policy := fmt.Sprintf("version=1\ninstructions=['Keep user rule']\n[[access]]\nresource=%q\nmode='read'\n[[operations]]\nid='read-source'\ndescription='Read source'\nkind='read_file'\nroot=%q\n", wd, wd)
	if companionPolicyCommand(cfg, wd, true, strings.NewReader(policy), &out, &errs) != 0 {
		t.Fatal(errs.String())
	}
	if companionProfileCommand(cfg, wd, strings.NewReader(discoveredProfile), &out, &errs) != 0 {
		t.Fatal(errs.String())
	}
	got, err := companionpolicy.Load(cfg, wd)
	if err != nil {
		t.Fatal(err)
	}
	if got.Purpose != "Maintain widget library" || len(got.Tasks) != 1 || len(got.Operations) != 1 || len(got.Access) != 1 || got.Access[0].Mode != "read" || got.Instructions[0] != "Keep user rule" {
		t.Fatalf("lost metadata/authority: %+v", got)
	}
	// A later discovery cannot replace existing metadata or rewrite comments/formatting.
	path, err := companionpolicy.Path(cfg, wd)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	original = append([]byte("# User commentary must remain\n"), original...)
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if companionProfileCommand(cfg, wd, strings.NewReader(strings.ReplaceAll(discoveredProfile, "Maintain widget library", "New guessed purpose")), &out, &errs) != 0 {
		t.Fatal(errs.String())
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, original) {
		t.Fatal("a no-op discovery rewrote the user policy")
	}
	after, _ := companionpolicy.Load(cfg, wd)
	if after.Purpose != got.Purpose {
		t.Fatal("replaced populated purpose")
	}
}

func TestDiscoveredProfileRejectsAuthorityAndInvalidTasksWithoutWriting(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	var out, errs bytes.Buffer
	if companionProfileCommand(cfg, wd, strings.NewReader(discoveredProfile), &out, &errs) != 0 {
		t.Fatal(errs.String())
	}
	path, _ := companionpolicy.Path(cfg, wd)
	before, _ := os.ReadFile(path)
	for _, bad := range []string{
		strings.TrimSuffix(discoveredProfile, "}") + `,"access":[{"resource":"/","mode":"write"}]}`,
		strings.TrimSuffix(discoveredProfile, "}") + `,"operations":[]}`,
		strings.Replace(discoveredProfile, "review-widget", "invalid id", 1),
		discoveredProfile + ` {}`, `{"purpose":"guess","tasks":[]}`, `{"purpose":`,
	} {
		if companionProfileCommand(cfg, wd, strings.NewReader(bad), &out, &errs) == 0 {
			t.Fatalf("accepted %s", bad)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("invalid profile changed policy")
		}
	}
}

func TestConcurrentProfileDiscoveryPreservesFirstDeclaredPurpose(t *testing.T) {
	cfg, wd := t.TempDir(), t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out, errs bytes.Buffer
			if companionProfileCommand(cfg, wd, strings.NewReader(discoveredProfile), &out, &errs) != 0 {
				t.Error(errs.String())
			}
		}()
	}
	wg.Wait()
	c, err := companionpolicy.Load(cfg, wd)
	if err != nil || c.Purpose != "Maintain widget library" || len(c.Tasks) != 1 {
		t.Fatal(c, err)
	}
}

func TestSelfDescriptionGuideUsesActualExecutableAndWorkspace(t *testing.T) {
	got := companionSelfDescriptionGuide("/workspace with spaces", "/tools/magi")
	for _, want := range []string{"'/workspace with spaces'", "'/tools/magi' --companion-profile-write", "--companion-policy-show", "not on every turn", "preserves access, operations and instructions"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q", want)
		}
	}
}
