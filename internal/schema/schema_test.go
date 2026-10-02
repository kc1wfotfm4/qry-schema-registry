package schema

import "testing"

func TestParseAcceptsWellFormedSchema(t *testing.T) {
	parsed, err := Parse(`{"fields":{"id":"string","age":"int"},"required":["id"]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := parsed.Fields["id"]; got != "string" {
		t.Fatalf("fields[id] = %q, want %q", got, "string")
	}
	if !parsed.Required["id"] || parsed.Required["age"] {
		t.Fatalf("required = %v, want only id", parsed.Required)
	}
}

func TestParseRejectsMalformedShapes(t *testing.T) {
	cases := map[string]string{
		"not json":            `{"fields":`,
		"json array":          `["fields"]`,
		"json null":           `null`,
		"missing fields":      `{"required":[]}`,
		"missing required":    `{"fields":{"id":"string"}}`,
		"fields not object":   `{"fields":["id"],"required":[]}`,
		"fields null":         `{"fields":null,"required":[]}`,
		"field type not text": `{"fields":{"id":1},"required":[]}`,
		"required not array":  `{"fields":{"id":"string"},"required":"id"}`,
		"required null":       `{"fields":{"id":"string"},"required":null}`,
		"required not text":   `{"fields":{"id":"string"},"required":[1]}`,
		"required unknown":    `{"fields":{"id":"string"},"required":["missing"]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatalf("Parse(%s) succeeded, want error", raw)
			}
		})
	}
}

func mustParse(t *testing.T, raw string) *Schema {
	t.Helper()
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	return parsed
}

func TestCheckCompatibilityMatrix(t *testing.T) {
	base := `{"fields":{"id":"string","note":"string","age":"int"},"required":["id","note"]}`
	cases := []struct {
		name  string
		level string
		next  string
		ok    bool
	}{
		// NONE checks nothing.
		{"none allows anything", LevelNone, `{"fields":{},"required":[]}`, true},
		// BACKWARD.
		{"backward add optional", LevelBackward, `{"fields":{"id":"string","note":"string","age":"int","email":"string"},"required":["id","note"]}`, true},
		{"backward add required", LevelBackward, `{"fields":{"id":"string","note":"string","age":"int","email":"string"},"required":["id","note","email"]}`, false},
		{"backward drop optional", LevelBackward, `{"fields":{"id":"string","note":"string"},"required":["id","note"]}`, true},
		{"backward drop required", LevelBackward, `{"fields":{"id":"string","age":"int"},"required":["id"]}`, false},
		{"backward change type", LevelBackward, `{"fields":{"id":"string","note":"string","age":"string"},"required":["id","note"]}`, false},
		{"backward optional to required", LevelBackward, `{"fields":{"id":"string","note":"string","age":"int"},"required":["id","note","age"]}`, false},
		{"backward required to optional", LevelBackward, `{"fields":{"id":"string","note":"string","age":"int"},"required":["id"]}`, true},
		// FORWARD.
		{"forward add optional", LevelForward, `{"fields":{"id":"string","note":"string","age":"int","email":"string"},"required":["id","note"]}`, true},
		{"forward add required", LevelForward, `{"fields":{"id":"string","note":"string","age":"int","email":"string"},"required":["id","note","email"]}`, true},
		{"forward drop optional", LevelForward, `{"fields":{"id":"string","note":"string"},"required":["id","note"]}`, false},
		{"forward drop required", LevelForward, `{"fields":{"id":"string","age":"int"},"required":["id"]}`, false},
		{"forward change type", LevelForward, `{"fields":{"id":"string","note":"string","age":"string"},"required":["id","note"]}`, false},
		{"forward optional to required", LevelForward, `{"fields":{"id":"string","note":"string","age":"int"},"required":["id","note","age"]}`, false},
		{"forward required to optional", LevelForward, `{"fields":{"id":"string","note":"string","age":"int"},"required":["id"]}`, true},
		// FULL satisfies both.
		{"full add optional", LevelFull, `{"fields":{"id":"string","note":"string","age":"int","email":"string"},"required":["id","note"]}`, true},
		{"full add required", LevelFull, `{"fields":{"id":"string","note":"string","age":"int","email":"string"},"required":["id","note","email"]}`, false},
		{"full drop optional", LevelFull, `{"fields":{"id":"string","note":"string"},"required":["id","note"]}`, false},
		{"full change type", LevelFull, `{"fields":{"id":"string","note":"string","age":"string"},"required":["id","note"]}`, false},
		{"full required to optional", LevelFull, `{"fields":{"id":"string","note":"string","age":"int"},"required":["id"]}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prev := mustParse(t, base)
			next := mustParse(t, tc.next)
			err := Check(tc.level, prev, next)
			if tc.ok && err != nil {
				t.Fatalf("Check(%s) = %v, want nil", tc.level, err)
			}
			if !tc.ok {
				violation, ok := err.(*Violation)
				if !ok {
					t.Fatalf("Check(%s) = %v, want *Violation", tc.level, err)
				}
				if violation.Reason == "" {
					t.Fatalf("violation reason is empty")
				}
			}
		})
	}
}
