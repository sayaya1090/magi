package jsonl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// A viewer that started first must still find a log another process wrote afterwards.
//
// That is the whole reason `locate` exists — the index is built at New, and the comment above Read
// names the case: "a viewer started before the daemon it is watching hits exactly this". It looked
// the log up with `filepath.Glob`, which takes the WHOLE path as a pattern, so the data directory
// in front of the `*` was a pattern too. A `[` in that directory — an account name, a MAGI_DATA_DIR
// somebody chose — became a character class, and the answer was no match and no error.
//
// Read turns that into `nil, nil`. The viewer draws an EMPTY conversation for one it simply could
// not find, and nothing anywhere says which. Measured 2026-09-11.
//
// The log is written by hand rather than through Append, because writing it through this Store
// would put it in the index and the fallback would never run — the bug lives in the path that only
// a second process reaches.
func TestAViewerFindsALogWrittenAfterItStarted(t *testing.T) {
	for _, dirName := range []string{"data", "user[1]", "a b", "plain.d"} {
		t.Run(dirName, func(t *testing.T) {
			data := filepath.Join(t.TempDir(), dirName)
			if err := os.MkdirAll(data, 0o755); err != nil {
				t.Skipf("이 파일시스템은 %q 라는 이름을 못 만든다: %v", dirName, err)
			}
			viewer, err := New(data) // indexed while there is nothing to index
			if err != nil {
				t.Fatal(err)
			}

			// Another process writes a session log.
			proj := filepath.Join(data, "projects", "someproject")
			if err := os.MkdirAll(proj, 0o755); err != nil {
				t.Fatal(err)
			}
			const line = `{"seq":1,"sessionId":"s_late","type":"session.created",` +
				`"ts":"2026-09-11T00:00:00Z","data":{"workdir":"/w"}}` + "\n"
			if err := os.WriteFile(filepath.Join(proj, "s_late.jsonl"), []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}

			evs, err := viewer.Read(context.Background(), session.SessionID("s_late"), 0)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(evs) != 1 {
				t.Errorf("나중에 쓰인 로그를 못 찾았다: 이벤트 %d개 — 뷰어는 이것을 「빈 대화」로 그린다", len(evs))
			}
		})
	}
}

// A session that genuinely is not there is still not there.
//
// Loosening how the lookup reads a directory must not loosen what counts as a hit: the id has to
// match a file exactly, not as a pattern.
func TestALogThatIsNotThereIsStillNotFound(t *testing.T) {
	data := t.TempDir()
	proj := filepath.Join(data, "projects", "someproject")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "s_real.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory, not a log, under the same name — a lookup that only checks existence would
	// hand back something nothing can read.
	if err := os.MkdirAll(filepath.Join(proj, "s_dir.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := New(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, sid := range []string{"s_missing", "s_*", "s_[r]eal", "s_dir"} {
		if path, found := s.locate(session.SessionID(sid)); found {
			t.Errorf("%q 를 찾았다고 답했다: %q", sid, path)
		}
	}
	if _, found := s.locate("s_real"); !found {
		t.Error("실제로 있는 로그를 못 찾았다")
	}
}
