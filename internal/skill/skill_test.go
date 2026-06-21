package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeSkill(t *testing.T, root, dir, name, desc, body string) string {
	t.Helper()
	path := filepath.Join(root, dir, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	content := "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write skill: %v", err)
	}
	return path
}

func TestParseFrontmatter(t *testing.T) {
	meta, err := parseFrontmatter("---\nname: sandbox-files\ndescription: file tools\n---\nbody")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if meta.Name != "sandbox-files" || meta.Description != "file tools" {
		t.Fatalf("bad metadata: %+v", meta)
	}
}

func TestParseFrontmatterErrors(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{name: "none", raw: "name: x", want: ErrMissingFrontmatter},
		{name: "missing name", raw: "---\ndescription: d\n---\n", want: ErrMissingName},
		{name: "missing description", raw: "---\nname: n\n---\n", want: ErrMissingDescription},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseFrontmatter(tc.raw)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestIndexerLoadStableAndSHA(t *testing.T) {
	root := t.TempDir()
	first := writeSkill(t, root, "b", "go-test", "go test runner", "Use go test ./...")
	second := writeSkill(t, root, "a", "sandbox-files", "sandbox file operations", "Use fs_read and fs_write.")

	idx, err := Indexer{Root: root}.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(idx.Skills) != 2 {
		t.Fatalf("want 2 skills, got %d", len(idx.Skills))
	}
	if idx.Skills[0].Path != second || idx.Skills[1].Path != first {
		t.Fatalf("paths not stable sorted: %+v", idx.Skills)
	}
	if idx.ByName["sandbox-files"].SHA256 == "" || len(idx.ByName["sandbox-files"].SHA256) != 64 {
		t.Fatalf("bad sha: %+v", idx.ByName["sandbox-files"])
	}

	if err := os.WriteFile(second, []byte("---\nname: sandbox-files\ndescription: sandbox file operations\n---\nchanged\n"), 0o644); err != nil {
		t.Fatalf("rewrite skill: %v", err)
	}
	next, err := Indexer{Root: root}.Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if next.ByName["sandbox-files"].SHA256 == idx.ByName["sandbox-files"].SHA256 {
		t.Fatalf("sha did not change")
	}
}

func TestIndexerMissingRootIsEmpty(t *testing.T) {
	idx, err := Indexer{Root: filepath.Join(t.TempDir(), "missing")}.Load()
	if err != nil {
		t.Fatalf("missing root should not error: %v", err)
	}
	if len(idx.Skills) != 0 || len(idx.ByName) != 0 {
		t.Fatalf("want empty index, got %+v", idx)
	}
}

func TestIndexerDuplicateName(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "a", "same", "first", "one")
	writeSkill(t, root, "b", "same", "second", "two")
	if _, err := (Indexer{Root: root}).Load(); err == nil {
		t.Fatalf("want duplicate name error")
	}
}

func TestRuleSelector(t *testing.T) {
	idx := Index{Skills: []Skill{
		{Name: "agentforge-overview", Description: "project architecture", Content: "runtime"},
		{Name: "sandbox-files", Description: "sandbox file operations with fs_read and fs_write", Content: "files"},
		{Name: "go-test", Description: "go unit testing", Content: "go test ./..."},
	}}
	got, err := RuleSelector{Index: idx, TopK: 2}.Select(context.Background(), "sandbox 文件工具怎么用 fs_write")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(got) != 1 || got[0].Name != "sandbox-files" {
		t.Fatalf("unexpected selection: %+v", got)
	}

	none, err := RuleSelector{Index: idx, TopK: 3}.Select(context.Background(), "unrelated")
	if err != nil {
		t.Fatalf("select unrelated: %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("want no matches, got %+v", none)
	}
}

func TestRuleSelectorTopK(t *testing.T) {
	idx := Index{Skills: []Skill{
		{Name: "a", Description: "debug shell", Content: "bash"},
		{Name: "b", Description: "debug logs", Content: "bash"},
		{Name: "c", Description: "debug tests", Content: "bash"},
	}}
	got, err := RuleSelector{Index: idx, TopK: 2}.Select(context.Background(), "debug")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("bad top-k: %+v", got)
	}
}

type countingSelector struct {
	calls int
	out   []Skill
}

func (s *countingSelector) Select(context.Context, string) ([]Skill, error) {
	s.calls++
	return s.out, nil
}

func TestCachedSelectorHit(t *testing.T) {
	next := &countingSelector{out: []Skill{{Name: "sandbox-files"}}}
	cached := &CachedSelector{Next: next, TTL: time.Minute, Capacity: 8}
	for i := 0; i < 2; i++ {
		got, err := cached.Select(context.Background(), "  Sandbox   Files ")
		if err != nil {
			t.Fatalf("select: %v", err)
		}
		if len(got) != 1 || got[0].Name != "sandbox-files" {
			t.Fatalf("bad cached result: %+v", got)
		}
	}
	if next.calls != 1 {
		t.Fatalf("want one delegate call, got %d", next.calls)
	}
}
