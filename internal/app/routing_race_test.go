package app

import (
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// providerFor is read from every model-request goroutine while SetProfile writes a.providers from the
// TUI thread (a /profile edit). Both must be guarded by a.mu — run under `go test -race` this fails if
// providerFor reads the map unlocked. (A plain run just exercises that neither path panics.)
func TestProviderForNoRaceWithSetProfile(t *testing.T) {
	a := newOrchApp(t, nil, Config{
		Permission:  "allow",
		NewProvider: func(p ProfileDef) port.LLMProvider { return nil }, // factory populates a.providers
	})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			a.SetProfile(ProfileDef{Name: "p", Model: "m"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 300; i++ {
			_ = a.providerFor(AgentSpec{Provider: "p"})
			_ = a.providerFor(AgentSpec{}) // the default (a.llm) branch too
		}
	}()
	wg.Wait()
}
