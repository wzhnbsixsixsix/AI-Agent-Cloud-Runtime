package controlplane

import "testing"

func TestCreateAgentInputValidate(t *testing.T) {
	valid := CreateAgentInput{Name: "researcher", Role: "Research", Image: "alpine:3.19"}
	if err := valid.Validate("alpine:3.19"); err != nil {
		t.Fatalf("valid input: %v", err)
	}
	if err := (CreateAgentInput{Name: "x", Role: "Research"}).Validate("alpine:3.19"); err == nil {
		t.Fatal("expected short name rejection")
	}
	if err := (CreateAgentInput{Name: "researcher", Role: "Research", Image: "unknown:latest"}).Validate("alpine:3.19"); err == nil {
		t.Fatal("expected image rejection")
	}
	for _, model := range []string{DefaultAgentModel, ModelScopeAgentModel} {
		if err := (CreateAgentInput{Name: "researcher", Role: "Research", Model: model}).Validate("alpine:3.19"); err != nil {
			t.Fatalf("supported model %q rejected: %v", model, err)
		}
	}
	if err := (CreateAgentInput{Name: "researcher", Role: "Research", Model: "unknown/model"}).Validate("alpine:3.19"); err == nil {
		t.Fatal("expected unsupported model rejection")
	}
}

func TestSafeWorkspacePath(t *testing.T) {
	for _, input := range []string{"../secret", "nested/../../secret", "..\\secret"} {
		if _, err := safeWorkspacePath(input); err == nil {
			t.Fatalf("expected path rejection for %q", input)
		}
	}
	got, err := safeWorkspacePath("reports/today.md")
	if err != nil || got != "reports/today.md" {
		t.Fatalf("got %q, %v", got, err)
	}
}
