package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Error/edge paths across the file & shell tools.
func TestToolEdgeCases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := port.ToolEnv{Workdir: dir}
	run := func(tl port.Tool, args string) (string, bool) {
		r, _ := tl.Execute(ctx, json.RawMessage(args), env)
		return resultText(t, r), r.IsError
	}

	// write creates parent dirs and the file round-trips through read.
	if _, isErr := run(Write{}, `{"path":"a/b/c.txt","content":"hi\nthere"}`); isErr {
		t.Fatal("write into a new nested dir should succeed")
	}
	if out, isErr := run(Read{}, `{"path":"a/b/c.txt"}`); isErr || !strings.Contains(out, "hi") {
		t.Errorf("read back = %q (err=%v)", out, isErr)
	}

	// read: missing file, and a directory, both error.
	if _, isErr := run(Read{}, `{"path":"nope.txt"}`); !isErr {
		t.Error("reading a missing file should error")
	}
	if _, isErr := run(Read{}, `{"path":"a"}`); !isErr {
		t.Error("reading a directory should error")
	}

	// read with offset past EOF returns no content but not an error.
	if _, isErr := run(Read{}, `{"path":"a/b/c.txt","offset":999}`); isErr {
		t.Error("offset past EOF should not error")
	}

	// edit: old == new is rejected as a no-op.
	if _, isErr := run(Edit{}, `{"path":"a/b/c.txt","old":"hi","new":"hi"}`); !isErr {
		t.Error("edit with old==new should error")
	}
	// edit: a non-existent match errors.
	if _, isErr := run(Edit{}, `{"path":"a/b/c.txt","old":"NOPE","new":"x"}`); !isErr {
		t.Error("edit with no match should error")
	}

	// list: a non-directory path errors.
	if _, isErr := run(List{}, `{"path":"a/b/c.txt"}`); !isErr {
		t.Error("list on a file should error")
	}

	// grep: an invalid regex errors.
	if _, isErr := run(Grep{}, `{"pattern":"[unclosed"}`); !isErr {
		t.Error("grep with an invalid regex should error")
	}

	// glob: a pattern that matches nothing returns an empty list, not an error.
	if out, isErr := run(Glob{}, `{"pattern":"**/*.nonesuch"}`); isErr {
		t.Errorf("glob with no matches should not error: %q", out)
	}

	// bash: a blank command errors before running anything.
	if _, isErr := run(Bash{}, `{"command":"   "}`); !isErr {
		t.Error("blank bash command should error")
	}

	// webfetch: a non-http(s) scheme errors.
	if _, isErr := run(WebFetch{}, `{"url":"ftp://example.com/x"}`); !isErr {
		t.Error("webfetch with a non-http scheme should error")
	}

	// every tool rejects malformed JSON args cleanly (no panic).
	for _, tl := range []port.Tool{Read{}, Write{}, Edit{}, MultiEdit{}, Grep{}, Glob{}, List{}, Bash{}, WebFetch{}} {
		if _, isErr := run(tl, `}{`); !isErr {
			t.Errorf("%s should reject malformed args", tl.Name())
		}
	}

	// sanity: the file really was created on disk.
	if _, err := os.Stat(filepath.Join(dir, "a", "b", "c.txt")); err != nil {
		t.Errorf("expected file on disk: %v", err)
	}
}
