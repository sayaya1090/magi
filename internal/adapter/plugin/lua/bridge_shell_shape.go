package lua

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"

	lua "github.com/yuin/gopher-lua"
	"mvdan.cc/sh/v3/syntax"
)

var shellName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
var shellOption = regexp.MustCompile(`^--?[A-Za-z][A-Za-z0-9-]*$`)

// bridgeShellShape parses for observation only. The digest is never used to
// authorize a tool call. The caller can fall back to an exact digest for unsupported syntax.
func bridgeShellShape(L *lua.LState) int {
	command := L.CheckString(1)
	if len(command) > 65536 {
		return 0
	}
	file, err := syntax.NewParser().Parse(strings.NewReader(command), "")
	if err != nil {
		return 0
	}
	shape := &shellShape{}
	edges := shellDependencyEdges(L, file)
	if !shape.list(file.Stmts, 0) || len(shape.labels) == 0 {
		L.Push(lua.LNil)
		L.Push(lua.LNil)
		L.Push(edges)
		rows, graphs := pipelineObservations(L, file)
		L.Push(rows)
		L.Push(graphs)
		return 5
	}
	sum := sha256.Sum256([]byte(strings.Join(shape.tokens, "\x00")))
	L.Push(lua.LString(hex.EncodeToString(sum[:])))
	L.Push(lua.LString(strings.Join(shape.labels, " ")))
	L.Push(edges)
	rows, graphs := pipelineObservations(L, file)
	L.Push(rows)
	L.Push(graphs)
	return 5
}

// Each supported binary subtree is a small dependency graph. Its enclosing
// statements do not participate in its identity.
func shellDependencyEdges(L *lua.LState, file *syntax.File) *lua.LTable {
	edges := L.NewTable()
	seen := make(map[string]bool)
	add := func(shape shellShape, chain *lua.LTable) {
		sum := sha256.Sum256([]byte(strings.Join(shape.tokens, "\x00")))
		key := hex.EncodeToString(sum[:])
		if seen[key] || edges.Len() >= 16 {
			return
		}
		seen[key] = true
		entry := L.NewTable()
		entry.RawSetString("key", lua.LString(key))
		entry.RawSetString("label", lua.LString(strings.Join(shape.labels, " ")))
		entry.RawSetString("conditions", lua.LNumber(shape.conditions))
		entry.RawSetString("file_edges", lua.LNumber(shape.fileEdges))
		entry.RawSetString("pipes", lua.LNumber(shape.pipes))
		if chain != nil {
			entry.RawSetString("chain", chain)
		}
		edges.Append(entry)
	}
	for _, shape := range shellFileChains(file.Stmts) {
		add(shape, nil)
	}
	// Give complete top-level chains first choice of the bounded slots. Nested
	// syntax (including dormant functions and conditional branches) is evidence
	// for a graph hint only.
	observe := func(stmt *syntax.Stmt, runnable bool) {
		if _, ok := stmt.Cmd.(*syntax.BinaryCmd); !ok {
			return
		}
		var shape shellShape
		if !shape.statement(stmt, 0) || (shape.conditions < 2 && shape.pipes < 2) {
			return
		}
		var chain *lua.LTable
		op := stmt.Cmd.(*syntax.BinaryCmd).Op
		edges := shape.conditions + shape.pipes
		if runnable && edges >= 2 && edges <= 7 && (op == syntax.AndStmt || op == syntax.Pipe) {
			stages := L.NewTable()
			if shellStages(L, stmt, op, stages, make(map[string]int)) {
				chain = stages
			}
		}
		add(shape, chain)
	}
	for _, stmt := range file.Stmts {
		if edges.Len() >= 16 {
			break
		}
		// An AND-list starts with its left prefix. A prefix retains its
		// own prerequisites, unlike a suffix detached from earlier stages.
		for edges.Len() < 16 {
			observe(stmt, true)
			binary, ok := stmt.Cmd.(*syntax.BinaryCmd)
			if !ok || binary.Op != syntax.AndStmt || stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
				break
			}
			stmt = binary.X
		}
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		if edges.Len() >= 16 {
			return false
		}
		if stmt, ok := node.(*syntax.Stmt); ok {
			observe(stmt, false)
		}
		return true
	})
	return edges
}

