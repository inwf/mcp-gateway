package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// Cursors store only a position and hashes of the query and its ordered
// matches. They hold no server-side snapshots and do not expire because
// an unrelated upstream changes. Page size and schema mode may change.
type searchCursor struct {
	Version int    `json:"v"`
	Offset  int    `json:"o"`
	Scope   string `json:"q"`
	Matches string `json:"m"`
}

func searchPageCursor(in searchToolsInput, hits []SearchHit, limit int) (int, string, error) {
	if in.Cursor == "" && len(hits) <= limit {
		return 0, "", nil
	}
	if len(in.Cursor) > 512 {
		return 0, "", fmt.Errorf("invalid cursor; restart the search without a cursor")
	}
	scope := cursorHash([]any{in.Server, lowerTerms(in.Query)})
	matches := searchMatchesHash(hits)
	start := 0
	if in.Cursor != "" {
		var cursor searchCursor
		data, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil ||
			cursor.Version != 1 || cursor.Offset <= 0 {
			return 0, "", fmt.Errorf("invalid cursor; restart the search without a cursor")
		}
		if cursor.Scope != scope {
			return 0, "", fmt.Errorf("cursor belongs to another query or server; restart the search without a cursor")
		}
		if cursor.Matches != matches || cursor.Offset >= len(hits) {
			return 0, "", fmt.Errorf("search results changed; restart the search without a cursor")
		}
		start = cursor.Offset
	}
	if start+limit >= len(hits) {
		return start, "", nil
	}
	data, _ := json.Marshal(searchCursor{Version: 1, Offset: start + limit, Scope: scope, Matches: matches})
	return start, base64.RawURLEncoding.EncodeToString(data), nil
}

func cursorHash(value any) string {
	// The scope contains only strings and string slices.
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func searchMatchesHash(hits []SearchHit) string {
	// Hash just the ordered identities. Length prefixes disambiguate names
	// without allocating a second directory or serializing all its metadata.
	size := 0
	for _, hit := range hits {
		size += 16 + len(hit.Server) + len(hit.Tool)
	}
	data := make([]byte, 0, size)
	for _, hit := range hits {
		data = binary.BigEndian.AppendUint64(data, uint64(len(hit.Server)))
		data = append(data, hit.Server...)
		data = binary.BigEndian.AppendUint64(data, uint64(len(hit.Tool)))
		data = append(data, hit.Tool...)
	}
	sum := sha256.Sum256(data)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
