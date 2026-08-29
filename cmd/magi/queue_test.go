package main

import (
	"sync"

	"testing"
)

// The queue says how deep it is, every time that changes.
//
// Nothing outside this process can see a queue held in memory, so the number has to be pushed to
// somewhere a reader can find it. Pushed on every change rather than sampled, because the reader is
// on another machine a gossip round away and the sampling interval would be added to the staleness.
func TestTheQueueSaysHowDeepItIsWheneverThatChanges(t *testing.T) {
	var said []int
	w := newWaiting(func(n int, _ bool) { said = append(said, n) })

	for i := 0; i < 3; i++ {
		if _, ok := w.take(pending{receipt: "r"}); !ok {
			t.Fatalf("piece %d was refused by an empty queue", i)
		}
	}
	if _, ok := w.next(); !ok {
		t.Fatal("nothing came out of a queue with three pieces in it")
	}
	want := []int{1, 2, 3, 2}
	if len(said) != len(want) {
		t.Fatalf("the depth was said %v, wanted %v", said, want)
	}
	for i := range want {
		if said[i] != want[i] {
			t.Fatalf("the depth was said %v, wanted %v", said, want)
		}
	}

	// An empty queue says so. Otherwise the last thing anybody heard about a companion that has
	// caught up is the depth it was at when it was behind, and it would look busy until it next
	// took work — which is exactly when nobody should be discouraged from asking it.
	for i := 0; i < 2; i++ {
		if _, ok := w.next(); !ok {
			t.Fatalf("the queue emptied early, with %d still in it", 2-i)
		}
	}
	if _, ok := w.next(); ok {
		t.Fatal("something came out of an empty queue")
	}
	if last := said[len(said)-1]; last != 0 {
		t.Errorf("after draining, the depth stands at %d", last)
	}
}

// A refusal is not a change, and an empty queue asked again is not either.
//
// Both would be a write to the published record for nothing, and the record is a file every reader
// of the roster opens.
func TestNothingIsAnnouncedWhenTheDepthDidNotMove(t *testing.T) {
	var count int
	w := newWaiting(func(int, bool) { count++ })
	for i := 0; i < maxWaiting; i++ {
		w.take(pending{receipt: "r"})
	}
	filled := count
	if _, ok := w.take(pending{receipt: "one too many"}); ok {
		t.Fatal("the queue took more than it holds")
	}
	if count != filled {
		t.Errorf("a refusal was announced as a change in depth")
	}
}

// Saying the depth does not hold the queue shut.
//
// Whatever is on the other end writes a file. A mutex held across that would put a disk write
// between one asker and the next, and anything that reacted by looking at the queue would deadlock
// — which is what this asserts, by doing exactly that.
func TestSayingTheDepthDoesNotHoldTheQueueShut(t *testing.T) {
	done := make(chan struct{})
	var w *waiting
	w = newWaiting(func(int, bool) {
		w.where("r")
		w.givenUp("r")
	})
	go func() {
		defer close(done)
		w.take(pending{receipt: "r"})
		w.next()
	}()
	<-done
}

// Two pieces run at once by design — a looking piece beside a writing one — and the busy flag
// must outlive the FIRST to end, not the last. As a bool it was cleared by whichever piece
// finished first, and the published record said "free" about a companion mid-change.
func TestOverlappingPiecesKeepTheBusyFlagUntilTheLastEnds(t *testing.T) {
	type ann struct {
		n    int
		hand bool
	}
	var said []ann
	w := newWaiting(func(n int, h bool) { said = append(said, ann{n, h}) })
	w.began() // a writing piece starts
	w.began() // a looking piece starts beside it
	w.ended() // the looking piece finishes first
	if last := said[len(said)-1]; !last.hand {
		t.Fatal("one piece ended and the advertisement said free while the other still runs")
	}
	w.ended()
	if last := said[len(said)-1]; last.hand {
		t.Fatal("all pieces ended and the advertisement still says busy")
	}
}

// The LAST word about busy matches the state. Publishing outside the state lock let two
// concurrent ended()s land in the wrong order and leave "busy" as the final record about an
// idle companion — permanent, since nothing else announces until the next handover.
func TestTheLastWordAboutBusyMatchesTheState(t *testing.T) {
	type ann struct {
		n    int
		hand bool
	}
	var mu sync.Mutex
	var said []ann
	w := newWaiting(func(n int, h bool) { mu.Lock(); said = append(said, ann{n, h}); mu.Unlock() })
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		w.began()
	}
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); w.ended() }()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if last := said[len(said)-1]; last.hand {
		t.Fatalf("everything ended and the final published word is still busy (%v)", said[len(said)-8:])
	}
}
