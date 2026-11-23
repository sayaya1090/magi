package lua

import (
	lua "github.com/yuin/gopher-lua"
	"github.com/yuin/gopher-lua/ast"
	"github.com/yuin/gopher-lua/parse"
	"strings"
)

// Look only at the immediate consumer of an explore result. The branches are evidence of
// a bounded decision, but they cannot establish whether the question also needed file access.
func judgeCandidates(source string) []int {
	if len(source) > 65536 || !strings.Contains(source, "explore") {
		return nil
	}
	body, err := parse.Parse(strings.NewReader(source), "routine")
	if err != nil {
		return nil
	}
	var lines []int
	var scan func([]ast.Stmt)
	scan = func(stmts []ast.Stmt) {
		for i, s := range stmts {
			switch node := s.(type) {
			case *ast.LocalAssignStmt:
				if len(node.Exprs) != 1 || len(node.Names) == 0 || i+1 >= len(stmts) {
					continue
				}
				call, ok := node.Exprs[0].(*ast.FuncCallExpr)
				if !ok || len(call.Args) != 2 {
					continue
				}
				fn, ok := call.Func.(*ast.IdentExpr)
				tool, literal := call.Args[0].(*ast.StringExpr)
				branch, branches := stmts[i+1].(*ast.IfStmt)
				if !ok || fn.Value != "call_tool" || !literal || tool.Value != "explore" || !branches {
					continue
				}
				cmp, ok := branch.Condition.(*ast.RelationalOpExpr)
				if !ok || cmp.Operator != "==" {
					continue
				}
				if comparesLabel(cmp.Lhs, cmp.Rhs, node.Names[0]) || comparesLabel(cmp.Rhs, cmp.Lhs, node.Names[0]) {
					lines = append(lines, node.Line())
				}
			case *ast.IfStmt:
				scan(node.Then)
				scan(node.Else)
			case *ast.GenericForStmt:
				scan(node.Stmts)
			case *ast.NumberForStmt:
				scan(node.Stmts)
			case *ast.DoBlockStmt:
				scan(node.Stmts)
			case *ast.WhileStmt:
				scan(node.Stmts)
			case *ast.RepeatStmt:
				scan(node.Stmts)
			}
		}
	}
	scan(body)
	return lines
}
func comparesLabel(variable, label ast.Expr, name string) bool {
	v, ok := variable.(*ast.IdentExpr)
	_, literal := label.(*ast.StringExpr)
	return ok && v.Value == name && literal
}
func bridgeJudgeCandidates(L *lua.LState) int {
	result := L.NewTable()
	for _, line := range judgeCandidates(L.CheckString(1)) {
		result.Append(lua.LNumber(line))
	}
	L.Push(result)
	return 1
}
