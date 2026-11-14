package app

import _ "embed"

//go:embed builtin_skills/visual-explanation/SKILL.md
var visualExplanationSkill string

//go:embed builtin_skills/ooad/SKILL.md
var ooadSkill string

// builtinSkills ship inside the binary: available in every workspace without installation, and
// shadowed by a workspace skill of the same name.
var builtinSkills = map[string]string{
	"visual-explanation": visualExplanationSkill,
	"ooad":               ooadSkill,
}
