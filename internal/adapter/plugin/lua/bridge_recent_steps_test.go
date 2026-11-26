package lua

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// recent_tool_steps hands a plugin that declared "history" the past tool calls of its own workspace:
// session by session, each call with its turn, arguments and result. Without the capability it is refused.
func TestRecentToolStepsNeedsHistoryAndReadsTheOwnWorkspace(t *testing.T) {
	const body = `magi.register_tool{name="peek", description="peek", execute=function()
  local got, err = magi.recent_tool_steps(3)
  if not got then return "ERR " .. tostring(err) end
  local out = {}
  for _, s in ipairs(got) do
    for _, st in ipairs(s.steps) do
      out[#out+1] = s.session .. "|" .. st.turn .. "|" .. st.name .. "|" .. tostring(st.args.path) .. "|" .. st.output
    end
  end
  return table.concat(out, "\n")
end}`
	var asked string
	var n int
	recent := func(_ context.Context, workdir string, sessions int) ([]port.SessionSteps, error) {
		asked, n = workdir, sessions
		return []port.SessionSteps{{Session: "s_new", Steps: []port.ChildStep{
			{Name: "read", Args: json.RawMessage(`{"path":"a.txt"}`), Output: "alpha", Turn: 2},
		}}}, nil
	}
	for _, tc := range []struct {
		caps string
		want string
	}{
		{`["tool","history"]`, "s_new|2|read|a.txt|alpha"},
		{`["tool"]`, `capability "history" not declared`},
	} {
		asked = ""
		dir := writePlugin(t, `name="peek"`+"\n"+`capabilities=`+tc.caps, body)
		reg := builtin.NewRegistry()
		h := NewHostWithConfig(HostConfig{ToolSink: reg, RecentSteps: recent, Runtime: RuntimeInfo{Workdir: "/work/here"}})
		if _, err := h.Load(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
		tool, _ := reg.Get("peek")
		got, _ := execTool(t, tool, `{}`, t.TempDir())
		if !strings.Contains(got, tc.want) {
			t.Fatalf("caps %s: got %q, want %q", tc.caps, got, tc.want)
		}
		if strings.Contains(tc.caps, "history") && (asked != "/work/here" || n != 3) {
			t.Fatalf("host asked for workdir %q, %d sessions", asked, n)
		}
	}
}
