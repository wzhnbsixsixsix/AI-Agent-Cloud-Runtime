package controlplane

import (
	"archive/tar"
	"bytes"
	"testing"
)

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

func TestReadWorkspaceEntriesStripsArchiveRoot(t *testing.T) {
	var archive bytes.Buffer
	tw := tar.NewWriter(&archive)
	for _, header := range []*tar.Header{
		{Name: "workspace", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "workspace/tt", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "workspace/note.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 3},
		{Name: "workspace/tt/nested.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 0},
	} {
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if header.Name == "workspace/note.txt" {
			_, _ = tw.Write([]byte("abc"))
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	entries, err := readWorkspaceEntries(bytes.NewReader(archive.Bytes()), "", "workspace")
	if err != nil {
		t.Fatalf("read entries: %v", err)
	}
	if len(entries) != 2 || entries[0].Path != "tt" || !entries[0].Directory || entries[1].Path != "note.txt" || entries[1].Directory {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}
