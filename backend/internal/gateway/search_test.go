package gateway_test

import (
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

// A second word is meant to narrow the result, not widen it.
func TestEveryTermMustMatch(t *testing.T) {
	both := gateway.SearchTools("file disk", candidates(), 0)
	if len(both) != 2 {
		t.Errorf("SearchTools(\"file disk\") = %v, want the two file tools", hitNames(both))
	}

	none := gateway.SearchTools("file kubernetes", candidates(), 0)
	if len(none) != 0 {
		t.Errorf("SearchTools(\"file kubernetes\") = %v, want nothing", hitNames(none))
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

func TestScoresDescend(t *testing.T) {
	hits := gateway.SearchTools("read file", candidates(), 0)

	for i := 1; i < len(hits); i++ {
		if hits[i-1].Score < hits[i].Score {
			t.Errorf("hit %d scores %d, below hit %d at %d",
				i-1, hits[i-1].Score, i, hits[i].Score)
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
