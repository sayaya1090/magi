package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestPromptAdmissionRejectsClosedAndUpdateHeldWithoutWriting(t *testing.T) {
	for _, closed := range []bool{false, true} {
		for _, steer := range []bool{false, true} {
			store, err := jsonl.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			a := New(store, nil, nil, bus.New(), nil, Config{})
			ctx := context.Background()
			sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := store.Read(ctx, sid, 0)
			want := ErrUpdateHeld
			if closed {
				if err := a.Close(ctx); err != nil {
					t.Fatal(err)
				}
				want = ErrShuttingDown
			} else {
				release, ok := a.HoldForUpdate()
				if !ok {
					t.Fatal("hold failed")
				}
				defer release()
			}
			c := command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "must not be saved"}}}
			if steer {
				err = a.Steer(ctx, c)
			} else {
				err = a.Submit(ctx, c)
			}
			if !errors.Is(err, want) {
				t.Fatalf("closed=%v steer=%v: %v", closed, steer, err)
			}
			after, _ := store.Read(ctx, sid, 0)
			if len(after) != len(before) {
				t.Fatal("rejected prompt changed transcript")
			}
		}
	}
}

type blockedPromptStore struct {
	port.Store
	entered, release chan struct{}
	once             sync.Once
}

func (s *blockedPromptStore) Append(ctx context.Context, id session.SessionID, evs ...event.Event) ([]int64, error) {
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			s.once.Do(func() { close(s.entered) })
			<-s.release
			break
		}
	}
	return s.Store.Append(ctx, id, evs...)
}

func TestCloseDrainsAdmittedPromptAndUpdateCannotPassIt(t *testing.T) {
	ctx := context.Background()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, nil, bus.New(), nil, Config{})
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	blocked := &blockedPromptStore{Store: a.store, entered: make(chan struct{}), release: make(chan struct{})}
	a.store = blocked
	submitted := make(chan error, 1)
	go func() {
		submitted <- a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "accepted before shutdown"}}})
	}()
	released := false
	defer func() {
		if !released {
			close(blocked.release)
		}
		a.Close(context.Background())
	}()
	select {
	case <-blocked.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt did not reach storage")
	}
	if release, ok := a.HoldForUpdate(); ok {
		release()
		t.Fatal("update passed pending prompt persistence")
	}
	deadline, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if err := a.Close(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close did not wait for prompt: %v", err)
	}
	close(blocked.release)
	released = true
	if err := <-submitted; err != nil {
		t.Fatal(err)
	}
	if err := a.Close(ctx); err != nil {
		t.Fatal(err)
	}
	evs, err := store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("accepted prompt count=%d", count)
	}
}
