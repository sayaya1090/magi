package lua

import (
	"crypto/sha256"
	"fmt"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"mvdan.cc/sh/v3/syntax"
)

// Describe simple top-level pipelines. Values are transient: the observer hashes
// literal operands before persisting evidence. A scalar expansion stays an input.
func pipelineObservations(L *lua.LState, file *syntax.File) *lua.LTable {
	out := L.NewTable()
	for _, stmt := range file.Stmts {
		if out.Len() >= 16 {
			break
		}
		var stages []*syntax.CallExpr
		if !literalPipeline(stmt, &stages) || len(stages) == 2 {
			continue
		}
		row := L.NewTable()
		valid := true
		delimited := false
		for i, cmd := range stages {
			stage := L.NewTable()
			stage.Append(lua.LString(cmd.Args[0].Lit()))
			for j, arg := range cmd.Args[1:] {
				word := shellOperandLiteral(arg)
				if word == "" {
					if len(arg.Parts) != 1 {
						valid = false
						break
					}
					v, ok := arg.Parts[0].(*syntax.ParamExp)
					if !ok || !v.Short || !shellName.MatchString(v.Param.Value) {
						valid = false
						break
					}
					stage.Append(lua.LFalse)
					continue
				}
				if len(arg.Parts) == 1 {
					if _, ok := arg.Parts[0].(*syntax.Lit); ok && strings.ContainsAny(word, "*?[{~") {
						valid = false
						break
					}
				}
				if i == 0 && word == "--" && j+2 < len(cmd.Args) {
					delimited = true
				}
				stage.Append(lua.LString(word))
			}
			row.Append(stage)
		}
		if valid && delimited {
			var shape shellShape
			if shape.statement(stmt, 0) {
				row.RawSetString("graph", lua.LString(fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(shape.tokens, "\x00"))))))
			}
			out.Append(row)
		}
	}
	return out
}

func literalPipeline(stmt *syntax.Stmt, stages *[]*syntax.CallExpr) bool {
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) != 0 || len(*stages) >= 8 {
		return false
	}
	switch n := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		return n.Op == syntax.Pipe && literalPipeline(n.X, stages) && literalPipeline(n.Y, stages)
	case *syntax.CallExpr:
		if len(n.Assigns) != 0 || len(n.Args) == 0 || len(n.Args) > 33 || !shellName.MatchString(n.Args[0].Lit()) {
			return false
		}
		*stages = append(*stages, n)
		return true
	}
	return false
}
