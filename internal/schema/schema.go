// Package schema parses schema definition documents and checks whether a new
// version may follow the previous one under a compatibility level.
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Compatibility levels accepted by the registry.
const (
	LevelNone     = "NONE"
	LevelBackward = "BACKWARD"
	LevelForward  = "FORWARD"
	LevelFull     = "FULL"
)

// ErrIncompatible reports that a schema violates the compatibility level
// requested for its subject.
var ErrIncompatible = errors.New("schema is not compatible with the previous version")

// Definition is a parsed schema document: Fields maps field names to type
// strings and Required marks the subset of field names that must be present.
type Definition struct {
	Fields   map[string]string
	Required map[string]bool
}

// ValidLevel reports whether level is one of the accepted compatibility levels.
func ValidLevel(level string) bool {
	switch level {
	case LevelNone, LevelBackward, LevelForward, LevelFull:
		return true
	}
	return false
}

// Parse decodes a schema document. The document must be a JSON object holding
// a fields object that maps field names to type strings and a required array
// naming a subset of those fields.
func Parse(raw string) (*Definition, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		return nil, fmt.Errorf("schema must be a JSON object: %w", err)
	}
	if document == nil {
		return nil, errors.New("schema must be a JSON object")
	}

	fieldsRaw, ok := document["fields"]
	if !ok {
		return nil, errors.New("schema must declare fields")
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(fieldsRaw, &rawFields); err != nil || rawFields == nil {
		return nil, errors.New("fields must map field names to type strings")
	}
	fields := make(map[string]string, len(rawFields))
	for name, rawType := range rawFields {
		var fieldType string
		if err := json.Unmarshal(rawType, &fieldType); err != nil || rawType[0] != '"' {
			return nil, fmt.Errorf("field %q must map to a type string", name)
		}
		fields[name] = fieldType
	}

	requiredRaw, ok := document["required"]
	if !ok {
		return nil, errors.New("schema must declare required")
	}
	var required []string
	if err := json.Unmarshal(requiredRaw, &required); err != nil || required == nil {
		return nil, errors.New("required must be an array of field names")
	}

	definition := &Definition{Fields: fields, Required: make(map[string]bool, len(required))}
	for _, name := range required {
		if _, declared := fields[name]; !declared {
			return nil, fmt.Errorf("required field %q is not declared in fields", name)
		}
		definition.Required[name] = true
	}
	return definition, nil
}

// Compatible reports whether next may follow prev under the given level.
func Compatible(level string, prev, next *Definition) bool {
	switch level {
	case LevelNone:
		return true
	case LevelBackward:
		return backwardCompatible(prev, next)
	case LevelForward:
		return forwardCompatible(prev, next)
	case LevelFull:
		return backwardCompatible(prev, next) && forwardCompatible(prev, next)
	}
	return false
}

// backwardCompatible allows adding optional fields and deleting optional
// fields; it forbids deleting required fields, retyping a kept field, turning
// an optional field required, and adding a required field.
func backwardCompatible(prev, next *Definition) bool {
	for name, prevType := range prev.Fields {
		nextType, kept := next.Fields[name]
		if !kept {
			if prev.Required[name] {
				return false
			}
			continue
		}
		if nextType != prevType {
			return false
		}
		if next.Required[name] && !prev.Required[name] {
			return false
		}
	}
	for name := range next.Fields {
		if _, existed := prev.Fields[name]; !existed && next.Required[name] {
			return false
		}
	}
	return true
}

// forwardCompatible allows adding fields and relaxing required fields to
// optional; it forbids deleting fields, retyping a kept field, and turning an
// optional field required.
func forwardCompatible(prev, next *Definition) bool {
	for name, prevType := range prev.Fields {
		nextType, kept := next.Fields[name]
		if !kept {
			return false
		}
		if nextType != prevType {
			return false
		}
		if next.Required[name] && !prev.Required[name] {
			return false
		}
	}
	return true
}
