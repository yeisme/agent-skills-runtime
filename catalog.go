package skillsruntime

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type CatalogMatch struct {
	Entry  CatalogEntry `json:"entry"`
	Score  int          `json:"score"`
	Reason string       `json:"reason"`
}

// List returns a stable name-sorted catalog view.
func (c Catalog) List(includeDependencies bool) []CatalogEntry {
	entries := make([]CatalogEntry, 0, len(c.Entries))
	for _, entry := range c.Entries {
		if includeDependencies || entry.Role == SkillRoleEntry {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// Describe finds an entry by exact Skill name.
func (c Catalog) Describe(name string) (CatalogEntry, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, entry := range c.Entries {
		if entry.Name == name {
			return entry, true
		}
	}
	return CatalogEntry{}, false
}

// Search performs deterministic local matching without network access.
func (c Catalog) Search(query string, includeDependencies bool) []CatalogMatch {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil
	}
	matches := make([]CatalogMatch, 0)
	for _, entry := range c.Entries {
		if !includeDependencies && entry.Role != SkillRoleEntry {
			continue
		}
		score, reason := scoreCatalogEntry(entry, terms)
		if score > 0 {
			matches = append(matches, CatalogMatch{Entry: entry, Score: score, Reason: reason})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return matches[i].Entry.Name < matches[j].Entry.Name
	})
	return matches
}

// Suggest returns at most limit installed-compatible, deterministic suggestions.
func (c Catalog) Suggest(query string, compatible map[string]bool, limit int) []Suggestion {
	if limit <= 0 || limit > 3 {
		limit = 3
	}
	matches := c.Search(query, false)
	result := make([]Suggestion, 0, limit)
	for _, match := range matches {
		if compatible != nil && !compatible[match.Entry.Name] {
			continue
		}
		result = append(result, Suggestion{
			SchemaVersion: SuggestionSchema,
			SkillRef:      "$" + match.Entry.Name,
			Name:          match.Entry.Name,
			Maturity:      match.Entry.Maturity,
			Reason:        match.Reason,
			DefaultPrompt: match.Entry.DefaultPrompt,
			Score:         match.Score,
		})
		if len(result) == limit {
			break
		}
	}
	return result
}

func queryTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	terms := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",.;:!?/\\|()[]{}\"'，。；：！？、（）【】", r)
	})
	if len(terms) == 0 {
		return []string{query}
	}
	return terms
}

func scoreCatalogEntry(entry CatalogEntry, terms []string) (int, string) {
	name := strings.ToLower(entry.Name)
	display := strings.ToLower(entry.DisplayName)
	description := strings.ToLower(entry.Description)
	prompt := strings.ToLower(entry.DefaultPrompt)
	keywords := make([]string, len(entry.Keywords))
	for i, keyword := range entry.Keywords {
		keywords[i] = strings.ToLower(keyword)
	}
	total := 0
	reasons := make([]string, 0, len(terms))
	for _, term := range terms {
		best, field := 0, ""
		switch {
		case name == term:
			best, field = 120, "name"
		case strings.Contains(name, term):
			best, field = 80, "name"
		case display == term:
			best, field = 70, "display_name"
		case strings.Contains(display, term):
			best, field = 55, "display_name"
		}
		for _, keyword := range keywords {
			if keyword == term && best < 65 {
				best, field = 65, "keyword"
			} else if strings.Contains(keyword, term) && best < 45 {
				best, field = 45, "keyword"
			}
		}
		if strings.Contains(description, term) && best < 35 {
			best, field = 35, "description"
		}
		if strings.Contains(prompt, term) && best < 20 {
			best, field = 20, "default_prompt"
		}
		if best == 0 {
			return 0, ""
		}
		total += best
		reasons = append(reasons, fmt.Sprintf("%s:%s", field, term))
	}
	return total, strings.Join(reasons, ",")
}
