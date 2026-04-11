package claudeplugin

import (
	"embed"
	"io/fs"
)

const skillsRoot = "skills"

//go:embed skills/**
var embeddedSkills embed.FS

// SkillsFS returns the canonical portable gcx skill bundle rooted at skills/.
func SkillsFS() fs.FS {
	sub, err := fs.Sub(embeddedSkills, skillsRoot)
	if err != nil {
		panic(err)
	}
	return sub
}
