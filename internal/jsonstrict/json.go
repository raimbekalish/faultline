// Package jsonstrict rejects ambiguous or excessive JSON before typed decoding.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Duplicate JSON keys are ambiguous even when parsers choose the last value.
// Unknown report fields remain preserved for schema-1 optional-field compatibility.
func Validate(b []byte) error {
	if !utf8.Valid(b) {
		return fmt.Errorf("invalid JSON UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > 32 {
			return fmt.Errorf("JSON nesting exceeds limit")
		}
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return err
					}
					s, ok := key.(string)
					if !ok || seen[strings.ToLower(s)] {
						return fmt.Errorf("duplicate JSON key")
					}
					seen[strings.ToLower(s)] = true
					if err = value(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err = value(depth + 1); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("invalid JSON delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
