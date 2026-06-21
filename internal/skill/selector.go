package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// RuleSelector scores skills by deterministic keyword overlap.
type RuleSelector struct {
	Index Index
	TopK  int
}

// Select returns the best matching skills for query.
func (s RuleSelector) Select(_ context.Context, query string) ([]Skill, error) {
	topK := s.TopK
	if topK < 0 {
		topK = 0
	}
	query = normalizeQuery(query)
	if query == "" || topK == 0 || len(s.Index.Skills) == 0 {
		return nil, nil
	}
	tokens := tokenize(query)
	type scored struct {
		skill Skill
		score int
	}
	var matches []scored
	for _, sk := range s.Index.Skills {
		score := scoreSkill(query, tokens, sk)
		if score > 0 {
			matches = append(matches, scored{skill: sk, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].skill.Name != matches[j].skill.Name {
			return matches[i].skill.Name < matches[j].skill.Name
		}
		return matches[i].skill.Path < matches[j].skill.Path
	})
	if topK > len(matches) {
		topK = len(matches)
	}
	out := make([]Skill, 0, topK)
	for idx := 0; idx < topK; idx++ {
		out = append(out, matches[idx].skill)
	}
	return out, nil
}

func scoreSkill(query string, tokens []string, sk Skill) int {
	name := strings.ToLower(sk.Name)
	desc := strings.ToLower(sk.Description)
	content := strings.ToLower(sk.Content)
	score := 0
	if len([]rune(name)) >= 3 && strings.Contains(query, name) {
		score += 12
	}
	for _, tk := range tokens {
		switch {
		case tk == name:
			score += 10
		case strings.Contains(name, tk):
			score += 6
		case strings.Contains(desc, tk):
			score += 3
		case len(tk) >= 4 && strings.Contains(content, tk):
			score++
		}
	}
	return score
}

func normalizeQuery(q string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(q))), " ")
}

func tokenize(q string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, part := range strings.FieldsFunc(q, func(r rune) bool {
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

// CachedSelector wraps another selector with an in-memory TTL cache.
type CachedSelector struct {
	Next     Selector
	TTL      time.Duration
	Capacity int

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	expires time.Time
	hitAt   time.Time
	skills  []Skill
}

// Select returns cached results when possible and delegates otherwise.
func (c *CachedSelector) Select(ctx context.Context, query string) ([]Skill, error) {
	if c == nil || c.Next == nil {
		return nil, nil
	}
	ttl := c.TTL
	capacity := c.Capacity
	if ttl <= 0 || capacity <= 0 {
		return c.Next.Select(ctx, query)
	}
	key := cacheKey(query)
	now := time.Now()

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]cacheEntry)
	}
	if ent, ok := c.entries[key]; ok && now.Before(ent.expires) {
		ent.hitAt = now
		c.entries[key] = ent
		out := cloneSkills(ent.skills)
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()

	selected, err := c.Next.Select(ctx, query)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]cacheEntry)
	}
	c.entries[key] = cacheEntry{
		expires: now.Add(ttl),
		hitAt:   now,
		skills:  cloneSkills(selected),
	}
	c.evictLocked(capacity)
	c.mu.Unlock()
	return selected, nil
}

func (c *CachedSelector) evictLocked(capacity int) {
	for len(c.entries) > capacity {
		var oldestKey string
		var oldest time.Time
		first := true
		for k, ent := range c.entries {
			if first || ent.hitAt.Before(oldest) {
				first = false
				oldest = ent.hitAt
				oldestKey = k
			}
		}
		delete(c.entries, oldestKey)
	}
}

func cacheKey(query string) string {
	sum := sha256.Sum256([]byte(normalizeQuery(query)))
	return hex.EncodeToString(sum[:])
}

func cloneSkills(in []Skill) []Skill {
	if len(in) == 0 {
		return nil
	}
	out := make([]Skill, len(in))
	copy(out, in)
	return out
}

// LLMSelector is a placeholder for a future function-call based selector.
type LLMSelector struct{}

// Select is intentionally not implemented in W5; the worker uses RuleSelector.
func (LLMSelector) Select(context.Context, string) ([]Skill, error) {
	return nil, nil
}
