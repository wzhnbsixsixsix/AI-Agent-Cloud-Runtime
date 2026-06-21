// Package skill implements W5 dynamic skill indexing and selection.
package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrMissingFrontmatter = errors.New("skill missing frontmatter")
	ErrMissingName        = errors.New("skill frontmatter missing name")
	ErrMissingDescription = errors.New("skill frontmatter missing description")
)

// Skill is the indexed form of a SKILL.md file.
type Skill struct {
	Name        string
	Description string
	SHA256      string
	Path        string
	Content     string
}

// Index is an immutable snapshot of all indexed skills.
type Index struct {
	Skills []Skill
	ByName map[string]Skill
}

// Indexer scans a skill root for **/SKILL.md files.
type Indexer struct {
	Root string
}

// Load returns a deterministic snapshot of all skills under Root. A missing
// root is treated as an empty index so skill loading can be optional in dev.
func (i Indexer) Load() (Index, error) {
	root := strings.TrimSpace(i.Root)
	if root == "" {
		root = "skills"
	}
	if st, err := os.Stat(root); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Index{ByName: map[string]Skill{}}, nil
		}
		return Index{}, err
	} else if !st.IsDir() {
		return Index{}, fmt.Errorf("skill root is not a directory: %s", root)
	}

	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == "SKILL.md" {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return Index{}, err
	}
	sort.Strings(paths)

	out := Index{Skills: make([]Skill, 0, len(paths)), ByName: make(map[string]Skill, len(paths))}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Index{}, err
		}
		meta, err := parseFrontmatter(string(raw))
		if err != nil {
			return Index{}, fmt.Errorf("%s: %w", path, err)
		}
		if _, ok := out.ByName[meta.Name]; ok {
			return Index{}, fmt.Errorf("duplicate skill name %q", meta.Name)
		}
		sum := sha256.Sum256(raw)
		s := Skill{
			Name:        meta.Name,
			Description: meta.Description,
			SHA256:      hex.EncodeToString(sum[:]),
			Path:        path,
			Content:     string(raw),
		}
		out.Skills = append(out.Skills, s)
		out.ByName[s.Name] = s
	}
	return out, nil
}

type metadata struct {
	Name        string
	Description string
}

func parseFrontmatter(raw string) (metadata, error) {
	raw = strings.TrimPrefix(raw, "\ufeff")
	if !strings.HasPrefix(raw, "---\n") && !strings.HasPrefix(raw, "---\r\n") {
		return metadata{}, ErrMissingFrontmatter
	}

	lines := strings.Split(raw, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return metadata{}, ErrMissingFrontmatter
	}
	end := -1
	for idx := 1; idx < len(lines); idx++ {
		if strings.TrimSpace(lines[idx]) == "---" {
			end = idx
			break
		}
	}
	if end == -1 {
		return metadata{}, ErrMissingFrontmatter
	}

	var meta metadata
	for _, line := range lines[1:end] {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "name":
			meta.Name = val
		case "description":
			meta.Description = val
		}
	}
	if meta.Name == "" {
		return metadata{}, ErrMissingName
	}
	if meta.Description == "" {
		return metadata{}, ErrMissingDescription
	}
	return meta, nil
}

// Selector chooses relevant skills for a query.
type Selector interface {
	Select(ctx context.Context, query string) ([]Skill, error)
}

// Renderer turns selected skills into a system message.
type Renderer struct{}

// RenderSystemMessage renders full SKILL.md contents with stable separators.
func (Renderer) RenderSystemMessage(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Relevant AgentForge skills have been dynamically loaded for this run.\n")
	b.WriteString("Follow these skill instructions when they apply, while preserving the user's request.\n")
	for _, s := range skills {
		b.WriteString("\n--- SKILL: ")
		b.WriteString(s.Name)
		b.WriteString(" (sha256=")
		if len(s.SHA256) >= 12 {
			b.WriteString(s.SHA256[:12])
		} else {
			b.WriteString(s.SHA256)
		}
		b.WriteString(") ---\n")
		b.WriteString(strings.TrimSpace(s.Content))
		b.WriteByte('\n')
	}
	return b.String()
}
