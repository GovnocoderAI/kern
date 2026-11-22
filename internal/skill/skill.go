// Package skill loads, validates and applies named prompts ("skills") that
// extend the base agent system prompt. A skill is a markdown file with a
// YAML frontmatter block:
//
//	---
//	name: code-review
//	description: Reviews a diff for bugs and style issues
//	model: plusvibe/[REDACTED].5
//	---
//
//	You are a careful reviewer...
//
// The body (everything after the frontmatter) is the skill prompt.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GovnocoderAI/kern/internal/util"
)

// Skill is one named prompt.
type Skill struct {
	Name        string
	Description string
	Prompt      string
	Model       string
	Path        string
}

// LoadDir reads all skills from a single directory. Non-existent directories
// are silently skipped.
func LoadDir(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s, err := Parse(string(data))
		if err != nil {
			continue
		}
		s.Path = p
		out = append(out, s)
	}
	return out
}

// LoadProject reads skills from a project's .kern/skills/ directory.
func LoadProject(workdir string) []Skill {
	return LoadDir(filepath.Join(workdir, ".kern", "skills"))
}

// LoadGlobal reads skills from ~/.config/kern/skills/.
func LoadGlobal() []Skill {
	return LoadDir(filepath.Join(util.KernConfigDir(), "skills"))
}

// Load merges project and global skills. Project skills override global ones
// with the same name.
func Load(workdir string) []Skill {
	global := LoadGlobal()
	project := LoadProject(workdir)
	byName := map[string]Skill{}
	for _, s := range global {
		byName[s.Name] = s
	}
	for _, s := range project {
		byName[s.Name] = s
	}
	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Validate checks a list of skills for common problems and returns one
// error message per problem.
func Validate(skills []Skill) []string {
	seen := map[string]string{}
	var problems []string
	for _, s := range skills {
		if s.Name == "" {
			problems = append(problems, fmt.Sprintf("%s: missing name", s.Path))
			continue
		}
		if s.Prompt == "" {
			problems = append(problems, fmt.Sprintf("%s: empty body", s.Name))
		}
		if prev, ok := seen[s.Name]; ok {
			problems = append(problems, fmt.Sprintf("duplicate skill name %q: %s and %s", s.Name, prev, s.Path))
		}
		seen[s.Name] = s.Path
	}
	return problems
}

var frontmatterRe = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n`)

// Parse parses a skill markdown file with frontmatter. The body is everything
// after the frontmatter block.
func Parse(raw string) (Skill, error) {
	loc := frontmatterRe.FindStringSubmatchIndex(raw)
	if loc == nil {
		return Skill{}, fmt.Errorf("skill has no frontmatter (missing name/description block)")
	}
	var s Skill
	s.Name, s.Description, s.Model = parseFMBlock(raw[loc[2]:loc[3]])
	s.Prompt = strings.TrimSpace(raw[loc[1]:])
	if s.Name == "" {
		return s, fmt.Errorf("skill frontmatter is missing a name")
	}
	if s.Prompt == "" {
		return s, fmt.Errorf("skill %q has an empty body", s.Name)
	}
	return s, nil
}

func parseFMBlock(fm string) (name, description, model string) {
	for _, line := range strings.Split(fm, "\n") {
		switch {
		case strings.HasPrefix(line, "name:"):
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		case strings.HasPrefix(line, "description:"):
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		case strings.HasPrefix(line, "model:"):
			model = strings.TrimSpace(strings.TrimPrefix(line, "model:"))
		}
	}
	return
}
