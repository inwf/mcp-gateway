package gateway_test

import (
	"slices"
	"testing"

	"mcphub/internal/gateway"
)

func candidates() []gateway.Searchable {
	return []gateway.Searchable{
		{Server: "files", Tool: "read", Exposed: "files_read",
			Description: "read the contents of a file from disk"},
		{Server: "files", Tool: "write", Exposed: "files_write",
			Description: "write contents to a file on disk"},
		{Server: "web", Tool: "search", Exposed: "web_search",
			Description: "search the web for a query"},
		{Server: "web", Tool: "fetch", Exposed: "web_fetch",
			Description: "fetch a page and return its text"},
		{Server: "db", Tool: "query", Exposed: "db_query",
			Description: "run a read-only SQL query"},
	}
}

func hitNames(hits []gateway.SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Exposed
	}
	return out
}

func TestSearchFindsByToolName(t *testing.T) {
	hits := gateway.SearchTools("read", candidates(), 0)

	if len(hits) == 0 {
		t.Fatal("no hits for a query that is an exact tool name")
	}
	if hits[0].Exposed != "files_read" {
		t.Errorf("best hit = %q, want files_read; got %v", hits[0].Exposed, hitNames(hits))
	}
}

// An exact name match is almost always what the caller meant, so it has
// to outrank a tool that merely mentions the word.
func TestExactNameOutranksADescriptionMention(t *testing.T) {
	hits := gateway.SearchTools("search", candidates(), 0)

	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Exposed != "web_search" {
		t.Errorf("best hit = %q, want web_search; got %v", hits[0].Exposed, hitNames(hits))
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	for _, query := range []string{"READ", "Read", "rEaD"} {
		hits := gateway.SearchTools(query, candidates(), 0)
		if len(hits) == 0 || hits[0].Exposed != "files_read" {
			t.Errorf("SearchTools(%q) = %v, want files_read first", query, hitNames(hits))
		}
	}
}

func TestSearchMatchesDescriptions(t *testing.T) {
	hits := gateway.SearchTools("sql", candidates(), 0)

	if len(hits) != 1 || hits[0].Exposed != "db_query" {
		t.Errorf("SearchTools(\"sql\") = %v, want only db_query", hitNames(hits))
	}
}

// Several words describing one thing must not behave like a filter.
//
// The query that prompted this was "horoscope zodiac astrology
// constellation" against a server whose tool is called
// get_daily_horoscope: four words for one idea, one of which matched
// exactly. Requiring all four answered "there is no such tool", and the
// caller believed it.
func TestMoreTermsMatchedRanksHigher(t *testing.T) {
	hits := gateway.SearchTools("file disk", candidates(), 0)

	if len(hits) < 2 {
		t.Fatalf("SearchTools(\"file disk\") = %v, want at least the two file tools", hitNames(hits))
	}
	if hits[0].Matched != 2 {
		t.Errorf("top hit %q matched %d terms, want both", hits[0].Exposed, hits[0].Matched)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i-1].Matched < hits[i].Matched {
			t.Errorf("hit %d matched %d terms, above hit %d which matched %d",
				i-1, hits[i-1].Matched, i, hits[i].Matched)
		}
	}
}

func TestATermThatMatchesNothingDoesNotEraseTheOthers(t *testing.T) {
	hits := gateway.SearchTools("file kubernetes", candidates(), 0)

	if len(hits) == 0 {
		t.Fatal("one word matching nothing threw away the word that matched")
	}
	for _, hit := range hits {
		if hit.Matched != 1 {
			t.Errorf("hit %q matched %d terms, want just the one", hit.Exposed, hit.Matched)
		}
	}
}

// The other half of the answer: which words found nothing. Without it, a
// thin result cannot be told apart from a gateway that lacks the
// capability — and the caller has no way to guess which word to drop.
func TestTermsThatMatchedNothingAreNamed(t *testing.T) {
	got := gateway.UnmatchedTerms("file Kubernetes helm", candidates())

	// Reported as the caller wrote them, capital and all: it has to
	// recognise its own word to drop it.
	want := []string{"Kubernetes", "helm"}
	if !slices.Equal(got, want) {
		t.Errorf("UnmatchedTerms = %v, want %v", got, want)
	}

	if got := gateway.UnmatchedTerms("file disk", candidates()); len(got) != 0 {
		t.Errorf("UnmatchedTerms = %v, want nothing when every word landed", got)
	}
	if got := gateway.UnmatchedTerms("", candidates()); len(got) != 0 {
		t.Errorf("UnmatchedTerms of a blank query = %v, want nothing", got)
	}
}

