package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Everything the gateway publishes has to be parseable by the clients
// that will actually connect to it, and those clients are stricter than
// the JSON Schema specification is.
//
// The case that prompted this file: a Go field typed `any` makes the SDK
// generate the schema `true` for it. That is valid JSON Schema — it is
// the schema that accepts anything — but the TypeScript MCP SDK, which
// Claude Code and most other clients are built on, validates each entry
// under "properties" as an object and rejects a boolean. It rejects the
// entire tools/list response on that one field, so every tool the
// gateway offers becomes invisible rather than just the offending one.
//
// A unit test cannot run the TypeScript validator, so it checks the
// property that validator enforces.

func TestPublishedSchemasHaveNoBooleanProperties(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) == 0 {
		t.Fatal("no tools were listed, so this proves nothing")
	}

	for _, tool := range listed.Tools {
		for label, schema := range map[string]any{
			"inputSchema":  tool.InputSchema,
			"outputSchema": tool.OutputSchema,
		} {
			if schema == nil {
				continue
			}
			for _, where := range booleanProperties(t, schema, tool.Name+"."+label) {
				t.Errorf("%s is a boolean; the TypeScript SDK rejects the whole "+
					"tools/list response over it", where)
			}
		}
	}
}

// booleanProperties returns the paths at which a boolean stands where a
// property's schema belongs.
//
// Only "properties" is inspected. A boolean is idiomatic and widely
// accepted elsewhere in a schema — "additionalProperties": false is the
// ordinary way to close an object — so flagging every boolean would
// report the healthy majority of the gateway's schemas.
func booleanProperties(t *testing.T, schema any, path string) []string {
	t.Helper()

	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("%s: cannot marshal: %v", path, err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("%s: cannot decode: %v", path, err)
	}

	var found []string
	var walk func(node any, path string)
	walk = func(node any, path string) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		if properties, ok := object["properties"].(map[string]any); ok {
			for name, value := range properties {
				at := fmt.Sprintf("%s.properties.%s", path, name)
				if _, isBool := value.(bool); isBool {
					found = append(found, at)
					continue
				}
				walk(value, at)
			}
		}
		// A nested schema can hide anywhere a schema is allowed, and the
		// ones the gateway actually generates are array items and the
		// definitions the SDK hoists out of reused Go types.
		walk(object["items"], path+".items")
		for _, key := range []string{"$defs", "definitions"} {
			defs, ok := object[key].(map[string]any)
			if !ok {
				continue
			}
			for name, value := range defs {
				walk(value, fmt.Sprintf("%s.%s.%s", path, key, name))
			}
		}
	}
	walk(decoded, path)
	return found
}
