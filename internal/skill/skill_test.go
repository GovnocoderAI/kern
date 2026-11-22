package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// testProvider is intentionally unused; kept for future agent integration tests.

// TestSkillJSON verifies that Skill can be marshaled to JSON.
func TestSkillJSON(t *testing.T) {
	s := Skill{
		Name:        "test",
		Description: "desc",
		Prompt:      "body",
		Model:       "m1",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back Skill
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Name != s.Name || back.Description != s.Description || back.Prompt != s.Prompt || back.Model != s.Model {
		t.Fatalf("round-trip mismatch: %+v vs %+v", back, s)
	}
}

// TestSkillValidateDuplicate checks that Validate() detects duplicates.
func TestSkillValidateDuplicate(t *testing.T) {
	skills := []Skill{
		{Name: "a", Prompt: "p"},
		{Name: "a", Prompt: "q"},
	}
	problems := Validate(skills)
	if len(problems) == 0 {
		t.Fatal("expected duplicate problem")
	}
}

// TestSkillValidateMissingName checks that Validate() flags missing names.
func TestSkillValidateMissingName(t *testing.T) {
	skills := []Skill{
		{Name: "", Prompt: "p"},
	}
	problems := Validate(skills)
	if len(problems) == 0 {
		t.Fatal("expected missing name problem")
	}
}

// TestSkillValidateEmptyBody checks that Validate() flags empty bodies.
func TestSkillValidateEmptyBody(t *testing.T) {
	skills := []Skill{
		{Name: "a", Prompt: ""},
	}
	problems := Validate(skills)
	if len(problems) == 0 {
		t.Fatal("expected empty body problem")
	}
}

// writeFile is a helper that creates parent directories.
func writeFile(path string, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// TestSkillLoadProject verifies that LoadProject finds skills in .kern/skills/.
func TestSkillLoadProject(t *testing.T) {
	dir := t.TempDir()
	// Create .kern/skills/test.md
	path := filepath.Join(dir, ".kern", "skills", "test.md")
	if err := writeFile(path,
		"---\nname: test-skill\ndescription: test\n---\nTest prompt body"); err != nil {
		t.Fatal(err)
	}
	skills := LoadProject(dir)
	if len(skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(skills))
	}
	if skills[0].Name != "test-skill" {
		t.Fatalf("name = %q, want test-skill", skills[0].Name)
	}
}

// TestSkillLoadGlobal verifies that LoadGlobal finds skills in ~/.config/kern/skills/.
func TestSkillLoadGlobal(t *testing.T) {
	dir := t.TempDir()
	// Create global skill in KERN_CONFIG_DIR
	if err := writeFile(filepath.Join(dir, "skills", "global.md"),
		"---\nname: global-skill\ndescription: global\n---\nGlobal prompt."); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KERN_CONFIG_DIR", dir)
	skills := LoadGlobal()
	if len(skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(skills))
	}
	if skills[0].Name != "global-skill" {
		t.Fatalf("name = %q, want global-skill", skills[0].Name)
	}
}

// TestSkillProjectOverridesGlobal verifies project skill overrides global.
func TestSkillProjectOverridesGlobal(t *testing.T) {
	projectDir := t.TempDir()
	globalDir := t.TempDir()
	// Same name in both
	if err := writeFile(filepath.Join(projectDir, ".kern", "skills", "dup.md"),
		"---\nname: dup\ndescription: project\n---\nProject body."); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(globalDir, "dup.md"),
		"---\nname: dup\ndescription: global\n---\nGlobal body."); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KERN_CONFIG_DIR", globalDir)
	skills := Load(projectDir)
	if len(skills) != 1 {
		t.Fatalf("skills = %d, want 1", len(skills))
	}
	if skills[0].Description != "project" {
		t.Fatalf("description = %q, want project", skills[0].Description)
	}
}

// TestSkillBOM verifies BOM handling.
func TestSkillBOM(t *testing.T) {
	raw := "\uFEFF---\nname: bom\n---\nbody"
	s, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "bom" {
		t.Fatalf("name = %q, want bom", s.Name)
	}
}

// TestSkillCRLF verifies CRLF handling.
func TestSkillCRLF(t *testing.T) {
	raw := "---\r\nname: crlf\r\n---\r\nbody"
	s, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "crlf" {
		t.Fatalf("name = %q, want crlf", s.Name)
	}
}

// TestSkillNoFrontmatter verifies error for missing frontmatter.
func TestSkillNoFrontmatter(t *testing.T) {
	_, err := Parse("just a body without frontmatter")
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

// TestSkillEmptyBody verifies error for empty body.
func TestSkillEmptyBody(t *testing.T) {
	_, err := Parse("---\nname: x\n---\n")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

// TestSkillMissingName verifies error for missing name.
func TestSkillMissingName(t *testing.T) {
	_, err := Parse("---\ndescription: x\n---\nbody")
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}
