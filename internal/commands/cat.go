package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"gscoop/internal/cli"
)

// RunCat mirrors libexec/scoop-cat.ps1: it prints the backing manifest as
// indented JSON. The bat/cat_style pretty-print path is intentionally not
// reproduced; output is plain JSON on stdout in all cases.
func RunCat(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 || args[0] == "" {
		Errorf(out, "<app> missing")
		printUsage(out, "cat")
		return 1
	}
	hit := env.FindManifest(args[0], out)
	if hit.Manifest == nil || len(hit.Raw) == 0 {
		// Classic aborts here, which prints without the ERROR prefix.
		if hit.Bucket != "" {
			fmt.Fprintf(out, "Couldn't find manifest for '%s' from '%s' bucket.\n", hit.Name, hit.Bucket)
		} else if hit.URL != "" {
			fmt.Fprintf(out, "Couldn't find manifest for '%s' at '%s'.\n", hit.Name, hit.URL)
		} else {
			fmt.Fprintf(out, "Couldn't find manifest for '%s'.\n", hit.Name)
		}
		return 1
	}
	fmt.Fprintln(out, prettyJSON(hit.Raw))
	return 0
}

// prettyJSON re-indents manifest bytes with 4-space indent, matching the
// ConvertToPrettyJson shape (indent and ": " separators) with LF endings.
// Keys sort alphabetically through encoding/json; document order waits on
// the ordered manifest writer.
//
// TODO(manifest): preserve document key order via internal/manifest.
func prettyJSON(raw []byte) string {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	indented, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		return string(raw)
	}
	return string(indented)
}

func printUsage(out io.Writer, name string) {
	if cmd := cli.Lookup(name); cmd != nil {
		fmt.Fprintf(out, "Usage: %s\n", cmd.Usage)
	}
}

// manifestRaw returns the raw manifest bytes for an app spec, or nil when
// the manifest cannot be resolved locally.
func manifestRaw(env *Env, out io.Writer, spec string) []byte {
	hit := env.FindManifest(spec, out)
	if hit == nil || len(hit.Raw) == 0 {
		return nil
	}
	return hit.Raw
}
