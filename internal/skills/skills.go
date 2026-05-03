// Package skills discovers per-repo agent capabilities under
// `<workspace>/.symphony/skills/<name>/`. A skill is a directory containing
// `SKILL.md` (description + usage) and any number of `*.sh` invocation
// scripts. The skill inventory is injected into the prompt via a
// `{{ skills }}` placeholder so the agent knows which capabilities exist
// without spending tokens to discover them.
package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	relSkillsRoot = ".symphony/skills"
	skillFile     = "SKILL.md"
)

type Skill struct {
	Name        string
	Path        string   // absolute path to the skill directory
	Description string   // first non-header paragraph from SKILL.md
	Scripts     []string // basenames of *.sh scripts inside the skill dir
}

// Discover scans for skills under workspaceRoot/.symphony/skills/. Missing or
// unreadable directories return (nil, nil) — skills are optional.
func Discover(workspaceRoot string) ([]Skill, error) {
	root := filepath.Join(workspaceRoot, relSkillsRoot)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("skills: read %s: %w", root, err)
	}
	var skills []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		s, err := loadSkill(dir, e.Name())
		if err != nil {
			// A malformed skill is logged-as-skipped via the returned slice;
			// callers can warn but the loop continues so one bad skill does
			// not blank the inventory.
			continue
		}
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, nil
}

func loadSkill(dir, name string) (Skill, error) {
	skill := Skill{Name: name, Path: dir}
	mdPath := filepath.Join(dir, skillFile)
	if data, err := os.ReadFile(mdPath); err == nil {
		skill.Description = firstParagraph(string(data))
	}
	scripts, err := filepath.Glob(filepath.Join(dir, "*.sh"))
	if err != nil {
		return Skill{}, err
	}
	for _, s := range scripts {
		skill.Scripts = append(skill.Scripts, filepath.Base(s))
	}
	sort.Strings(skill.Scripts)
	return skill, nil
}

// firstParagraph returns the first non-empty, non-header line block from
// SKILL.md, capped at 240 characters so prompt bloat stays bounded.
func firstParagraph(s string) string {
	scanner := bufio.NewScanner(strings.NewReader(s))
	var buf []string
	collecting := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line == "" {
			if collecting {
				break
			}
			continue
		}
		collecting = true
		buf = append(buf, line)
	}
	out := strings.Join(buf, " ")
	if len(out) > 240 {
		out = out[:237] + "..."
	}
	return out
}

// Render formats the skill inventory for prompt injection. Empty input
// renders a single-line note so the agent does not see a dangling header.
func Render(skills []Skill) string {
	if len(skills) == 0 {
		return "_No skills configured for this repo._"
	}
	var b strings.Builder
	b.WriteString("Available skills (invoke via the listed scripts; read each skill's SKILL.md for usage):\n\n")
	for _, s := range skills {
		fmt.Fprintf(&b, "- **%s** — %s\n", s.Name, fallback(s.Description, "(no description)"))
		fmt.Fprintf(&b, "  path: %s\n", s.Path)
		if len(s.Scripts) > 0 {
			fmt.Fprintf(&b, "  scripts: %s\n", strings.Join(s.Scripts, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func fallback(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