// A runnable chain has a narrower contract than an observed graph. Each stage
// gets a fixed executable and current literal argv. Redirections, assignments,
// substitutions, globs and optional branches require shell evaluation.
func shellStages(L *lua.LState, stmt *syntax.Stmt, op syntax.BinCmdOperator, stages *lua.LTable, inputs map[string]int) bool {
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) > 0 {
		return false
	}
	switch cmd := stmt.Cmd.(type) {
	case *syntax.BinaryCmd:
		return cmd.Op == op && shellStages(L, cmd.X, op, stages, inputs) && shellStages(L, cmd.Y, op, stages, inputs)
	case *syntax.CallExpr:
		if len(cmd.Assigns) != 0 || len(cmd.Args) == 0 || len(cmd.Args) > 33 || !shellName.MatchString(cmd.Args[0].Lit()) {
			return false
		}
		for _, word := range cmd.Args[1:] {
			literal := true
			syntax.Walk(word, func(node syntax.Node) bool {
				switch part := node.(type) {
				case nil, *syntax.Word, *syntax.SglQuoted, *syntax.DblQuoted:
				case *syntax.Lit:
					if strings.ContainsAny(part.Value, "*?[{`~") {
						literal = false
					}
				case *syntax.ParamExp:
					// A single scalar is an input supplied by the new request.
					literal = literal && part.Short && shellName.MatchString(part.Param.Value)
					return false
				default:
					literal = false
				}
				return literal
			})
			if !literal {
				return false
			}
		}
		stage := L.NewTable()
		stage.RawSetString("name", lua.LString(cmd.Args[0].Lit()))
		slots := L.NewTable()
		options := true
		for i, arg := range cmd.Args[1:] {
			word := shellOperandLiteral(arg)
			if options && (word == "--" || shellOption.MatchString(word) || i == 0 && len(cmd.Args) > 2 && shellName.MatchString(word) && strings.HasPrefix(shellOperandLiteral(cmd.Args[2]), "-")) {
				slots.Append(lua.LString(word))
				options = word != "--"
			} else {
				// A dynamic or empty word gets its own input.
				index := inputs[word]
				if word == "" || index == 0 {
					index = len(inputs) + 1
					if word == "" {
						inputs["\x00"+arg.Pos().String()] = index
					} else {
						inputs[word] = index
					}
				}
				slots.Append(lua.LNumber(index))
			}
		}
		stage.RawSetString("slots", slots)
		stages.Append(stage)
		return true
	default:
		return false
	}
}

