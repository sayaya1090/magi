package atomicfile

import (
	"errors"
	"io/fs"
	"testing"
)

// What counts as a replacement in flight, asked directly, on every platform.
//
// ⚠ **This exists because the behaviour tests beside it cannot fail where the behaviour does not
// happen.** Replace's retry and ReadFile's budget are Windows-only; on every other platform
// ReadFile is one line of os.ReadFile. So TestAMissingFileComesBackAtOnce is green on darwin no
// matter what this predicate says — it is measuring a function that has no retry loop in that
// build. Measured 2026-09-11: fs.ErrNotExist put back into transient, then gofmt, go vet,
// GOOS=windows build, GOOS=windows vet and `go test ./internal/atomicfile/` on a darwin checkout —
// all five green, about a defect costing 200ms per absent record.
//
// CI runs on ubuntu. Without this table, every decision in transient() is unguarded there, and the
// GOOS=windows build+vet step added for the same worry catches only compilation: reverting this
// predicate is a one-line change that compiles and vets clean.
//
// So the errors are fed in by hand. No files, no syscalls, nothing platform-shaped — which is
// precisely what lets it run where the defect cannot.
func TestWhatCountsAsAReplacementInFlight(t *testing.T) {
	for _, c := range []struct {
		name string
		err  error
		want bool
	}{
		{"nothing went wrong", nil, false},
		// The one this table exists for. "Not there" is an ANSWER — daemon.List draws a socket with
		// no record as a row, because something is listening there either way — and waiting the
		// budget out for it costs 200ms per absent record on a list refreshed every two seconds.
		{"the file is not there", fs.ErrNotExist, false},
		{"the file is not there, wrapped", &fs.PathError{Op: "open", Err: fs.ErrNotExist}, false},
		// ERROR_ACCESS_DENIED is what a destination somebody holds open answers with.
		{"access is denied", fs.ErrPermission, true},
		{"access is denied, wrapped", &fs.PathError{Op: "rename", Err: fs.ErrPermission}, true},
		// A file that already exists is a different conversation; nothing here retries on it.
		{"it already exists", fs.ErrExist, false},
		{"something unrelated", errors.New("disk on fire"), false},
	} {
		if got := transient(c.err); got != c.want {
			t.Errorf("%s: transient(%v) = %v, want %v", c.name, c.err, got, c.want)
		}
	}
}

// And the platform's own errors are in the list on the platform that has them.
//
// Named as a count rather than as numbers so this reads the same everywhere: what it holds is that
// a build which NEEDS raw errnos actually registered them — the init in rename_windows.go is easy
// to drop while moving code, and dropping it puts the retry back to never firing, which is exactly
// as visible as no retry at all.
func TestThePlatformsOwnContentionErrorsAreRegistered(t *testing.T) {
	for i, e := range contentionErrnos {
		if e == nil {
			t.Errorf("contentionErrnos[%d] is nil", i)
			continue
		}
		if !transient(e) {
			t.Errorf("contentionErrnos[%d] = %v is registered and transient() says no", i, e)
		}
	}
	t.Logf("이 빌드가 든 경합 errno %d 개: %v", len(contentionErrnos), contentionErrnos)
}
