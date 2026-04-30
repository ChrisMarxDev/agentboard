package store

import (
	"reflect"
	"testing"
)

func TestCheckShape_TaskMissingTitle(t *testing.T) {
	warnings := CheckShape("tasks/task-42", map[string]any{
		"status": "in-progress",
	})
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1: %+v", len(warnings), warnings)
	}
	w := warnings[0]
	if w.Code != "shape_hint" {
		t.Errorf("code = %q, want shape_hint", w.Code)
	}
	if w.Shape != "task" {
		t.Errorf("shape = %q, want task", w.Shape)
	}
	if !reflect.DeepEqual(w.MissingSuggestedFields, []string{"title"}) {
		t.Errorf("missing fields = %v, want [title]", w.MissingSuggestedFields)
	}
}

func TestCheckShape_NestedTask(t *testing.T) {
	warnings := CheckShape("acme/tasks/onboarding", map[string]any{})
	if len(warnings) != 1 {
		t.Fatalf("nested task should still match: got %d warnings", len(warnings))
	}
}

func TestCheckShape_FullyShapedTaskSilent(t *testing.T) {
	warnings := CheckShape("tasks/task-42", map[string]any{
		"title":  "Build it",
		"status": "todo",
	})
	if len(warnings) != 0 {
		t.Errorf("complete shape should be silent; got %+v", warnings)
	}
}

func TestCheckShape_NoGlobMatchesSilent(t *testing.T) {
	warnings := CheckShape("handbook/intro", map[string]any{})
	if len(warnings) != 0 {
		t.Errorf("non-shaped paths should be silent; got %+v", warnings)
	}
}

func TestCheckShape_MetricMissingValueAndLabel(t *testing.T) {
	warnings := CheckShape("metrics/dau", map[string]any{})
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warnings))
	}
	w := warnings[0]
	if w.Shape != "metric" {
		t.Errorf("shape = %q, want metric", w.Shape)
	}
	if !reflect.DeepEqual(w.MissingSuggestedFields, []string{"value", "label"}) {
		t.Errorf("missing fields = %v, want [value, label]", w.MissingSuggestedFields)
	}
}

func TestCheckShape_SkillManifest(t *testing.T) {
	warnings := CheckShape("skills/agentboard/SKILL", map[string]any{
		"name": "agentboard",
	})
	if len(warnings) != 1 {
		t.Fatalf("got %d warnings, want 1", len(warnings))
	}
	if !reflect.DeepEqual(warnings[0].MissingSuggestedFields, []string{"description"}) {
		t.Errorf("missing fields = %v, want [description]", warnings[0].MissingSuggestedFields)
	}
}

func TestCheckShape_OneWarningPerShape(t *testing.T) {
	// `tasks/*` AND `tasks/**` both match `tasks/task-42`. Without
	// dedup we'd emit two identical warnings.
	warnings := CheckShape("tasks/task-42", map[string]any{})
	if len(warnings) > 1 {
		t.Errorf("got %d warnings — overlapping globs must dedupe by shape name", len(warnings))
	}
}
