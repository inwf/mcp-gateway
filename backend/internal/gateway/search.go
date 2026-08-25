package gateway

import (
	"slices"
	"strings"
)

// Scores for where a query term was found. A term in the name says far
// more about relevance than the same term buried in prose, and an exact
// name match is almost always what the caller meant.
const (
	scoreExactName    = 1000
	scoreNamePrefix   = 400
	scoreNameContains = 150
	scoreDescContains = 40
)

// SearchHit is one tool matching a query.
type SearchHit struct {
	Server      string `json:"server"`
	Tool        string `json:"tool"`
	Exposed     string `json:"exposed"`
	Description string `json:"description,omitempty"`
	Score       int    `json:"score"`
}

// Searchable is one candidate offered to [SearchTools].
type Searchable struct {
	Server      string
	Tool        string
	Exposed     string
	Description string
}

// SearchTools ranks candidates against a query.
//
// Every whitespace-separated term must appear somewhere in the tool name
// or its description; terms narrow the result rather than widening it,
// which is what someone typing a second word expects. Matching is
// case-insensitive.
//
// A blank query matches everything, so that a caller can page through
// the full list with the same call.
func SearchTools(query string, candidates []Searchable, limit int) []SearchHit {
	terms := strings.Fields(strings.ToLower(query))

	hits := make([]SearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		score, ok := scoreCandidate(terms, candidate)
		if !ok {
			continue
		}
		hits = append(hits, SearchHit{
			Server:      candidate.Server,
			Tool:        candidate.Tool,
			Exposed:     candidate.Exposed,
			Description: candidate.Description,
			Score:       score,
		})
	}

	// Ties break on name so that repeating a search gives the same
	// order.
	slices.SortFunc(hits, func(a, b SearchHit) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return strings.Compare(a.Exposed, b.Exposed)
	})

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// scoreCandidate returns the total score, and whether every term matched.
func scoreCandidate(terms []string, candidate Searchable) (int, bool) {
	if len(terms) == 0 {
		return 0, true
	}

	name := strings.ToLower(candidate.Tool)
	exposed := strings.ToLower(candidate.Exposed)
	description := strings.ToLower(candidate.Description)

	total := 0
	for _, term := range terms {
		score := bestPlacement(term, name, exposed, description)
		if score == 0 {
			return 0, false
		}
		total += score
	}
	return total, true
}

// bestPlacement scores the most significant place a term appears, or
// zero if it appears nowhere.
func bestPlacement(term, name, exposed, description string) int {
	switch {
	case term == name:
		return scoreExactName
	case strings.HasPrefix(name, term):
		return scoreNamePrefix
	case strings.Contains(name, term):
		return scoreNameContains
	// The exposed name carries the server, so searching by server name
	// finds that server's tools.
	case strings.Contains(exposed, term):
		return scoreNameContains
	case strings.Contains(description, term):
		return scoreDescContains
	default:
		return 0
	}
}
