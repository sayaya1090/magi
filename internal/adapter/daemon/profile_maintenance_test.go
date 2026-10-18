package daemon

import (
	"context"
	"testing"
)

type maintenanceEngine struct {
	*fakeEngine
	session string
	refresh bool
}

func (e *maintenanceEngine) ProfileMaintenanceStatus(_ context.Context, sid string, refresh bool) (string, error) {
	e.session = sid
	e.refresh = refresh
	return `{"id":"internal","running":false}`, nil
}
func TestMaintenanceDoorDoesNotStartWorkForStatus(t *testing.T) {
	e := &maintenanceEngine{fakeEngine: &fakeEngine{}}
	r := answerProfileMaintenance(context.Background(), e, Request{})
	if !r.OK || e.refresh || r.Out == "" {
		t.Fatal(r, e)
	}
	r = answerProfileMaintenance(context.Background(), e, Request{Session: "source", Name: "refresh"})
	if !r.OK || !e.refresh || e.session != "source" {
		t.Fatal(r, e)
	}
	if r := answerProfileMaintenance(context.Background(), e, Request{Name: "execute"}); r.OK {
		t.Fatal("unknown action accepted")
	}
}
