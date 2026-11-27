package lua

import (
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"mvdan.cc/sh/v3/syntax"
)

func TestBindShellOperandPreservesOneLiteralWord(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	L.SetGlobal("bind", L.NewFunction(bindShellOperand))
	if err := L.DoString(`command=assert(bind("show -- {{value}}", "Sam's report 2"))
assert(not bind("{{value}} --version","report"))
assert(not bind("show -- {{value}}","-n"))
assert(not bind("show '{{value}}'", "report"))`); err != nil {
		t.Fatal(err)
	}
	f, err := syntax.NewParser().Parse(strings.NewReader(L.GetGlobal("command").String()), "")
	if err != nil || len(f.Stmts) != 1 {
		t.Fatalf("invalid command: %v", err)
	}
	call := f.Stmts[0].Cmd.(*syntax.CallExpr)
	if len(call.Args) != 3 {
		t.Fatalf("expected 3 words, got %d", len(call.Args))
	}
	var value strings.Builder
	for _, p := range call.Args[2].Parts {
		switch part := p.(type) {
		case *syntax.SglQuoted:
			value.WriteString(part.Value)
		case *syntax.Lit:
			if part.Value != `\'` {
				t.Fatalf("unexpected literal %q", part.Value)
			}
			value.WriteString("'")
		default:
			t.Fatalf("expected literal argument, got %T", part)
		}
	}
	if value.String() != "Sam's report 2" {
		t.Fatalf("bound operand %q", value.String())
	}
}
