package builtin

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
)

func TestMermaidRejectsExecutableContent(t *testing.T) {
	for _, src := range []string{"flowchart LR\nclick A call foo()", "flowchart LR\nA[<script>bad</script>]", "%%{init: {}}%%\nflowchart LR", "flowchart LR\nA[https://example.com]", strings.Repeat("x", 24001)} {
		b, _ := json.Marshal(map[string]string{"title": "test", "source": src})
		r, e := (Mermaid{}).Execute(context.Background(), b, port.ToolEnv{})
		if e != nil || !r.IsError {
			t.Fatalf("accepted %q", src[:min(50, len(src))])
		}
	}
	r, e := (Mermaid{}).Execute(context.Background(), json.RawMessage(`{"title":"구성","source":"flowchart LR\n A[에이전트] --> B[개발자]"}`), port.ToolEnv{})
	if e != nil || r.IsError {
		t.Fatalf("valid: %+v %v", r, e)
	}
}
func TestMermaidPreviewUnavailableOverSSH(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "remote")
	if DiagramBrowserAvailable(context.Background()) {
		t.Fatal("remote browser advertised")
	}
	r, _ := (Mermaid{Preview: true}).Execute(context.Background(), json.RawMessage(`{"title":"x","source":"flowchart LR\nA-->B"}`), port.ToolEnv{})
	if !r.IsError {
		t.Fatal("remote preview created")
	}
}

func TestMermaidDescriptionsTravelWithPortableSource(t *testing.T) {
	raw := json.RawMessage(`{"title":"구성","source":"flowchart LR\nA[Agent] --> B[Developer]","descriptions":{"Agent":"Reads the request.\nPrepares a reply."},"animate":true}`)
	r, err := (Mermaid{}).Execute(context.Background(), raw, port.ToolEnv{})
	if err != nil || r.IsError {
		t.Fatalf("metadata: %+v %v", r, err)
	}
	var text string
	if err := json.Unmarshal(r.Content, &text); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `%% magi-descriptions {"Agent":"Reads the request.\nPrepares a reply."}`) || !strings.Contains(text, "\n%% magi-animate\n```") {
		t.Fatalf("missing portable metadata: %s", text)
	}
}

func TestMermaidDescriptionLimits(t *testing.T) {
	for _, description := range []string{"<script>bad</script>", "https://example.com", "```", strings.Repeat("x", 2001)} {
		raw, err := json.Marshal(map[string]any{"title": "test", "source": "flowchart LR\nA-->B", "descriptions": map[string]string{"A": description}})
		if err != nil {
			t.Fatal(err)
		}
		r, err := (Mermaid{}).Execute(context.Background(), raw, port.ToolEnv{})
		if err != nil || !r.IsError {
			t.Fatalf("accepted invalid description: %+v %v", r, err)
		}
	}
	descriptions := map[string]string{}
	for i := 0; i < 20; i++ {
		descriptions[strings.Repeat("a", i+1)] = strings.Repeat("x", 1500)
	}
	raw, err := json.Marshal(map[string]any{"title": "test", "source": "flowchart LR\nA-->B", "descriptions": descriptions})
	if err != nil {
		t.Fatal(err)
	}
	r, err := (Mermaid{}).Execute(context.Background(), raw, port.ToolEnv{})
	if err != nil || !r.IsError {
		t.Fatalf("accepted oversized combined source: %+v %v", r, err)
	}
}

func TestMermaidColorsTravelAsRolesOrHex(t *testing.T) {
	m := Mermaid{}
	run := func(colors string) (string, bool) {
		r, err := m.Execute(context.Background(), json.RawMessage(`{"title":"t","source":"flowchart LR\n A[Start] --> B[Fail]","colors":`+colors+`}`), port.ToolEnv{})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		_ = json.Unmarshal(r.Content, &text)
		return text, r.IsError
	}
	text, bad := run(`{"Start":"primary","Fail":"error","Odd":"#1A2B3C"}`)
	if bad || !strings.Contains(text, `%% magi-colors {"Fail":"error","Odd":"#1A2B3C","Start":"primary"}`) {
		t.Fatalf("colors metadata missing (error=%v):\n%s", bad, text)
	}
	for _, c := range []string{`"orange"`, `"#12345"`, `"var(--x)"`, `""`} {
		if _, bad := run(`{"Start":` + c + `}`); !bad {
			t.Errorf("colour %s accepted", c)
		}
	}
	if !strings.Contains(m.Description(), "colors") || !strings.Contains(string(m.Schema()), "tertiary") {
		t.Error("the tool does not tell the model the palette roles")
	}
}

