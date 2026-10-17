package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/fleet"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Call discovers a peer's explicit contract and invokes its bounded executor without another model.
type Call struct {
	Reader    func() fleet.Reader
	ConfigDir string
	Self      string
	Cache     *fleet.Cache
	Reach     Reach
}

func (Call) Name() string { return "companion_call" }
func (Call) Description() string {
	return "Discover and directly invoke a companion's user-published operations without asking its model. Use companions or hand_off's roster to find who, list for operation summaries, describe for the full input/output contract, then call with its ID and arguments. This is separate from skills and hand_off. Only read_file operations are supported initially. An unavailable operation is refused; this never falls back to delegation."
}
func (Call) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"who":{"type":"string"},"action":{"type":"string","enum":["list","describe","call"]},"id":{"type":"string","description":"Operation ID, required for describe and call."},"arguments":{"type":"object","description":"Arguments matching the described input schema; required for call."}},"required":["who","action"],"additionalProperties":false}`)
}

// No ReadOnly marker: future mutation support must not enter looking mode through a generic door.
func (t Call) Execute(ctx context.Context, raw json.RawMessage, _ port.ToolEnv) (session.ToolResult, error) {
	var in struct {
		Who       string          `json:"who"`
		Action    string          `json:"action"`
		ID        string          `json:"id"`
		Arguments json.RawMessage `json:"arguments"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return errText("invalid companion_call arguments: " + err.Error()), nil
	}
	var more any
	if dec.Decode(&more) != io.EOF {
		return errText("arguments must be one JSON object"), nil
	}
	in.Who = strings.TrimSpace(in.Who)
	in.ID = strings.TrimSpace(in.ID)
	if in.Who == "" {
		return errText("who must name a companion"), nil
	}
	switch in.Action {
	case "list":
		if in.ID != "" || len(in.Arguments) != 0 {
			return errText("list accepts no id or arguments"), nil
		}
	case "describe":
		if in.ID == "" || len(in.Arguments) != 0 {
			return errText("describe requires id and accepts no arguments"), nil
		}
	case "call":
		if in.ID == "" || len(in.Arguments) == 0 || strings.TrimSpace(string(in.Arguments)) == "null" {
			return errText("call requires id and an arguments object"), nil
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(in.Arguments, &object); err != nil {
			return errText("arguments must be an object"), nil
		}
	default:
		return errText("action must be list, describe, or call"), nil
	}
	if t.Reader == nil || t.Reader() == nil || t.Reach == nil {
		return errText("this companion cannot reach its peers"), nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	list, err := fleet.ListCached(ctx, t.Reader(), t.ConfigDir, t.Self, t.Cache)
	if err != nil {
		return errText("cannot list companions: " + err.Error()), nil
	}
	found := fleet.Resolve(list, in.Who)
	if len(found) != 1 {
		return errText(fmt.Sprintf("%q must identify exactly one companion; there is: %s", in.Who, fleet.Roster(list))), nil
	}
	target := found[0]
	if !target.Live {
		return errText(target.Name + " is not running"), nil
	}
	cl, err := t.Reach(ctx, target.Host, target.Socket)
	if err != nil {
		return errText("cannot reach " + target.Name + ": " + err.Error()), nil
	}
	defer cl.Close()
	// Closing the transport also bounds local calls, whose socket dial does not consume ctx.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			if err := cl.Close(); err != nil {
				log.Printf("magi: closing cancelled companion operation: %v", err)
			}
		case <-done:
		}
	}()
	reply, err := cl.CompanionOperation(in.Action, in.ID, in.Arguments)
	if err != nil {
		return errText("companion operation failed: " + err.Error()), nil
	}
	if in.Action == "call" {
		if reply.OperationResult == nil {
			return errText("peer returned no operation result"), nil
		}
		return *reply.OperationResult, nil
	}
	if len(reply.Declaration) == 0 {
		return errText("peer returned no operation declaration"), nil
	}
	return session.ToolResult{Content: reply.Declaration}, nil
}
