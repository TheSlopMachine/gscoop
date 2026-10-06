package manifest

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Marshal renders obj with ConvertToPrettyJson shape: 4-space indent,
// CRLF line endings, ": " after colons, and single-element arrays
// collapsed to scalars (lib/json.ps1 normalize_values). Key order
// follows the decoded document.
func Marshal(obj *Object) []byte {
	normalized := NormalizeObject(obj)
	var out strings.Builder
	writeValue(&out, normalized, 0)
	return []byte(out.String())
}

// MarshalManifest renders m.Root.
func MarshalManifest(m *Manifest) []byte {
	return Marshal(m.Root)
}

// NormalizeObject returns a copy of obj with normalize_values applied:
// multiline strings split to arrays, single-element arrays collapse to
// scalars, and single-element nested arrays unwrap. Nested objects
// recurse (lib/json.ps1:161-213).
func NormalizeObject(obj *Object) *Object {
	if obj == nil {
		return &Object{Values: make(map[string]any)}
	}
	clone := &Object{Keys: append([]string(nil), obj.Keys...), Values: make(map[string]any, len(obj.Values))}
	for _, key := range obj.Keys {
		clone.Values[key] = normalizeValue(obj.Values[key])
	}
	return clone
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case *Object:
		return NormalizeObject(typed)
	case string:
		return normalizeString(typed)
	case []any:
		return normalizeArray(typed)
	default:
		return value
	}
}

var lineSplitter = regexp.MustCompile(`\r?\n`)

func normalizeString(value string) any {
	parts := lineSplitter.Split(value, -1)
	trimmed := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed = append(trimmed, strings.TrimSpace(part))
	}
	if len(trimmed) > 1 {
		items := make([]any, 0, len(trimmed))
		for _, part := range trimmed {
			items = append(items, part)
		}
		return items
	}
	return value
}

func normalizeArray(items []any) any {
	if len(items) == 1 {
		if _, nested := items[0].([]any); nested {
			return items
		}
		return normalizeValue(items[0])
	}
	out := make([]any, 0, len(items))
	for _, item := range items {
		if nested, ok := item.([]any); ok {
			if len(nested) == 1 {
				out = append(out, normalizeValue(nested[0]))
			} else {
				out = append(out, nested)
			}
			continue
		}
		out = append(out, normalizeValue(item))
	}
	return out
}

const (
	prettyIndent = "    "
	prettyEOL    = "\r\n"
)

func writeValue(out *strings.Builder, value any, depth int) {
	switch typed := value.(type) {
	case *Object:
		writeObject(out, typed, depth)
	case []any:
		writeArray(out, typed, depth)
	default:
		writeScalar(out, typed)
	}
}

func writeObject(out *strings.Builder, obj *Object, depth int) {
	out.WriteString("{")
	if len(obj.Keys) == 0 {
		out.WriteString(prettyEOL + strings.Repeat(prettyIndent, depth+1) + prettyEOL + strings.Repeat(prettyIndent, depth) + "}")
		return
	}
	out.WriteString(prettyEOL + strings.Repeat(prettyIndent, depth+1))
	for i, key := range obj.Keys {
		if i > 0 {
			out.WriteString("," + prettyEOL + strings.Repeat(prettyIndent, depth+1))
		}
		writeJSONString(out, key)
		out.WriteString(": ")
		writeValue(out, obj.Values[key], depth+1)
	}
	out.WriteString(prettyEOL + strings.Repeat(prettyIndent, depth) + "}")
}

func writeArray(out *strings.Builder, items []any, depth int) {
	out.WriteString("[")
	if len(items) == 0 {
		out.WriteString(prettyEOL + strings.Repeat(prettyIndent, depth+1) + prettyEOL + strings.Repeat(prettyIndent, depth) + "]")
		return
	}
	out.WriteString(prettyEOL + strings.Repeat(prettyIndent, depth+1))
	for i, item := range items {
		if i > 0 {
			out.WriteString("," + prettyEOL + strings.Repeat(prettyIndent, depth+1))
		}
		writeValue(out, item, depth+1)
	}
	out.WriteString(prettyEOL + strings.Repeat(prettyIndent, depth) + "]")
}

func writeScalar(out *strings.Builder, value any) {
	switch typed := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if typed {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		writeJSONString(out, typed)
	case json.Number:
		out.WriteString(typed.String())
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			out.WriteString("null")
			return
		}
		out.Write(encoded)
	}
}

// writeJSONString emits a quoted string with ConvertToPrettyJson unescape
// behavior: quote and backslash stay escaped; newline, carriage return,
// tab, and \uXXXX sequences resolve to literal characters.
func writeJSONString(out *strings.Builder, value string) {
	out.WriteString(`"`)
	for _, r := range value {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString("\n")
		case '\r':
			out.WriteString("\r")
		case '\t':
			out.WriteString("\t")
		default:
			out.WriteRune(r)
		}
	}
	out.WriteString(`"`)
}
