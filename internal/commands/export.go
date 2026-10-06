package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/config"
)

// exportStripped lists machine-specific config keys excluded from
// scoop export -c output (libexec/scoop-export.ps1:13-15).
var exportStripped = []string{"last_update", "root_path", "global_path", "cache_path", "alias"}

// RunExport mirrors libexec/scoop-export.ps1: installed buckets and apps
// as pretty JSON. Only the first argument selects -c/--config output.
// Single-element arrays collapse to scalars, matching ConvertToPrettyJson
// via normalize_values in lib/json.ps1.
func RunExport(env *Env, out io.Writer, args []string) int {
	withConfig := len(args) > 0 && (args[0] == "-c" || args[0] == "--config")
	var top []pair
	if withConfig {
		top = append(top, pair{key: "config", val: normalizeValue(exportConfig(env))})
	}
	top = append(top, pair{key: "buckets", val: normalizeValue(exportBuckets(env))})
	top = append(top, pair{key: "apps", val: normalizeValue(exportApps(env))})
	fmt.Fprintln(out, prettyScan(compactValue(obj(top))))
	return 0
}

// exportConfig reads the live config minus machine-specific keys.
//
// TODO(config): apply Complete-ConfigChange side-effect values through
// internal/config instead of raw file values when that seam lands.
func exportConfig(env *Env) any {
	store, err := config.Load(env.ConfigPath)
	if err != nil || store.Len() == 0 {
		return nil
	}
	stripped := map[string]bool{}
	for _, k := range exportStripped {
		stripped[k] = true
	}
	vals := map[string]any{}
	for _, k := range store.Keys() {
		if stripped[strings.ToLower(k)] {
			continue
		}
		raw, _ := store.Get(k)
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			continue
		}
		vals[k] = v
	}
	return vals
}

// exportBuckets mirrors list_buckets in lib/buckets.ps1.
func exportBuckets(env *Env) []any {
	var out []any
	for _, b := range env.ListBuckets() {
		updated := b.Updated
		var updatedVal any = updated
		if updated == "" {
			updatedVal = nil
		}
		out = append(out, obj{
			{key: "Name", val: b.Name},
			{key: "Source", val: b.Source},
			{key: "Updated", val: updatedVal},
			{key: "Manifests", val: b.Manifests},
		})
	}
	return out
}

// exportApps mirrors the scoop-list.ps1 projection consumed by
// scoop-export.ps1, without the console header.
func exportApps(env *Env) []any {
	rows, err := env.CollectApps("")
	if err != nil {
		return nil
	}
	var out []any
	for _, r := range rows {
		fields := obj{
			{key: "Name", val: r.Name},
			{key: "Version", val: r.Version},
		}
		if r.Source != "" {
			fields = append(fields, pair{key: "Source", val: r.Source})
		}
		fields = append(fields, pair{key: "Updated", val: r.Updated}, pair{key: "Info", val: r.Info})
		out = append(out, fields)
	}
	return out
}

// pair is one ordered object field. obj preserves document order end to
// end so exports stay diff-clean (technical plan section 6.1: ordered
// writer, never map iteration).
type pair struct {
	key string
	val any
}

// obj is an ordered JSON object.
type obj []pair

// normalizeValue ports normalize_values in lib/json.ps1: multiline strings
// split into arrays, single-element arrays collapse to their scalar, and
// nested single-element arrays unwrap one level. Object properties recurse;
// array elements holding objects are left as-is.
func normalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = normalizeValue(e)
		}
		return t
	case obj:
		for i, p := range t {
			t[i].val = normalizeValue(p.val)
		}
		return t
	case string:
		parts := strings.Split(t, "\n")
		var lines []string
		for _, p := range parts {
			lines = append(lines, strings.TrimSpace(strings.TrimSuffix(p, "\r")))
		}
		if len(lines) > 1 {
			out := make([]any, 0, len(lines))
			for _, l := range lines {
				out = append(out, l)
			}
			return out
		}
		return t
	case []any:
		if len(t) == 1 {
			if _, isArr := t[0].([]any); !isArr {
				return t[0]
			}
			return t
		}
		out := make([]any, 0, len(t))
		for _, e := range t {
			if inner, ok := e.([]any); ok && len(inner) == 1 {
				out = append(out, inner[0])
			} else {
				out = append(out, e)
			}
		}
		return out
	default:
		return v
	}
}

