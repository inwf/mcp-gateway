package config_test

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"mcphub/internal/config"
)

// The configuration reference was the one part of this project's account of
// itself that nothing was checking.
//
// internal/guide has four tests holding its document against the program —
// the commands it names, the flags it names, the system tools it explains,
// the constants it quotes — and they caught real drift three times during
// development. docs/configuration.md had none of that, and by the time
// these tests were written it was wrong in two ways at once: four fields
// added later were missing from it, and one field's description said the
// opposite of what the code does.
//
// So these check the document against the struct rather than against an
// expected text, for the same reason the guide tests do: an expected text
// only asserts that the file has not been edited, and has to be updated
// every time it is.
//
// What they cannot check is prose. A field name is either in the document
// or it is not; whether the sentence beside it is true is not something
// reflect can see. That boundary is real — the sentence that was wrong
// here needed an assertion of its own.

// The document lives in the repository, beside the other things a reader
// browses, rather than inside this module. A //go:embed directive cannot
// reach outside its own module directory; a test reading a file has no
// such limit.
const referencePath = "../../../docs/configuration.md"

// Every field someone can write in the file has to appear in the document,
// or it is a setting they can only find by reading the source.
func TestTheConfigurationReferenceDocumentsEveryField(t *testing.T) {
	doc := readReference(t)

	for _, name := range sortedNames(yamlFieldNames(t)) {
		// Anywhere in the document, not specifically in a table: the two
		// lists under sessionModeRules are explained by a worked example,
		// which is a better way to document them than a row each.
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("the configuration reference never mentions %q; someone "+
				"configuring mcphub from that document cannot know the field exists", name)
		}
	}
}

// And every field the document describes has to exist, or it is telling
// someone to write a key that will now be rejected — config.Parse refuses
// unknown keys.
func TestTheConfigurationReferenceOnlyDocumentsRealFields(t *testing.T) {
	fields := yamlFieldNames(t)
	documented := documentedFields(t)

	if len(documented) == 0 {
		t.Fatal("no field rows were found in the configuration reference, " +
			"so this test proves nothing")
	}
	for _, name := range documented {
		if !fields[name] {
			t.Errorf("the configuration reference documents %q, which is not a field; "+
				"the fields are: %s", name, strings.Join(sortedNames(fields), ", "))
		}
	}
}

// yamlFieldNames walks Config for every name that can appear as a key in
// the file, including the keys of a server entry.
//
// Reflected rather than listed here on purpose: a list would be a second
// copy of the schema, and would drift from the first — which is the very
// thing these tests exist to catch.
func yamlFieldNames(t *testing.T) map[string]bool {
	t.Helper()

	names := map[string]bool{}
	seen := map[reflect.Type]bool{}

	var walk func(reflect.Type)
	walk = func(typ reflect.Type) {
		// Through pointers and containers to whatever holds the fields:
		// mcpServers is a map whose values carry a server's own keys.
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice ||
			typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true

		for i := range typ.NumField() {
			field := typ.Field(i)
			tag := field.Tag.Get("yaml")
			if tag == "" || tag == "-" {
				continue
			}
			if name := strings.Split(tag, ",")[0]; name != "" {
				names[name] = true
			}
			walk(field.Type)
		}
	}
	walk(reflect.TypeOf(config.Config{}))

	if len(names) == 0 {
		t.Fatal("no yaml fields were found on config.Config, so this test proves nothing")
	}
	return names
}

// fieldRow matches the first cell of a field table: `name` and nothing else.
var fieldRow = regexp.MustCompile("^\\| `([A-Za-z][A-Za-z0-9]*)`")

// sectionHeading matches the heading of a section that describes a
// configuration section, which is written as the key itself.
var sectionHeading = regexp.MustCompile("^## `[A-Za-z]")

// documentedFields collects the field names the document puts in a table.
//
// Only tables under a section named after a configuration key count. The
// prose sections carry tables of their own — the data directory's, for one
// — and their first column is not a field name.
func documentedFields(t *testing.T) []string {
	t.Helper()

	var out []string
	inSection := false

	for _, line := range strings.Split(readReference(t), "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = sectionHeading.MatchString(line)
			continue
		}
		if !inSection {
			continue
		}
		if match := fieldRow.FindStringSubmatch(line); match != nil {
			out = append(out, match[1])
		}
	}
	return out
}

func readReference(t *testing.T) string {
	t.Helper()

	// Fatal rather than skip when it cannot be read. A skip would leave
	// this checking nothing on the day the document is moved or renamed,
	// which is exactly when it is needed.
	text, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("read the configuration reference: %v", err)
	}
	return string(text)
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
