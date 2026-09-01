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

	// Matched is how many of the query's terms this tool matched. It is
	// reported because it is the first thing the ranking goes on, so a
	// caller can see why one hit came above another — and can tell a tool
	// that answered the whole question from one that answered a word of it.
	Matched int `json:"matched"`

	Score int `json:"score"`
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
// A tool matching more of the query's terms outranks one matching fewer,
// and within an equal count the placement of those terms decides. A term
// that matches nothing does not erase the terms that did: someone
// describing what they want in several words ("horoscope zodiac astrology")
// is naming one thing several ways, not narrowing a filter, and requiring
// all of them turns a good query into no answer at all. Which terms found
// nothing is worth knowing separately — see [UnmatchedTerms].
//
// Matching is case-insensitive. A blank query matches everything, so that
// a caller can page through the full list with the same call.
func SearchTools(query string, candidates []Searchable, limit int) []SearchHit {
	terms := lowerTerms(query)

	hits := make([]SearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		matched, score := scoreCandidate(terms, candidate)
		// A query with no terms asks for everything; one with terms asks
		// for the tools that answered at least one of them.
		if len(terms) > 0 && matched == 0 {
			continue
		}
		hits = append(hits, SearchHit{
			Server:      candidate.Server,
			Tool:        candidate.Tool,
			Exposed:     candidate.Exposed,
			Description: candidate.Description,
			Matched:     matched,
			Score:       score,
		})
	}

	// Ties break on the origin rather than on the exposed name, which is
	// empty for every tool an installation has not exposed — and that is
	// most of them. Sorting on a field that is usually blank leaves the
	// order to chance.
	slices.SortFunc(hits, func(a, b SearchHit) int {
		if a.Matched != b.Matched {
			return b.Matched - a.Matched
		}
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		if origin := strings.Compare(a.Server, b.Server); origin != 0 {
			return origin
		}
		return strings.Compare(a.Tool, b.Tool)
	})

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// UnmatchedTerms returns the query terms that appear in no candidate at
// all, spelled as the caller wrote them.
//
// It exists so that an empty or thin result can say why. "No hits" reads
// as "this gateway cannot do that", which is a conclusion a caller should
// not reach because it used a word this installation does not use. Naming
// the words that found nothing turns a dead end into a next attempt.
func UnmatchedTerms(query string, candidates []Searchable) []string {
	words := strings.Fields(query)
	terms := lowerTerms(query)

	unmatched := make([]string, 0)
	for i, term := range terms {
		if !slices.ContainsFunc(candidates, func(candidate Searchable) bool {
			return bestPlacement(term, placesIn(candidate)) > 0
		}) {
			unmatched = append(unmatched, words[i])
		}
	}
	return unmatched
}

func lowerTerms(query string) []string {
	words := strings.Fields(query)
	terms := make([]string, len(words))
	for i, word := range words {
		terms[i] = strings.ToLower(word)
	}
	return terms
}

// places are the three fields of a candidate a term can be found in,
// lowered once so that a query of several terms does not lower them again
// for each one.
type places struct {
	name        string
	exposed     string
	description string
}

func placesIn(candidate Searchable) places {
	return places{
		name:        strings.ToLower(candidate.Tool),
		exposed:     strings.ToLower(candidate.Exposed),
		description: strings.ToLower(candidate.Description),
	}
}

// scoreCandidate returns how many terms matched and their total score.
func scoreCandidate(terms []string, candidate Searchable) (matched, score int) {
	where := placesIn(candidate)
	for _, term := range terms {
		if placement := bestPlacement(term, where); placement > 0 {
			matched++
			score += placement
		}
	}
	return matched, score
}

// bestPlacement scores the most significant place a term appears, or
// zero if it appears nowhere.
func bestPlacement(term string, where places) int {
	switch {
	case term == where.name:
		return scoreExactName
	case strings.HasPrefix(where.name, term):
		return scoreNamePrefix
	case strings.Contains(where.name, term):
		return scoreNameContains
	// The exposed name carries the server, so searching by server name
	// finds that server's tools.
	case strings.Contains(where.exposed, term):
		return scoreNameContains
	case strings.Contains(where.description, term):
		return scoreDescContains
	default:
		return 0
	}
}
