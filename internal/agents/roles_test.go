package agents

import "testing"

func TestDefaultsCoverExpectedRoles(t *testing.T) {
	for _, name := range []string{"plan", "build", "test", "review"} {
		r, ok := Get(name)
		if !ok {
			t.Fatalf("role %q missing", name)
		}
		if r.SystemPrompt == "" {
			t.Errorf("role %q has empty system prompt", name)
		}
		if r.MaxSteps <= 0 {
			t.Errorf("role %q has non-positive MaxSteps", name)
		}
		if r.MaxTokens <= 0 {
			t.Errorf("role %q has non-positive MaxTokens", name)
		}
	}
}

func TestPlanIsReadOnlyAndCannotShell(t *testing.T) {
	plan := MustGet("plan")
	if !plan.ReadOnly {
		t.Fatal("plan must be read-only")
	}
	for _, forbidden := range []string{"bash", "write", "edit", "task"} {
		if plan.AllowsTool(forbidden) {
			t.Errorf("plan must not allow %q", forbidden)
		}
	}
	for _, allowed := range []string{"read", "grep", "glob", "ls"} {
		if !plan.AllowsTool(allowed) {
			t.Errorf("plan must allow %q", allowed)
		}
	}
}

func TestBuildAllowsEverything(t *testing.T) {
	build := MustGet("build")
	if build.ReadOnly {
		t.Fatal("build must not be read-only")
	}
	for _, tool := range []string{"read", "write", "bash", "process"} {
		if !build.AllowsTool(tool) {
			t.Errorf("build must allow %q", tool)
		}
	}
}

func TestReviewIsReadOnly(t *testing.T) {
	review := MustGet("review")
	if !review.ReadOnly {
		t.Fatal("review must be read-only")
	}
	if review.AllowsTool("write") || review.AllowsTool("edit") {
		t.Fatal("review must not allow write/edit")
	}
}

func TestNamesSorted(t *testing.T) {
	names := Names()
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("Names() not sorted: %v", names)
		}
	}
}

func TestUnknownRole(t *testing.T) {
	if _, ok := Get("nope"); ok {
		t.Fatal("unexpected role")
	}
}
