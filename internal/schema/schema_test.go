package schema

import "testing"

func mustParse(t *testing.T, raw string) *Definition {
	t.Helper()
	definition, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q): %v", raw, err)
	}
	return definition
}

func TestParseAcceptsWellFormedDocument(t *testing.T) {
	definition := mustParse(t, `{"fields":{"id":"string","age":"int"},"required":["id"]}`)
	if definition.Fields["id"] != "string" || definition.Fields["age"] != "int" {
		t.Fatalf("fields = %v", definition.Fields)
	}
	if !definition.Required["id"] || definition.Required["age"] {
		t.Fatalf("required = %v", definition.Required)
	}
}

func TestParseRejectsMalformedDocuments(t *testing.T) {
	cases := map[string]string{
		"not json":              `{`,
		"json array":            `["fields"]`,
		"json string":           `"fields"`,
		"json null":             `null`,
		"missing fields":        `{"required":[]}`,
		"missing required":      `{"fields":{}}`,
		"fields not object":     `{"fields":[],"required":[]}`,
		"fields null":           `{"fields":null,"required":[]}`,
		"field type not string": `{"fields":{"id":1},"required":[]}`,
		"field type null":       `{"fields":{"id":null},"required":[]}`,
		"required not array":    `{"fields":{},"required":{}}`,
		"required null":         `{"fields":{},"required":null}`,
		"required entry number": `{"fields":{"id":"string"},"required":[1]}`,
		"required not subset":   `{"fields":{"id":"string"},"required":["id","ghost"]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatalf("Parse(%q) succeeded, want error", raw)
			}
		})
	}
}

func TestCompatibleLevels(t *testing.T) {
	prev := mustParse(t, `{"fields":{"id":"string","nick":"string","age":"int"},"required":["id"]}`)

	cases := []struct {
		name     string
		level    string
		next     string
		expected bool
	}{
		{"none skips every check", LevelNone, `{"fields":{},"required":[]}`, true},
		{"backward identical", LevelBackward, `{"fields":{"id":"string","nick":"string","age":"int"},"required":["id"]}`, true},
		{"backward add optional", LevelBackward, `{"fields":{"id":"string","nick":"string","age":"int","email":"string"},"required":["id"]}`, true},
		{"backward delete optional", LevelBackward, `{"fields":{"id":"string","age":"int"},"required":["id"]}`, true},
		{"backward delete required", LevelBackward, `{"fields":{"nick":"string","age":"int"},"required":[]}`, false},
		{"backward retype kept field", LevelBackward, `{"fields":{"id":"string","nick":"string","age":"long"},"required":["id"]}`, false},
		{"backward optional to required", LevelBackward, `{"fields":{"id":"string","nick":"string","age":"int"},"required":["id","nick"]}`, false},
		{"backward add required", LevelBackward, `{"fields":{"id":"string","nick":"string","age":"int","email":"string"},"required":["id","email"]}`, false},
		{"forward add required", LevelForward, `{"fields":{"id":"string","nick":"string","age":"int","email":"string"},"required":["id","email"]}`, true},
		{"forward required to optional", LevelForward, `{"fields":{"id":"string","nick":"string","age":"int"},"required":[]}`, true},
		{"forward delete optional", LevelForward, `{"fields":{"id":"string","age":"int"},"required":["id"]}`, false},
		{"forward retype kept field", LevelForward, `{"fields":{"id":"string","nick":"bytes","age":"int"},"required":["id"]}`, false},
		{"forward optional to required", LevelForward, `{"fields":{"id":"string","nick":"string","age":"int"},"required":["id","age"]}`, false},
		{"full add optional", LevelFull, `{"fields":{"id":"string","nick":"string","age":"int","email":"string"},"required":["id"]}`, true},
		{"full required to optional", LevelFull, `{"fields":{"id":"string","nick":"string","age":"int"},"required":[]}`, true},
		{"full delete optional", LevelFull, `{"fields":{"id":"string","age":"int"},"required":["id"]}`, false},
		{"full add required", LevelFull, `{"fields":{"id":"string","nick":"string","age":"int","email":"string"},"required":["id","email"]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compatible(tc.level, prev, mustParse(t, tc.next)); got != tc.expected {
				t.Fatalf("Compatible(%s) = %v, want %v", tc.level, got, tc.expected)
			}
		})
	}
}
