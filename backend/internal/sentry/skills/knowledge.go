package skills

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The knowledge layer is multi-skill (#219): knowledge/ holds one
// subdirectory per skill, each with a SKILL.md and a references/ dir.
// load_skill takes the skill name; load_skill_reference takes (skill, name).
// An on-disk override (sentry.skill_dir) is a PARENT directory with the same
// one-subdir-per-skill layout.
//
//go:embed knowledge
var embeddedSkill embed.FS

const skillRoot = "knowledge"

// skillNames lists the available skills: subdirectories of skillDir when
// set, else of the embedded knowledge tree.
func skillNames(skillDir string) []string {
	var names []string
	if skillDir != "" {
		entries, _ := os.ReadDir(skillDir)
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
	} else {
		entries, _ := fs.ReadDir(embeddedSkill, skillRoot)
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
	}
	sort.Strings(names)
	return names
}

// readSkillFile reads skill/rel from skillDir when set, else the embedded
// copy.
func readSkillFile(skillDir, skill, rel string) ([]byte, error) {
	if skillDir != "" {
		return os.ReadFile(filepath.Join(skillDir, skill, rel))
	}
	return embeddedSkill.ReadFile(skillRoot + "/" + skill + "/" + rel)
}

func referenceNames(skillDir, skill string) []string {
	var names []string
	if skillDir != "" {
		entries, _ := os.ReadDir(filepath.Join(skillDir, skill, "references"))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
	} else {
		_ = fs.WalkDir(embeddedSkill, skillRoot+"/"+skill+"/references", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				names = append(names, filepath.Base(p))
			}
			return nil
		})
	}
	sort.Strings(names)
	return names
}

// knownSkill reports whether name is one of the available skills.
func knownSkill(skillDir, name string) bool {
	for _, s := range skillNames(skillDir) {
		if s == name {
			return true
		}
	}
	return false
}

type loadSkill struct{ dir string }

func NewLoadSkill(dir string) Skill { return loadSkill{dir} }

func (loadSkill) Name() string { return "load_skill" }
func (loadSkill) Description() string {
	return "Load a knowledge skill by name. Available: timeplus-sql-guide (Timeplus SQL syntax, table(), windows, MVs — load before writing non-trivial SQL) and agentguard-guide (AgentGuard's streams and the canonical queries behind every API resource — load before querying AgentGuard data)."
}
func (loadSkill) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "skill name, e.g. timeplus-sql-guide or agentguard-guide"},
		},
		"required": []string{"name"},
	}
}
func (loadSkill) Access() AccessClass { return ReadOnly }
func (l loadSkill) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(args, &a)
	a.Name = filepath.Base(strings.TrimSpace(a.Name)) // no traversal
	if a.Name == "" || a.Name == "." || !knownSkill(l.dir, a.Name) {
		return "unknown skill; available skills: " + strings.Join(skillNames(l.dir), ", "), nil
	}
	if rc.Loaded["skill:"+a.Name] {
		return "(already loaded above)", nil
	}
	b, err := readSkillFile(l.dir, a.Name, "SKILL.md")
	if err != nil {
		return fmt.Sprintf("skill unavailable: %v", err), nil
	}
	rc.Loaded["skill:"+a.Name] = true
	return string(b), nil
}

type loadSkillRef struct{ dir string }

func NewLoadSkillReference(dir string) Skill { return loadSkillRef{dir} }

func (loadSkillRef) Name() string { return "load_skill_reference" }
func (loadSkillRef) Description() string {
	return "Load one deep-dive reference file from a knowledge skill (e.g. skill=timeplus-sql-guide name=SQL_REFERENCE.md, or skill=agentguard-guide name=COST.md)."
}
func (loadSkillRef) Params() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill": map[string]any{"type": "string", "description": "skill name, e.g. timeplus-sql-guide or agentguard-guide"},
			"name":  map[string]any{"type": "string", "description": "reference file name"},
		},
		"required": []string{"skill", "name"},
	}
}
func (loadSkillRef) Access() AccessClass { return ReadOnly }
func (l loadSkillRef) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Skill string `json:"skill"`
		Name  string `json:"name"`
	}
	_ = json.Unmarshal(args, &a)
	a.Skill = filepath.Base(strings.TrimSpace(a.Skill)) // no traversal
	a.Name = filepath.Base(strings.TrimSpace(a.Name))
	if a.Skill == "" || a.Skill == "." || !knownSkill(l.dir, a.Skill) {
		return "unknown skill; available skills: " + strings.Join(skillNames(l.dir), ", "), nil
	}
	key := "ref:" + a.Skill + "/" + a.Name
	if rc.Loaded[key] {
		return "(already loaded above)", nil
	}
	b, err := readSkillFile(l.dir, a.Skill, "references/"+a.Name)
	if err != nil {
		return "unknown reference; available references in " + a.Skill + ": " + strings.Join(referenceNames(l.dir, a.Skill), ", "), nil
	}
	rc.Loaded[key] = true
	return string(b), nil
}
