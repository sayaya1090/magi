package lua

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
			m := &port.Memory{
				Text:        str("text"),
				Description: str("description"),
				Kind:        str("memory_kind"),
				Tier:        str("tier"),
				Status:      str("status"),
				ValidUntil:  str("valid_until"),
			}
			if areas, ok := t.RawGetString("areas").(*lua.LTable); ok {
				areas.ForEach(func(_, av lua.LValue) {
					if s := av.String(); s != "" && s != "nil" {
						m.Areas = append(m.Areas, s)
					}
				})
			}
			if links, ok := t.RawGetString("links").(*lua.LTable); ok {
				links.ForEach(func(_, lv lua.LValue) {
					if s := lv.String(); s != "" && s != "nil" {
						m.Links = append(m.Links, s)
					}
				})
			}
			if evs, ok := t.RawGetString("evidence").(*lua.LTable); ok {
				evs.ForEach(func(_, ev lua.LValue) {
					et, ok := ev.(*lua.LTable)
					if !ok {
						return
					}
					estr := func(k string) string {
						v := et.RawGetString(k)
						if v == lua.LNil {
							return ""
						}
						return v.String()
					}
					turn := 0
					if tv, ok := et.RawGetString("turn").(lua.LNumber); ok {
						turn = int(tv)
					}
					m.Evidence = append(m.Evidence, port.MemoryEvidence{
						Session: estr("session"),
						Turn:    turn,
						Commit:  estr("commit"),
						Outcome: estr("outcome"),
					})
				})
			}
			c.Memory = m
		} else if c.Kind == "skill" {
			c.Skill = &port.Skill{Name: str("name"), Description: str("description"), Body: str("body")}
			digest := func(text string) string {
				if text == "" {
					return ""
				}
				sum := sha256.Sum256([]byte(text))
				return hex.EncodeToString(sum[:])
			}
			c.Skill.DocumentDigest = digest(str("document_snapshot"))
			c.Skill.VerificationDigest = digest(str("verification_snapshot"))
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

// magi.search_experience{query=?, kind=?, min_similarity=?, limit=?} -> array of managed records
func (p *plugin) bridgeSearchExperience(L *lua.LState) int {
	p.requireCap(L, "experience")
	if p.host == nil || p.host.experience == nil {
		return fail(L, "managed experience is unavailable")
	}
	insp, ok := p.host.experience.(port.ManagedExperienceInspector)
	if !ok {
		return fail(L, "managed experience search is unavailable")
	}
	query, kind := "", ""
	minSim := 0.0
	limit := 0
	if L.GetTop() >= 1 && L.Get(1) != lua.LNil {
		t := L.CheckTable(1)
		if v := t.RawGetString("query"); v != lua.LNil {
			query = v.String()
		}
		if v := t.RawGetString("kind"); v != lua.LNil {
			kind = v.String()
		}
		if v, ok := t.RawGetString("min_similarity").(lua.LNumber); ok {
			minSim = float64(v)
		}
		if v, ok := t.RawGetString("limit").(lua.LNumber); ok {
			limit = int(v)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	items, err := insp.SearchManagedExperience(ctx, query, kind, minSim, limit)
	if err != nil {
		return fail(L, err.Error())
	}
	out := L.NewTable()
	for _, it := range items {
		row := L.NewTable()
		row.RawSetString("object_id", lua.LString(it.ObjectID))
		row.RawSetString("revision", lua.LString(it.Revision))
		row.RawSetString("content_revision", lua.LString(it.ContentRevision))
		row.RawSetString("owner", lua.LString(it.Owner))
		row.RawSetString("kind", lua.LString(it.Kind))
		row.RawSetString("state", lua.LString(it.State))
		row.RawSetString("host_outcome", lua.LString(it.HostOutcome))
		row.RawSetString("similarity", lua.LNumber(it.Similarity))
		row.RawSetString("score", lua.LNumber(it.Score))
		row.RawSetString("cold", lua.LBool(it.Cold))
		row.RawSetString("updated_at", lua.LString(it.UpdatedAt))
		row.RawSetString("updated_epoch", lua.LNumber(it.UpdatedEpoch))
		if it.Memory != nil {
			mt := L.NewTable()
			mt.RawSetString("id", lua.LString(it.Memory.ID))
			mt.RawSetString("text", lua.LString(it.Memory.Text))
			mt.RawSetString("description", lua.LString(it.Memory.Description))
			mt.RawSetString("kind", lua.LString(it.Memory.Kind))
			mt.RawSetString("tier", lua.LString(it.Memory.Tier))
			mt.RawSetString("status", lua.LString(it.Memory.Status))
			mt.RawSetString("valid_until", lua.LString(it.Memory.ValidUntil))
			areas := L.NewTable()
			for _, a := range it.Memory.Areas {
				areas.Append(lua.LString(a))
			}
			mt.RawSetString("areas", areas)
			links := L.NewTable()
			for _, l := range it.Memory.Links {
				links.Append(lua.LString(l))
			}
			mt.RawSetString("links", links)
			evs := L.NewTable()
			for _, ev := range it.Memory.Evidence {
				et := L.NewTable()
				et.RawSetString("session", lua.LString(ev.Session))
				et.RawSetString("turn", lua.LNumber(ev.Turn))
				et.RawSetString("commit", lua.LString(ev.Commit))
				et.RawSetString("outcome", lua.LString(ev.Outcome))
				evs.Append(et)
			}
			mt.RawSetString("evidence", evs)
			row.RawSetString("memory", mt)
			row.RawSetString("text", lua.LString(it.Memory.Text))
			row.RawSetString("description", lua.LString(it.Memory.Description))
			row.RawSetString("memory_kind", lua.LString(it.Memory.Kind))
			row.RawSetString("tier", lua.LString(it.Memory.Tier))
			row.RawSetString("status", lua.LString(it.Memory.Status))
			row.RawSetString("valid_until", lua.LString(it.Memory.ValidUntil))
			row.RawSetString("areas", areas)
			row.RawSetString("links", links)
			row.RawSetString("evidence", evs)
		}
		if it.Skill != nil {
			st := L.NewTable()
			st.RawSetString("name", lua.LString(it.Skill.Name))
			st.RawSetString("description", lua.LString(it.Skill.Description))
			st.RawSetString("body", lua.LString(it.Skill.Body))
			row.RawSetString("skill", st)
		}
		out.Append(row)
	}
	L.Push(out)
	return 1
}
