package daemon

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// A meeting is one session per participant, and the daemon is where that is remembered.
//
// Every contribution used to be its own child: three companions over five rounds put fifteen of
// them on the strip, each starting cold and knowing nothing of what this participant had already
// said. The id travels with the request so the daemon can hand the same session back.
type roomEngine struct {
	// Everything a daemon has to be, plus the two methods this test is about. Embedded rather than
	// reimplemented: what is being checked is the meeting id crossing the socket, not the engine.
	fakeEngine
	mu     sync.Mutex
	joined []string
	turns  []string // the meeting id each turn arrived with
}

func (e *roomEngine) MeetingJoin(ctx context.Context, meeting, topic string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.joined = append(e.joined, meeting)
	return "ready: " + topic, nil
}

func (e *roomEngine) MeetingTurn(ctx context.Context, meeting, topic, transcript string, closing bool) (
	string, bool, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.turns = append(e.turns, meeting)
	return "said something", false, nil
}

func TestAMeetingTurnCarriesWhichMeetingItIs(t *testing.T) {
	eng := &roomEngine{}
	sock, _ := serveOn(t, eng)
	cl := dialWhenUp(t, sock)
	defer cl.Close()

	ready, err := cl.Join("m-1", "who owns the retry budget")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ready, "retry budget") {
		t.Errorf("joining answered %q — it is what the participant brings", ready)
	}
	for _, id := range []string{"m-1", "m-1", "m-2"} {
		if _, _, err := cl.Meet(id, "topic", "so far", false); err != nil {
			t.Fatal(err)
		}
	}
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if len(eng.joined) != 1 || eng.joined[0] != "m-1" {
		t.Errorf("joining was recorded as %v", eng.joined)
	}
	// The id is the whole point: without it the daemon cannot tell one meeting's session from
	// another's, and every turn is a new child again.
	if got := strings.Join(eng.turns, ","); got != "m-1,m-1,m-2" {
		t.Errorf("the turns arrived as %q", got)
	}
}