// The exposed name carries the server, which makes "show me everything
// on this server" work without a separate filter.
func TestSearchByServerName(t *testing.T) {
	hits := gateway.SearchTools("web", candidates(), 0)

	if len(hits) != 2 {
		t.Fatalf("SearchTools(\"web\") = %v, want both web tools", hitNames(hits))
	}
	for _, hit := range hits {
		if hit.Server != "web" {
			t.Errorf("hit %q comes from server %q, want web", hit.Exposed, hit.Server)
		}
	}
}

func TestBlankQueryMatchesEverything(t *testing.T) {
	for _, query := range []string{"", "   ", "\t\n"} {
		if got := gateway.SearchTools(query, candidates(), 0); len(got) != len(candidates()) {
			t.Errorf("SearchTools(%q) returned %d hits, want all %d",
				query, len(got), len(candidates()))
		}
	}
}

func TestSearchLimit(t *testing.T) {
	hits := gateway.SearchTools("", candidates(), 2)

	if len(hits) != 2 {
		t.Errorf("returned %d hits, want 2", len(hits))
	}
}

func TestLimitKeepsTheBestHits(t *testing.T) {
	hits := gateway.SearchTools("file", candidates(), 1)

	if len(hits) != 1 {
		t.Fatalf("returned %d hits, want 1", len(hits))
	}
	// files_read and files_write both match; the winner must be the one
	// that scored highest, not whichever came first.
	unlimited := gateway.SearchTools("file", candidates(), 0)
	if hits[0].Exposed != unlimited[0].Exposed {
		t.Errorf("limited search returned %q, want the top hit %q",
			hits[0].Exposed, unlimited[0].Exposed)
	}
}

// Repeating a search must give the same order, or a paged UI jumps
// around.
func TestSearchOrderIsStable(t *testing.T) {
	first := hitNames(gateway.SearchTools("file", candidates(), 0))

	for i := 0; i < 25; i++ {
		again := hitNames(gateway.SearchTools("file", candidates(), 0))
		if len(again) != len(first) {
			t.Fatalf("run %d returned %d hits, first run returned %d", i, len(again), len(first))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("run %d returned %v, first run returned %v", i, again, first)
			}
		}
	}
}

// Relevance is the pair, in that order: how much of the query a tool
// answered, then where it answered it. A tool matching two words from
// their descriptions belongs above one matching a single word exactly,
// which is why score alone is not the contract.
func TestHitsAreOrderedByRelevance(t *testing.T) {
	hits := gateway.SearchTools("read file", candidates(), 0)

	for i := 1; i < len(hits); i++ {
		before, after := hits[i-1], hits[i]
		if before.Matched < after.Matched {
			t.Errorf("hit %d matched %d terms, above hit %d which matched %d",
				i-1, before.Matched, i, after.Matched)
		}
		if before.Matched == after.Matched && before.Score < after.Score {
			t.Errorf("hit %d scores %d, above hit %d at %d on the same term count",
				i-1, before.Score, i, after.Score)
		}
	}
}

func TestSearchNoCandidates(t *testing.T) {
	if got := gateway.SearchTools("read", nil, 0); len(got) != 0 {
		t.Errorf("SearchTools with no candidates returned %v", hitNames(got))
	}
}

func TestSearchCarriesTheOrigin(t *testing.T) {
	hits := gateway.SearchTools("read", candidates(), 1)

	if len(hits) != 1 {
		t.Fatalf("returned %d hits, want 1", len(hits))
	}
	// A caller acts on the result by calling the tool, which needs both
	// halves of the origin.
	if hits[0].Server != "files" || hits[0].Tool != "read" {
		t.Errorf("hit = %+v, want server files and tool read", hits[0])
	}
}
