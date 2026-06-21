package rag

import (
	"sort"
	"strings"
	"unicode"
)

// KeywordReranker adds a small exact-token overlap bonus and keeps ordering deterministic.
type KeywordReranker struct{}

// Rerank reorders results by final score.
func (KeywordReranker) Rerank(query string, results []Result) []Result {
	if len(results) == 0 {
		return nil
	}
	toks := tokenize(query)
	out := make([]Result, len(results))
	copy(out, results)
	for i := range out {
		out[i].Score += float64(overlap(toks, tokenize(out[i].Content))) * 0.03
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Ordinal < out[j].Ordinal
	})
	return out
}

func tokenize(s string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range strings.FieldsFunc(normalizeText(s), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	}) {
		part = strings.Trim(part, "_-")
		if len([]rune(part)) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	sort.Strings(out)
	return out
}

func overlap(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]struct{}, len(a))
	for _, x := range a {
		set[x] = struct{}{}
	}
	var n int
	for _, x := range b {
		if _, ok := set[x]; ok {
			n++
		}
	}
	return n
}
