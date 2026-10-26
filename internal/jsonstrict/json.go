// Package jsonstrict rejects ambiguous or excessive JSON before typed decoding.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Duplicate JSON keys are ambiguous even when parsers choose the last value.
// Unknown report fields remain preserved for schema-1 optional-field compatibility.
func Validate(b []byte) error {
	if !utf8.Valid(b) {
		return fmt.Errorf("invalid JSON UTF-8")
	}
	if err := validSurrogates(b); err != nil {
		return err
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
					if !ok || seen[foldKey(s)] {
						return fmt.Errorf("duplicate JSON key")
					}
					seen[foldKey(s)] = true
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

// Match encoding/json's Unicode simple-fold equivalence, including long s.
func foldKey(s string) string {
	return strings.Map(func(r rune) rune {
		smallest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < smallest {
				smallest = next
			}
		}
		return smallest
	}, s)
}

// encoding/json replaces unpaired escaped UTF-16 surrogates with U+FFFD. Reject
// that lossy interpretation at the evidence boundary. Syntax remains decoder-owned.
func validSurrogates(b []byte) error {
	inString := false
	for i := 0; i < len(b); i++ {
		if b[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || b[i] != '\\' {
			continue
		}
		i++
		if i >= len(b) {
			break
		}
		if b[i] != 'u' || i+4 >= len(b) {
			continue
		}
		n, err := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
		if err != nil {
			continue
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return fmt.Errorf("unpaired JSON surrogate")
		}
		if n < 0xd800 || n > 0xdbff {
			continue
		}
		if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
			return fmt.Errorf("unpaired JSON surrogate")
		}
		low, err := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
		if err != nil || low < 0xdc00 || low > 0xdfff {
			return fmt.Errorf("unpaired JSON surrogate")
		}
		i += 6
	}
	return nil
}