// compactValue renders compact JSON without HTML escaping, matching
// ConvertTo-Json -Compress input to the pretty printer. Plain maps sort
// their keys for determinism; obj keeps document order.
func compactValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		return quoteJSON(t)
	case json.Number:
		return t.String()
	case int:
		return fmt.Sprintf("%d", t)
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(t); err != nil {
			return "0"
		}
		return strings.TrimSuffix(buf.String(), "\n")
	case obj:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			parts = append(parts, quoteJSON(p.key)+":"+compactValue(normalizePlain(p.val)))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, quoteJSON(k)+":"+compactValue(normalizePlain(t[k])))
		}
		return "{" + strings.Join(parts, ",") + "}"
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, compactValue(normalizePlain(e)))
		}
		return "[" + strings.Join(parts, ",") + "]"
	default:
		return quoteJSON(fmt.Sprintf("%v", t))
	}
}

// normalizePlain converts map/object nesting for the compact writer.
func normalizePlain(v any) any {
	return v
}

func quoteJSON(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// prettyScan ports the ConvertToPrettyJson character scan in lib/json.ps1:
// 4-space indent, ": " after colons, newline after commas and openers. The
// classic CRLF ending normalizes to LF here, matching the testdata/cli
// convention. Escape sequences inside strings unescape as classic does.
func prettyScan(compact string) string {
	var out strings.Builder
	depth := 0
	inString := false
	indent := "    "
	eol := "\n"
	i := 0
	for i < len(compact) {
		c := compact[i]
		if c == '"' {
			inString = !inString
		}
		if c == '\\' && i+1 < len(compact) {
			seq := compact[i : i+2]
			if seq == `\u` && i+5 < len(compact) {
				if r, ok := parseHex4(compact[i+2 : i+6]); ok {
					out.WriteRune(r)
					i += 6
					continue
				}
			}
			if inString {
				switch seq {
				case `\n`:
					out.WriteString("\n")
					i += 2
					continue
				case `\r`:
					out.WriteString("\r")
					i += 2
					continue
				case `\t`:
					out.WriteString("\t")
					i += 2
					continue
				}
			}
			out.WriteString(seq)
			i += 2
			continue
		}
		isOpen := !inString && (c == '{' || c == '[')
		isClose := !inString && (c == '}' || c == ']')
		isColon := !inString && c == ':'
		isComma := !inString && c == ','
		if isOpen {
			depth++
		} else if isClose {
			depth--
			out.WriteString(eol + strings.Repeat(indent, depth))
		}
		out.WriteByte(c)
		if isColon {
			out.WriteString(" ")
		} else if isComma || isOpen {
			out.WriteString(eol + strings.Repeat(indent, depth))
		}
		i++
	}
	return collapseBlankLines(out.String())
}

// collapseBlankLines clears whitespace-only lines emitted for empty
// objects and arrays, keeping files free of trailing whitespace.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.Trim(l, " ") == "" {
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

func parseHex4(s string) (rune, bool) {
	var v rune
	for i := 0; i < 4; i++ {
		c := s[i]
		var d rune
		switch {
		case '0' <= c && c <= '9':
			d = rune(c - '0')
		case 'a' <= c && c <= 'f':
			d = rune(c-'a') + 10
		case 'A' <= c && c <= 'F':
			d = rune(c-'A') + 10
		default:
			return 0, false
		}
		v = v*16 + d
	}
	return v, true
}
