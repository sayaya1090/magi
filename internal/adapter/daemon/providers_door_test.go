package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// providerEngine answers the providers door: the backends serving on this machine right now.
type providerEngine struct {
	omniEngine
	list []ProviderChoice
}

func (e *providerEngine) ProvidersHere(context.Context) ([]ProviderChoice, error) { return e.list, nil }

// The IDE clients talk only to the socket, so the provider picker the web console and the TUI
// build from provider.Discover has to cross it: name, address and models, as the engine said them.
func TestTheProvidersDoorCrossesTheSocket(t *testing.T) {
	home, err := os.MkdirTemp(shortRoot(), "mgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	sock := filepath.Join(home, "daemon-prov.sock")
	d, err := Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	eng := &providerEngine{list: []ProviderChoice{
		{Name: "claudecode", Base: "http://127.0.0.1:53345/v1", Models: []string{"opus", "sonnet"}},
		{Name: "codex", Base: "http://127.0.0.1:55545/v1", Models: []string{"codex-default"}},
	}}
	go func() { _ = d.Serve(context.Background(), eng) }()
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := c.Providers()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "claudecode" || got[0].Base != "http://127.0.0.1:53345/v1" ||
		strings.Join(got[0].Models, ",") != "opus,sonnet" || got[1].Name != "codex" {
		t.Fatalf("the providers arrived as %+v", got)
	}
}

// A daemon that cannot list them says so, rather than answering an empty roster that reads as
// "nothing is serving".
func TestAnEngineWithoutProvidersRefusesTheDoor(t *testing.T) {
	resp := answerProviders(context.Background(), &omniEngine{}, Request{Method: "providers"})
	if resp.OK || !strings.Contains(resp.Err, "providers") {
		t.Fatalf("the door answered %+v", resp)
	}
}