func TestMermaidFlowsTravelAsCases(t *testing.T) {
	m := Mermaid{}
	run := func(flows string) (string, bool) {
		r, err := m.Execute(context.Background(), json.RawMessage(`{"title":"t","source":"flowchart LR\n A[Start] --> B[Mid]\n B --> C[End]","flows":`+flows+`}`), port.ToolEnv{})
		if err != nil {
			t.Fatal(err)
		}
		var text string
		_ = json.Unmarshal(r.Content, &text)
		return text, r.IsError
	}
	text, bad := run(`{"Case 1":["Start","Mid","End"],"Case 2":["Start","Mid"]}`)
	if bad || !strings.Contains(text, `%% magi-flows {"Case 1":["Start","Mid","End"],"Case 2":["Start","Mid"]}`) {
		t.Fatalf("flows metadata missing (error=%v):\n%s", bad, text)
	}
	for _, f := range []string{`{"one":["Start"]}`, `{"":["Start","Mid"]}`, `{"x":["Start",""]}`, `{"x":["<b>","Mid"]}`, `{"x":"Start"}`} {
		if _, bad := run(f); !bad {
			t.Errorf("flows %s accepted", f)
		}
	}
	if !strings.Contains(string(m.Schema()), `"flows"`) || !strings.Contains(m.Description(), "flows") {
		t.Error("the tool does not tell the model about flows")
	}
}

func TestMermaidFlowControlChoosesThePicker(t *testing.T) {
	m := Mermaid{}
	run := func(control string) (string, bool) {
		r, _ := m.Execute(context.Background(), json.RawMessage(`{"title":"t","source":"flowchart LR\n A[Start] --> B[Mid]","flows":{"c":["Start","Mid"]},"flowControl":"`+control+`"}`), port.ToolEnv{})
		var text string
		_ = json.Unmarshal(r.Content, &text)
		return text, r.IsError
	}
	if text, bad := run("checkbox"); bad || !strings.Contains(text, "%% magi-flow-control checkbox") {
		t.Fatalf("checkbox not carried (error=%v): %s", bad, text)
	}
	if text, bad := run("radio"); bad || strings.Contains(text, "magi-flow-control") {
		t.Fatalf("radio is the default and needs no comment (error=%v): %s", bad, text)
	}
	if _, bad := run("slider"); !bad {
		t.Error("unknown control accepted")
	}
}


func TestMermaidFlowTriggersTravelWithTheirCase(t *testing.T) {
	m := Mermaid{}
	run := func(extra string) (string, bool) {
		r, _ := m.Execute(context.Background(), json.RawMessage(`{"title":"t","source":"flowchart LR\n A[Start] --> B[Mid]","flows":{"c":["Start","Mid"]}`+extra+`}`), port.ToolEnv{})
		var text string
		_ = json.Unmarshal(r.Content, &text)
		return text, r.IsError
	}
	if text, bad := run(`,"triggers":{"c":["Mid"]}`); bad || !strings.Contains(text, `%% magi-flow-triggers {"c":["Mid"]}`) {
		t.Fatalf("triggers metadata missing (error=%v):\n%s", bad, text)
	}
	for _, bad := range []string{`,"triggers":{"nope":["Mid"]}`, `,"triggers":{"c":[]}`, `,"triggers":{"c":["<b>"]}`} {
		if _, refused := run(bad); !refused {
			t.Errorf("triggers %s accepted", bad)
		}
	}
	if !strings.Contains(string(m.Schema()), `"triggers"`) || !strings.Contains(m.Description(), "triggers") {
		t.Error("the tool does not tell the model about triggers")
	}
}
