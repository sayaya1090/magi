package daemon

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/atomicfile"
)

// What a companion was asked to take on, kept long enough to argue with.
//
// # Why the live number is not enough
//
// A record says how much is waiting right now, and that is what an asker needs: it is choosing
// between companions this minute. It is exactly the wrong thing for the other question — whether
// one of this companion is enough — because that question is about a pattern, and a pattern is
// invisible in an instant. A companion that is full every afternoon and empty every evening reads
// as idle to anybody who happens to look in the evening.
//
// So the moments are written down: each piece taken, each one refused because there was no room.
// Read back over a week they are the case for running a second copy, or the case that the queue
// nobody complained about was never the problem.
//
// # Where, and why not with the delegation record
//
// Beside the socket, with the published record, and not in the workspace where the verdicts on
// handovers go. Those two files answer different questions and belong to different people: a
// verdict is a judgement about a companion's work, is worth keeping forever, and is the same fact
// on every machine — it is committed. This is operational, is about one process on one machine, and
// stops being true about the future the moment anything changes, so it decays.
//
// Named off the socket rather than the daemon's name, because a socket path is unique on a machine
// and a name is not required to be. Kept when the daemon stops, unlike the published record: the
// week after a companion was killed is exactly when somebody asks whether it was overloaded.

// LoadFile is where the daemon at that socket writes what it was asked to take on.
func LoadFile(socketPath string) string { return socketPath + ".load" }

// LoadKept is how long a moment stays readable.
//
// A month, which is longer than the argument it feeds needs and short enough that the file stays a
// file. Load is a claim about how a companion is being used now; last quarter's rush is history,
// and history that outlives its relevance is read as if it were current.
const LoadKept = 30 * 24 * time.Hour

// loadLine is one moment: work arrived, and this is what happened to it.
type loadLine struct {
	At   time.Time `json:"at"`
	Name string    `json:"name,omitempty"`
	// Full is the refusal — the queue was at its limit and the asker was sent away. The count of
	// these is the whole point of the file.
	Full bool `json:"full,omitempty"`
	// Ahead is how many pieces were already waiting when this one arrived. Zero means it went
	// straight to work, which is a companion that is keeping up.
	Ahead int `json:"ahead,omitempty"`
}

// NoteLoad writes down one arrival.
//
// Errors are returned and are expected to be dropped by the caller: this is a record of how busy a
// companion was, and failing a handover because the note could not be written would make the
// measurement more damaging than the thing it measures.
func NoteLoad(socketPath, name string, full bool, ahead int) error {
	b, err := json.Marshal(loadLine{At: time.Now().UTC(), Name: name, Full: full, Ahead: ahead})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(LoadFile(socketPath), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(b, '\n'))
	return err
}

// Pressure is what one companion's file adds up to over a window.
type Pressure struct {
	Name    string
	Socket  string
	Taken   int
	Refused int
	// Deepest is the most that was ever waiting in front of an arriving piece. It separates a
	// companion that was briefly busy from one that runs at its limit.
	Deepest int
	First   time.Time
	Last    time.Time
}

// Busy reports whether there is anything here worth telling somebody about.
//
// Work that arrived and started immediately, every time, is a companion doing its job. Printing a
// line for it would bury the ones that are not.
func (p Pressure) Busy() bool { return p.Refused > 0 || p.Deepest > 0 }

// LoadSince reads what every companion on this machine was asked to take on, since a moment.
//
// Includes companions that are no longer running, deliberately: their files are not removed when
// they stop, and the week after one was killed is when somebody asks whether it was overloaded.
func LoadSince(configDir string, since time.Time) []Pressure {
	paths, err := filepath.Glob(filepath.Join(configDir, "daemon-*.sock.load"))
	if err != nil {
		return nil
	}
	out := make([]Pressure, 0, len(paths))
	for _, path := range paths {
		sock := strings.TrimSuffix(path, ".load")
		p := Pressure{Socket: sock}
		for _, l := range readLoad(path) {
			if l.At.Before(since) {
				continue
			}
			if l.Name != "" {
				p.Name = l.Name
			}
			if l.Full {
				p.Refused++
			} else {
				p.Taken++
			}
			if l.Ahead > p.Deepest {
				p.Deepest = l.Ahead
			}
			if p.First.IsZero() || l.At.Before(p.First) {
				p.First = l.At
			}
			if l.At.After(p.Last) {
				p.Last = l.At
			}
		}
		if p.Taken+p.Refused == 0 {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Socket < out[j].Socket
	})
	return out
}

// PruneLoad drops moments older than the window. Called when a daemon starts, which is often
// enough: the file grows by a line per request, and nothing reads past the window anyway.
func PruneLoad(socketPath string, before time.Time) error {
	path := LoadFile(socketPath)
	lines := readLoad(path)
	kept := make([]byte, 0, len(lines)*96)
	dropped := 0
	for _, l := range lines {
		if l.At.Before(before) {
			dropped++
			continue
		}
		b, err := json.Marshal(l)
		if err != nil {
			continue
		}
		kept = append(kept, append(b, '\n')...)
	}
	if dropped == 0 {
		return nil // nothing to do, and rewriting would be a write per daemon start for nothing
	}
	if len(kept) == 0 {
		return os.Remove(path)
	}
	return atomicfile.Write(path, kept, 0o600)
}

// readLoad reads a file of moments, skipping anything unreadable.
//
// A half-written last line is the normal state of an append-only file whose writer was killed
// mid-write, and it is not a reason to lose the month in front of it.
func readLoad(path string) []loadLine {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []loadLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 1<<16)
	for sc.Scan() {
		var l loadLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		out = append(out, l)
	}
	return out
}
