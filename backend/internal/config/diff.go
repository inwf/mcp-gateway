package config

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

// UnsetValue marks a field that exists on only one side of a comparison.
const UnsetValue = "(unset)"

// Change is one field-level difference between two configurations. Field
// is the same dotted path used by [FieldError], so a change and a
// validation problem name a setting the same way.
//
// The JSON tags matter as much as the fields: this type is handed to the
// web UI, which reads change.field/Settings.tsx to decide whether a save
// needs a restart. The tags make the wire shape the one the frontend
// declares, and keep the field names from being the Go ones.
type Change struct {
	Field string `json:"field"`
	Old   string `json:"old"`
	New   string `json:"new"`
}

func (c Change) String() string {
	return fmt.Sprintf("%s: %s -> %s", c.Field, c.Old, c.New)
}

// Diff reports the field-level differences between two configurations,
// ordered by field path.
//
// Values are compared before they are hidden, then hidden before they
// are returned. That ordering matters: hiding first would make both
// sides of a rotated credential read as the same placeholder, and the
// rotation — exactly the kind of event an audit trail exists for —
// would vanish. So a changed secret is reported as a change whose values
// both read as redacted.
func Diff(before, after Config) []Change {
	oldTree, err := toTree(before)
	if err != nil {
		return []Change{{Field: "", Old: "", New: fmt.Sprintf("cannot compare: %v", err)}}
	}
	newTree, err := toTree(after)
	if err != nil {
		return []Change{{Field: "", Old: "", New: fmt.Sprintf("cannot compare: %v", err)}}
	}

	var changes []Change
	diffTrees("", oldTree, newTree, &changes)

	for i, c := range changes {
		changes[i] = redactChange(c)
	}

	sort.Slice(changes, func(i, j int) bool { return changes[i].Field < changes[j].Field })
	return changes
}

// redactChange hides the values of a change without hiding that it
// happened.
func redactChange(c Change) Change {
	key := c.Field
	if i := strings.LastIndex(key, "."); i >= 0 {
		key = key[i+1:]
	}

	switch {
	case IsSecretKey(key):
		c.Old, c.New = hideValue(c.Old), hideValue(c.New)
	case key == "url" || key == "proxy":
		// The address is useful in a log; only the credentials in it are
		// not.
		c.Old, c.New = redactURLCredentials(c.Old), redactURLCredentials(c.New)
	}
	return c
}

// hideValue replaces a value but leaves the marker that says a field was
// absent, so an added or removed secret still reads as added or removed.
func hideValue(v string) string {
	if v == UnsetValue {
		return v
	}
	return RedactedValue
}

// toTree renders a configuration as nested generic values, using the
// same names the YAML file uses so that reported paths match what a user
// would edit.
func toTree(cfg Config) (map[string]any, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var tree map[string]any
	if err := yaml.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	return tree, nil
}

func diffTrees(prefix string, before, after map[string]any, changes *[]Change) {
	keys := slices.Sorted(maps.Keys(unionKeys(before, after)))

	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		oldVal, oldOK := before[key]
		newVal, newOK := after[key]

		oldMap, oldIsMap := asMap(oldVal)
		newMap, newIsMap := asMap(newVal)

		switch {
		case oldIsMap && newIsMap:
			diffTrees(path, oldMap, newMap, changes)

		// A whole section appearing or disappearing is reported as a
		// change on each of its fields, which reads better in a log than
		// one opaque entry for the section.
		case oldIsMap && !newOK:
			diffTrees(path, oldMap, map[string]any{}, changes)
		case newIsMap && !oldOK:
			diffTrees(path, map[string]any{}, newMap, changes)

		default:
			oldText, newText := render(oldVal, oldOK), render(newVal, newOK)
			if oldText != newText {
				*changes = append(*changes, Change{Field: path, Old: oldText, New: newText})
			}
		}
	}
}

func unionKeys(a, b map[string]any) map[string]struct{} {
	keys := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		keys[k] = struct{}{}
	}
	for k := range b {
		keys[k] = struct{}{}
	}
	return keys
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

// render turns a value into the text shown in a change entry. Lists are
// rendered whole rather than element by element: an audit log is easier
// to read with one line per setting than one line per list index.
func render(v any, present bool) string {
	if !present || v == nil {
		return UnsetValue
	}
	return fmt.Sprintf("%v", v)
}
