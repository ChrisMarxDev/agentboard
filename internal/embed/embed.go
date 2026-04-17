package embed

import (
	_ "embed"
)

//go:embed skill.md
var skillFile string

// SkillFile returns the static Claude skill file content.
func SkillFile() string {
	return skillFile
}
