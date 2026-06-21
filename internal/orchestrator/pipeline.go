package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Pipeline is a tiny YAML-compatible DAG spec.
type Pipeline struct {
	Name  string
	Steps []PipelineStep
}

// PipelineStep is one DAG node.
type PipelineStep struct {
	ID        string
	Role      string
	Task      string
	DependsOn []string
}

// PipelineResult is returned by CLI/RPC.
type PipelineResult struct {
	Name    string
	Results []SubagentResult
	Status  string
	Error   string
}

// ParsePipelineFile parses the minimal W7 YAML subset.
func ParsePipelineFile(path string) (Pipeline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Pipeline{}, err
	}
	return ParsePipeline(string(raw))
}

// ParsePipeline parses:
// name: ...
// steps:
//   - id: ...
//     role: ...
//     task: ...
//     depends_on: [a,b]
func ParsePipeline(raw string) (Pipeline, error) {
	var p Pipeline
	sc := bufio.NewScanner(strings.NewReader(raw))
	var cur *PipelineStep
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " \t")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || trim == "steps:" {
			continue
		}
		if strings.HasPrefix(trim, "name:") {
			p.Name = cleanYAMLValue(strings.TrimPrefix(trim, "name:"))
			continue
		}
		if strings.HasPrefix(trim, "- ") {
			if cur != nil {
				p.Steps = append(p.Steps, *cur)
			}
			cur = &PipelineStep{}
			kv := strings.TrimSpace(strings.TrimPrefix(trim, "- "))
			if strings.HasPrefix(kv, "id:") {
				cur.ID = cleanYAMLValue(strings.TrimPrefix(kv, "id:"))
			}
			continue
		}
		if cur == nil {
			continue
		}
		key, val, ok := strings.Cut(trim, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "id":
			cur.ID = cleanYAMLValue(val)
		case "role":
			cur.Role = cleanYAMLValue(val)
		case "task":
			cur.Task = cleanYAMLValue(val)
		case "depends_on":
			cur.DependsOn = parseList(val)
		}
	}
	if cur != nil {
		p.Steps = append(p.Steps, *cur)
	}
	if err := sc.Err(); err != nil {
		return Pipeline{}, err
	}
	if p.Name == "" {
		p.Name = "pipeline"
	}
	return p, ValidatePipeline(p)
}

// ValidatePipeline checks ids and dependencies.
func ValidatePipeline(p Pipeline) error {
	seen := map[string]struct{}{}
	for _, st := range p.Steps {
		if st.ID == "" {
			return fmt.Errorf("%w: empty id", ErrMissingStep)
		}
		if _, ok := seen[st.ID]; ok {
			return fmt.Errorf("%w: %s", ErrDuplicateStep, st.ID)
		}
		seen[st.ID] = struct{}{}
		if strings.TrimSpace(st.Task) == "" {
			return fmt.Errorf("%w: %s has empty task", ErrMissingStep, st.ID)
		}
	}
	for _, st := range p.Steps {
		for _, dep := range st.DependsOn {
			if _, ok := seen[dep]; !ok {
				return fmt.Errorf("%w: %s depends on %s", ErrMissingStep, st.ID, dep)
			}
		}
	}
	if _, err := TopologicalOrder(p); err != nil {
		return err
	}
	return nil
}

// TopologicalOrder returns deterministic dependency order.
func TopologicalOrder(p Pipeline) ([]PipelineStep, error) {
	byID := map[string]PipelineStep{}
	indeg := map[string]int{}
	children := map[string][]string{}
	for _, st := range p.Steps {
		byID[st.ID] = st
		indeg[st.ID] = len(st.DependsOn)
		for _, dep := range st.DependsOn {
			children[dep] = append(children[dep], st.ID)
		}
	}
	var ready []string
	for id, n := range indeg {
		if n == 0 {
			ready = append(ready, id)
		}
	}
	sort.Strings(ready)
	var out []PipelineStep
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		out = append(out, byID[id])
		for _, child := range children[id] {
			indeg[child]--
			if indeg[child] == 0 {
				ready = append(ready, child)
				sort.Strings(ready)
			}
		}
	}
	if len(out) != len(p.Steps) {
		return nil, ErrPipelineCycle
	}
	return out, nil
}

// RunPipeline executes each step through Supervisor with dependency outputs appended.
func RunPipeline(ctx context.Context, sup *Supervisor, p Pipeline, parentRunID, traceID, userID, model string) (PipelineResult, error) {
	ordered, err := TopologicalOrder(p)
	if err != nil {
		return PipelineResult{Name: p.Name, Status: "error", Error: err.Error()}, err
	}
	results := map[string]SubagentResult{}
	out := PipelineResult{Name: p.Name, Status: "ok"}
	for _, st := range ordered {
		task := st.Task
		if len(st.DependsOn) > 0 {
			var b strings.Builder
			b.WriteString(task)
			b.WriteString("\n\nDependency outputs:\n")
			for _, dep := range st.DependsOn {
				r := results[dep]
				b.WriteString(fmt.Sprintf("- %s (%s): %s\n", dep, r.Status, r.Summary))
			}
			task = b.String()
		}
		res, err := sup.Dispatch(ctx, parentRunID, traceID, userID, model, 0, SubagentRequest{
			Role: st.Role,
			Task: task,
		})
		results[st.ID] = res
		out.Results = append(out.Results, res)
		if err != nil {
			out.Status = "error"
			out.Error = err.Error()
			return out, err
		}
	}
	return out, nil
}

func cleanYAMLValue(s string) string {
	return strings.Trim(strings.TrimSpace(s), `"'`)
}

func parseList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := cleanYAMLValue(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
