package update

import (
	"context"
	"errors"
	"testing"
)

type fakeSource struct {
	rel Release
	err error
}

func (f fakeSource) Latest(context.Context) (Release, error) { return f.rel, f.err }
func (f fakeSource) Download(context.Context, string) ([]byte, error) {
	return nil, errors.New("no download in this test")
}

// RunCommit's early answers, with no network and no binary touched: a source that cannot say, and
// a release that is not newer — the skip that keeps a daemon from re-installing itself.
func TestRunCommitEarlyAnswers(t *testing.T) {
	if _, err := RunCommit(context.Background(), fakeSource{err: errors.New("offline")}, "v1", "/nowhere"); err == nil {
		t.Fatal("a source that cannot say is an error, not a skip")
	}
	res, err := RunCommit(context.Background(), fakeSource{rel: Release{Version: "v1.0.0"}}, "v1.0.0", "/nowhere")
	if err != nil || !res.Updated == false || res.Skipped == "" {
		t.Fatalf("same version skips and says so: (%+v, %v)", res, err)
	}
}
