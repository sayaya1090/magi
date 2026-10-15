package lua

import (
	"context"
	"time"

	"github.com/sayaya1090/magi/internal/port"
	lua "github.com/yuin/gopher-lua"
)

// apply_experience is a local managed projection, not an independently editable
// team copy. Owner/source are assigned by this host, never a table's claims.
func (p *plugin) bridgeApplyExperience(L *lua.LState) int {
	p.requireCap(L, "experience")
	if p.host == nil {
		return fail(L, "managed experience is unavailable")
	}
	store, ok := p.host.experience.(port.ManagedExperienceStore)
	if !ok {
		return fail(L, "managed experience is unavailable")
	}
	t := L.CheckTable(1)
	str := func(key string) string {
		v := t.RawGetString(key)
		if v == lua.LNil {
			return ""
		}
		return v.String()
	}
	c := port.ExperienceChange{ObjectID: str("object_id"), ExpectedRevision: str("expected_revision"), OperationID: str("operation_id"), ObservationID: str("observation_id"), Kind: str("kind"), State: str("state"), Scope: "project", Owner: "plugin:" + p.name, Source: "plugin:" + p.name, HostOutcome: str("host_outcome")}
	if c.State == "active" {
		if c.Kind == "memory" {
			c.Memory = &port.Memory{Text: str("text")}
		} else if c.Kind == "skill" {
			c.Skill = &port.Skill{Name: str("name"), Description: str("description"), Body: str("body")}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := store.ApplyExperience(ctx, c)
	if err != nil {
		return fail(L, err.Error())
	}
	out := L.NewTable()
	out.RawSetString("object_id", lua.LString(r.ObjectID))
	out.RawSetString("revision", lua.LString(r.Revision))
	out.RawSetString("state", lua.LString(r.State))
	L.Push(out)
	return 1
}
