// Package schema parses structure definitions and judges whether a new definition may follow
// the previous one of the same subject under a compatibility level.
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Compatibility levels accepted by the registry.
const (
	LevelNone     = "NONE"
	LevelBackward = "BACKWARD"
	LevelForward  = "FORWARD"
	LevelFull     = "FULL"
)

// ValidLevel reports whether level is one of the accepted compatibility levels.
func ValidLevel(level string) bool {
	switch level {
	case LevelNone, LevelBackward, LevelForward, LevelFull:
		return true
	}
	return false
}

// Schema is the parsed shape of a structure definition: Fields maps a field name to its type
// string and Required marks the subset of those names that data must carry.
type Schema struct {
	Fields   map[string]string
	Required map[string]bool
}

// Parse decodes raw into a Schema. raw must be a JSON object holding "fields", an object
// mapping field names to type strings, and "required", an array naming a subset of those
// fields.
func Parse(raw string) (*Schema, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || doc == nil {
		return nil, errors.New("schema must be a JSON object")
	}
	fieldsRaw, ok := doc["fields"]
	if !ok {
		return nil, errors.New(`schema must contain "fields"`)
	}
	var fields map[string]string
	if err := json.Unmarshal(fieldsRaw, &fields); err != nil || fields == nil {
		return nil, errors.New(`"fields" must map field names to type strings`)
	}
	requiredRaw, ok := doc["required"]
	if !ok {
		return nil, errors.New(`schema must contain "required"`)
	}
	var requiredList []string
	if err := json.Unmarshal(requiredRaw, &requiredList); err != nil || requiredList == nil {
		return nil, errors.New(`"required" must be an array of field names`)
	}
	required := make(map[string]bool, len(requiredList))
	for _, name := range requiredList {
		if _, ok := fields[name]; !ok {
			return nil, fmt.Errorf("required field %q is not declared in fields", name)
		}
		required[name] = true
	}
	return &Schema{Fields: fields, Required: required}, nil
}

// Violation describes why a new schema may not follow the previous one under a compatibility
// level.
type Violation struct {
	Reason string
}

func (v *Violation) Error() string { return v.Reason }

// Check judges whether next may follow prev under level. NONE performs no checks; BACKWARD
// and FORWARD apply their own rules; FULL must satisfy both.
func Check(level string, prev, next *Schema) error {
	switch level {
	case LevelBackward:
		return checkBackward(prev, next)
	case LevelForward:
		return checkForward(prev, next)
	case LevelFull:
		if err := checkBackward(prev, next); err != nil {
			return err
		}
		return checkForward(prev, next)
	default:
		return nil
	}
}

// checkBackward allows adding optional fields and removing optional fields; it forbids
// removing required fields, changing the type of a shared field, and turning an optional
// field into a required one.
func checkBackward(prev, next *Schema) error {
	for _, name := range sortedKeys(prev.Fields) {
		nextType, kept := next.Fields[name]
		if !kept {
			if prev.Required[name] {
				return &Violation{Reason: fmt.Sprintf("required field %q is removed", name)}
			}
			continue
		}
		if prevType := prev.Fields[name]; prevType != nextType {
			return &Violation{Reason: fmt.Sprintf("field %q changes type from %q to %q", name, prevType, nextType)}
		}
		if !prev.Required[name] && next.Required[name] {
			return &Violation{Reason: fmt.Sprintf("field %q changes from optional to required", name)}
		}
	}
	for _, name := range sortedKeys(next.Fields) {
		if _, ok := prev.Fields[name]; !ok && next.Required[name] {
			return &Violation{Reason: fmt.Sprintf("new field %q must be optional", name)}
		}
	}
	return nil
}

// checkForward allows adding fields and relaxing a required field to optional; it forbids
// removing fields, changing the type of a shared field, and turning an optional field into
// a required one.
func checkForward(prev, next *Schema) error {
	for _, name := range sortedKeys(prev.Fields) {
		nextType, kept := next.Fields[name]
		if !kept {
			return &Violation{Reason: fmt.Sprintf("field %q is removed", name)}
		}
		if prevType := prev.Fields[name]; prevType != nextType {
			return &Violation{Reason: fmt.Sprintf("field %q changes type from %q to %q", name, prevType, nextType)}
		}
		if !prev.Required[name] && next.Required[name] {
			return &Violation{Reason: fmt.Sprintf("field %q changes from optional to required", name)}
		}
	}
	return nil
}

func sortedKeys(fields map[string]string) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
