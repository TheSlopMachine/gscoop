package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Schema is the compiled manifest contract from schema.json.
type Schema struct {
	compiled *jsonschema.Schema
}

// LoadSchema reads and compiles the schema document at path
// (C:\devel\Scoop\schema.json, draft-07).
func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return CompileSchema(data)
}

// CompileSchema compiles schema bytes. Regex format assertions are
// removed first: schema.json targets .NET regex syntax (validator.exe)
// while Go validates with RE2, so constructs such as named groups,
// backreferences, and lookarounds would fail spuriously. Hash and URL
// patterns stay enforced.
func CompileSchema(data []byte) (*Schema, error) {
	data, err := stripRegexFormat(data)
	if err != nil {
		return nil, err
	}
	compiled, err := jsonschema.CompileString("https://scoop.sh/draft/schema", string(data))
	if err != nil {
		return nil, err
	}
	return &Schema{compiled: compiled}, nil
}

// stripRegexFormat deletes every "format": "regex" member from a decoded
// schema document.
func stripRegexFormat(data []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return json.Marshal(stripRegexValue(value))
}

func stripRegexValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if key == "format" {
				if name, ok := item.(string); ok && name == "regex" {
					delete(typed, key)
					continue
				}
			}
			typed[key] = stripRegexValue(item)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = stripRegexValue(item)
		}
		return typed
	default:
		return value
	}
}

// ValidateBytes checks structural validity against schema.json.
func (s *Schema) ValidateBytes(data []byte) error {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := s.compiled.Validate(value); err != nil {
		return formatValidationError(err)
	}
	return nil
}

// ValidateManifest checks structural validity plus URL and hash patterns.
// The jsonschema library treats format as an annotation, so URI shape and
// the no-$ rule for plain urls are enforced here.
func (s *Schema) ValidateManifest(m *Manifest) error {
	if err := s.ValidateBytes(m.Raw); err != nil {
		return err
	}
	return CheckURLHashPatterns(m.Root)
}

// ValidateFile checks the manifest file at path.
func (s *Schema) ValidateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	manifest, err := Parse(data, AppNameFromURL(path), "", "", path)
	if err != nil {
		return err
	}
	return s.ValidateManifest(manifest)
}

func formatValidationError(err error) error {
	verr, ok := err.(*jsonschema.ValidationError)
	if !ok {
		return err
	}
	lines := flattenCauses(verr)
	if len(lines) == 0 {
		return err
	}
	return fmt.Errorf("schema validation failed:\n  %s", strings.Join(lines, "\n  "))
}

func flattenCauses(err *jsonschema.ValidationError) []string {
	if len(err.Causes) == 0 {
		location := err.InstanceLocation
		if location == "" {
			location = "(root)"
		}
		return []string{fmt.Sprintf("%s: %s", location, err.Message)}
	}
	lines := []string{}
	for _, cause := range err.Causes {
		lines = append(lines, flattenCauses(cause)...)
	}
	return lines
}

// CheckURLHashPatterns enforces the URI shape and no-$ rule for url
// values at the top level and under each architecture section.
func CheckURLHashPatterns(root *Object) error {
	locations := []string{"url"}
	if arch := root.Sub("architecture"); arch != nil {
		for _, name := range arch.Keys {
			locations = append(locations, "architecture."+name+".url")
		}
	}
	for _, location := range locations {
		node := lookupPath(root, strings.Split(location, "."))
		for _, candidate := range StringList(node) {
			if err := CheckManifestURL(candidate); err != nil {
				return fmt.Errorf("%s: %w", location, err)
			}
		}
	}
	return nil
}

// CheckManifestURL requires an absolute URI without autoupdate $ variables.
// It mirrors the uriOrArrayOfUris definition: format uri plus not $.
func CheckManifestURL(candidate string) error {
	if strings.Contains(candidate, "$") {
		return fmt.Errorf("plain url %q must not contain $ variables", candidate)
	}
	parsed, err := url.Parse(candidate)
	if err != nil || !parsed.IsAbs() {
		return fmt.Errorf("url %q is not an absolute URI", candidate)
	}
	return nil
}

func lookupPath(root *Object, parts []string) any {
	var current any = root
	for _, part := range parts {
		obj := AsObject(current)
		if obj == nil {
			return nil
		}
		value, ok := obj.Get(part)
		if !ok {
			return nil
		}
		current = value
	}
	return current
}
