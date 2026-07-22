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

//go:embed knowledge/timeplus-sql-guide
var embeddedSkill embed.FS

const skillRoot = "knowledge/timeplus-sql-guide"

// readSkillFile reads rel from skillDir when set, else the embedded copy.
func readSkillFile(skillDir, rel string) ([]byte, error) {
	if skillDir != "" {
		return os.ReadFile(filepath.Join(skillDir, rel))
	}
	return embeddedSkill.ReadFile(skillRoot + "/" + rel)
}

func referenceNames(skillDir string) []string {
	var names []string
	if skillDir != "" {
		entries, _ := os.ReadDir(filepath.Join(skillDir, "references"))
		for _, e := range entries {
			if !e.IsDir() {
				names = append(names, e.Name())
			}
		}
	} else {
		_ = fs.WalkDir(embeddedSkill, skillRoot+"/references", func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				names = append(names, filepath.Base(p))
			}
			return nil
		})
	}
	sort.Strings(names)
	return names
}

type loadSkill struct{ dir string }

func NewLoadSkill(dir string) Skill { return loadSkill{dir} }

func (loadSkill) Name() string { return "load_skill" }
func (loadSkill) Description() string {
	return "Load the Timeplus SQL guide (streaming SQL syntax, table(), windows, MVs). Call once before writing non-trivial SQL."
}
func (loadSkill) Params() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (loadSkill) Access() AccessClass { return ReadOnly }
func (l loadSkill) Execute(_ context.Context, rc *RunContext, _ json.RawMessage) (string, error) {
	if rc.Loaded["skill"] {
		return "(already loaded above)", nil
	}
	b, err := readSkillFile(l.dir, "SKILL.md")
	if err != nil {
		return fmt.Sprintf("skill unavailable: %v", err), nil
	}
	rc.Loaded["skill"] = true
	return string(b), nil
}

type loadSkillRef struct{ dir string }

func NewLoadSkillReference(dir string) Skill { return loadSkillRef{dir} }

func (loadSkillRef) Name() string { return "load_skill_reference" }
func (loadSkillRef) Description() string {
	return "Load one deep-dive reference file from the Timeplus SQL guide (e.g. functions, UDFs, joins)."
}
func (loadSkillRef) Params() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string", "description": "reference file name"}},
		"required":   []string{"name"},
	}
}
func (loadSkillRef) Access() AccessClass { return ReadOnly }
func (l loadSkillRef) Execute(_ context.Context, rc *RunContext, args json.RawMessage) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(args, &a)
	a.Name = filepath.Base(a.Name) // no traversal
	if rc.Loaded["ref:"+a.Name] {
		return "(already loaded above)", nil
	}
	b, err := readSkillFile(l.dir, "references/"+a.Name)
	if err != nil {
		return "unknown reference; available references: " + strings.Join(referenceNames(l.dir), ", "), nil
	}
	rc.Loaded["ref:"+a.Name] = true
	return string(b), nil
}