// A path may be quoted in one stage and unquoted in another. Only fold
// quotes when the resulting bytes are known without evaluating shell code.
func shellOperandLiteral(arg *syntax.Word) string {
	if len(arg.Parts) == 1 {
		switch part := arg.Parts[0].(type) {
		case *syntax.SglQuoted:
			if !part.Dollar {
				return part.Value
			}
		case *syntax.DblQuoted:
			if !part.Dollar && len(part.Parts) == 1 {
				if literal, ok := part.Parts[0].(*syntax.Lit); ok && !strings.Contains(literal.Value, `\`) {
					return literal.Value
				}
			}
		}
	}
	word := arg.Lit()
	if strings.Contains(word, `\`) {
		return ""
	}
	return word
}

// Absolute literal paths keep the edge independent of intervening directory
// changes. Only explicit stdin/stdout redirects establish a connection.
func shellFileChains(stmts []*syntax.Stmt) []shellShape {
	producers := make(map[string]shellShape)
	var chains []shellShape
	for _, stmt := range stmts {
		if len(chains) >= 16 {
			break
		}
		var stage shellShape
		if _, ok := stmt.Cmd.(*syntax.CallExpr); !ok || !stage.statement(stmt, 0) || len(stage.labels) != 1 {
			clear(producers)
			continue
		}
		var input, output string
		valid := true
		for _, redirect := range stmt.Redirs {
			path := redirect.Word.Lit()
			if !filepath.IsAbs(path) || strings.ContainsAny(path, "\\*$?[]{}") {
				valid = false
				break
			}
			switch redirect.Op {
			case syntax.RdrIn:
				if redirect.N == nil || redirect.N.Value == "0" {
					input = path
				}
			case syntax.RdrOut, syntax.AppOut:
				if redirect.N == nil || redirect.N.Value == "1" {
					output = path
				}
			default:
				valid = false
			}
		}
		if !valid {
			clear(producers)
			continue
		}
		if prior, ok := producers[input]; ok && input != output && prior.fileEdges < 2 {
			tokens := append(append([]string{}, prior.tokens...), "file edge")
			labels := append(append([]string{}, prior.labels...), "→")
			stage.tokens = append(tokens, stage.tokens...)
			stage.labels = append(labels, stage.labels...)
			stage.fileEdges = prior.fileEdges + 1
			if stage.fileEdges == 2 {
				chains = append(chains, stage)
			}
		}
		if output != "" {
			producers[output] = stage
		}
	}
	return chains
}

type shellShape struct {
	tokens     []string
	labels     []string
	conditions int
	fileEdges  int
	pipes      int
}

func (s *shellShape) list(stmts []*syntax.Stmt, depth int) bool {
	for i, stmt := range stmts {
		if i > 0 {
			s.tokens = append(s.tokens, ";")
			s.labels = append(s.labels, ";")
		}
		if !s.statement(stmt, depth) {
			return false
		}
	}
	return true
}

func (s *shellShape) statement(stmt *syntax.Stmt, depth int) bool {
	if depth >= 32 || len(s.tokens) > 1024 || stmt.Background || stmt.Coprocess || stmt.Disown {
		return false
	}
	s.tokens = append(s.tokens, "statement")
	if stmt.Negated {
		s.tokens = append(s.tokens, "!")
	}
	for _, redirect := range stmt.Redirs {
		if redirect.Hdoc != nil {
			return false
		}
		fd := ""
		if redirect.N != nil {
			fd = redirect.N.Value
		}
		target := "operand"
		if op := redirect.Op.String(); op == ">&" || op == "<&" {
			target = redirect.Word.Lit()
			if target == "" {
				return false
			}
		}
		s.tokens = append(s.tokens, "redirect", fd, redirect.Op.String(), target)
	}

	switch cmd := stmt.Cmd.(type) {
	case *syntax.CallExpr:
		for _, assign := range cmd.Assigns {
			if assign.Name == nil || assign.Index != nil || assign.Array != nil || assign.Append {
				return false
			}
			s.tokens = append(s.tokens, "assignment", assign.Name.Value)
		}
		if len(cmd.Args) == 0 {
			return true
		}
		name := cmd.Args[0].Lit()
		if !shellName.MatchString(name) {
			return false
		}
		s.tokens = append(s.tokens, "command", name)
		s.labels = append(s.labels, name)
		if len(cmd.Args) > 2 {
			word, next := shellOperandLiteral(cmd.Args[1]), shellOperandLiteral(cmd.Args[2])
			if shellName.MatchString(word) && strings.HasPrefix(next, "-") {
				s.tokens = append(s.tokens, "leading word", word)
			}
		}
		options := true
		for _, arg := range cmd.Args[1:] {
			word := shellOperandLiteral(arg)
			option, _, assigned := strings.Cut(word, "=")
			switch {
			case options && word == "--":
				s.tokens = append(s.tokens, "--")
				options = false
			case options && shellOption.MatchString(option):
				if assigned {
					option += "="
				}
				s.tokens = append(s.tokens, option)
			default:
				s.tokens = append(s.tokens, "operand")
			}
		}
	case *syntax.BinaryCmd:
		op := cmd.Op.String()
		if op == "&&" || op == "||" {
			s.conditions++
		} else if cmd.Op == syntax.Pipe {
			s.pipes++
		}
		s.tokens = append(s.tokens, "binary", op)
		if !s.statement(cmd.X, depth+1) {
			return false
		}
		s.labels = append(s.labels, op)
		if !s.statement(cmd.Y, depth+1) {
			return false
		}
	case *syntax.Block:
		s.tokens = append(s.tokens, "block")
		if !s.list(cmd.Stmts, depth+1) {
			return false
		}
	case *syntax.Subshell:
		s.tokens = append(s.tokens, "subshell")
		if !s.list(cmd.Stmts, depth+1) {
			return false
		}
	default:
		return false
	}
	s.tokens = append(s.tokens, "end statement")
	return true
}
