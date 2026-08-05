package lua

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Everything a plugin contributes comes out in a FIXED order.
//
// h.plugins is a map and Go randomises map iteration deliberately, so the slash-command list was
// shuffled on every draw — a user watching the palette saw the same commands swap places for no
// reason. One test covers the three collectors that range that map, because they shared the bug and
// now share the fix.
func TestPluginContributionsComeOutInAFixedOrder(t *testing.T) {
	h := hostWith(nil, nil)

	// Load in an order that is neither sorted nor reverse-sorted, so "it happened to come out
	// right" is not an explanation for a pass.
	for _, name := range []string{"mid", "alpha", "zulu", "beta"} {
		dir := writePlugin(t,
			fmt.Sprintf("name=%q\ncapabilities=[\"tool\",\"command\"]\n", name),
			fmt.Sprintf(`
magi.register_command{ name = %q, description = "d", execute = function() end }
magi.register_tool{ name = %q, description = "d",
  schema = { type = "object", properties = {} },
  execute = function() return "" end }
`, "cmd_"+name, "tool_"+name))
		if _, err := h.Load(context.Background(), dir); err != nil {
			t.Fatalf("load %s: %v", name, err)
		}
	}

	want := []string{"cmd_alpha", "cmd_beta", "cmd_mid", "cmd_zulu"}
	// Repeat: a map with four entries can hand back the same order twice by luck, and the symptom
	// the user saw was precisely that it did not do so every time.
	for i := 0; i < 40; i++ {
		var got []string
		for _, c := range h.PluginCommands() {
			got = append(got, c.Name())
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("draw %d: commands came out %v, want %v", i, got, want)
		}

		var tools []string
		for _, tl := range h.Capabilities().Tools {
			tools = append(tools, tl.Name())
		}
		if w := "tool_alpha,tool_beta,tool_mid,tool_zulu"; strings.Join(tools, ",") != w {
			t.Fatalf("draw %d: tools came out %v, want %s", i, tools, w)
		}
	}
}
