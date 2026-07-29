package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// A file bigger than the read cap is two facts, and the note used to merge them into one that was
// false: magi read the first 10 MiB OFF DISK, and it showed whatever window the caller asked for.
// Observed live (large-scale-text-editing, 2026-07-29): `read{path, limit:"50.0"}` on a large CSV
// delivered 50 lines / 2165 bytes and was told "showing first 10 MiB — use offset/limit to page",
// advice for a caller that had just used limit.
//
// The same conflation reaches further: total counts the lines of what was READ, so on a capped read
// "file has N lines" is a claim about the first 10 MiB wearing the file's name.
func TestACappedReadDescribesTheReadNotTheWindow(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.csv")
	var b strings.Builder
	for i := 0; b.Len() <= maxReadBytes+4096; i++ {
		fmt.Fprintf(&b, "row%d,alpha,beta,gamma,delta,epsilon,zeta,eta,theta,iota,kappa\n", i)
	}
	if err := os.WriteFile(big, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	env := port.ToolEnv{Workdir: dir}
	// The tool's content is a JSON document, so decode it: the gutter's tabs and newlines are
	// escaped in the raw bytes and a substring match against them would silently never fire.
	read := func(args string) string {
		res, err := Read{}.Execute(context.Background(), json.RawMessage(args), env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if err := json.Unmarshal(res.Content, &s); err != nil {
			t.Fatalf("tool content is not a JSON string: %v", err)
		}
		return s
	}

	// A 50-line window out of a capped file: say what was read, never that 10 MiB was shown.
	got := read(`{"path":` + strconv.Quote(big) + `,"limit":"50.0"}`)
	if strings.Contains(got, "showing first") {
		t.Errorf("the window was 50 lines, not ten megabytes:\n%s", got[:min(400, len(got))])
	}
	if !strings.Contains(got, "read only its first 10 MiB") {
		t.Errorf("the cap is still a fact worth stating:\n%s", got[:min(400, len(got))])
	}
	// The caller supplied a limit; telling it to use one reads as if its own was ignored.
	if strings.Contains(got, "use offset/limit to page") {
		t.Errorf("advice for something the caller already did:\n%s", got[:min(400, len(got))])
	}
	// The window itself is still delivered, numbered, and bounded by the caller's own limit.
	if !strings.Contains(got, "1\trow0,") || !strings.Contains(got, "50\trow49,") {
		t.Errorf("the asked-for window must be there, numbered:\n%s", got[:min(400, len(got))])
	}
	if strings.Contains(got, "51\trow50,") {
		t.Errorf("limit=50 means fifty lines:\n%s", got[:min(400, len(got))])
	}

	// An offset past what was read is not past the end of the FILE — magi never saw the end.
	over := read(`{"path":` + strconv.Quote(big) + `,"offset":"9000000","limit":"20"}`)
	if strings.Contains(over, "past end of file") {
		t.Errorf("magi did not read the end and cannot say the offset is past it:\n%s", over)
	}
	for _, want := range []string{"past the end of the first 10 MiB", "cannot say whether the file has more"} {
		if !strings.Contains(over, want) {
			t.Errorf("missing %q:\n%s", want, over)
		}
	}

	// The control: a file under the cap keeps the plain wording, because there the read IS the file.
	small := filepath.Join(dir, "small.txt")
	if err := os.WriteFile(small, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := read(`{"path":` + strconv.Quote(small) + `,"offset":"99","limit":"10"}`)
	if !strings.Contains(s, "past end of file; file has 3 lines") {
		t.Errorf("an uncapped read knows the file's length and says so:\n%s", s)
	}
	if strings.Contains(s, "10 MiB") {
		t.Errorf("nothing was capped here:\n%s", s)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
