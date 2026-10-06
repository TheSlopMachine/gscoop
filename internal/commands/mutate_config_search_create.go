// Config, search, and create wiring (internal/commands/mutate_config_search_create.go).
//
// RunConfig mirrors libexec/scoop-config.ps1: bare `scoop config` lists
// settings, `scoop config <name>` prints one value, `scoop config <name>
// <value>` stores it, and `scoop config rm <name>` removes it. Values
// render through config.Store.Display, matching the classic null and
// DateTime handling.
// RunSearch mirrors the local path of libexec/scoop-search.ps1 through
// internal/search: empty queries list every manifest, invalid regex falls
// back to literal matching with a warning, and no matches fail closed.
// Remote-bucket search stays unwired.
// RunCreate mirrors libexec/scoop-create.ps1 for the non-interactive
// defaults: the app name is the URL file stem and the version stays empty,
// matching empty choose_item answers. Interactive number-pick prompts have
// no equivalent here. No emojis.
package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/search"
)

// OwnsMisc reports whether name is a misc command wired here: config,
// search, create.
func OwnsMisc(name string) bool {
	switch name {
	case "config", "search", "create":
		return true
	default:
		return false
	}
}

// RunMisc dispatches one misc command. It returns the exit code and true
// when name is owned here, or 0 and false otherwise.
func RunMisc(env *Env, out io.Writer, name string, args []string) (int, bool) {
	switch name {
	case "config":
		return RunConfig(env, out, args), true
	case "search":
		return RunSearch(env, out, args), true
	case "create":
		return RunCreate(env, out, args), true
	default:
		return 0, false
	}
}

// RunConfig mirrors scoop-config.ps1 argument handling: no args lists all
// settings, `rm <name>` removes one, `<name> <value>` stores one, and
// `<name>` prints one. Unknown names read as unset; sets accept any name,
// like set_config.
func RunConfig(env *Env, out io.Writer, args []string) int {
	store, err := config.Load(env.ConfigPath)
	if err != nil {
		Errorf(out, "Could not load config: %s", err.Error())
		return 1
	}
	switch {
	case len(args) == 0:
		for _, key := range store.Keys() {
			text, ok := store.Display(key)
			if !ok {
				text = "null"
			}
			fmt.Fprintf(out, "%s: %s\n", key, text)
		}
		return 0
	case args[0] == "rm":
		if len(args) < 2 {
			Errorf(out, "<name> missing")
			printUsage(out, "config")
			return 1
		}
		name := args[1]
		store.Remove(name)
		if err := store.Save(); err != nil {
			Errorf(out, "Could not save config: %s", err.Error())
			return 1
		}
		fmt.Fprintf(out, "'%s' has been removed\n", name)
		return 0
	case len(args) >= 2:
		name, value := args[0], args[1]
		if err := store.Set(name, value); err != nil {
			Errorf(out, "Could not set config: %s", err.Error())
			return 1
		}
		if err := store.Save(); err != nil {
			Errorf(out, "Could not save config: %s", err.Error())
			return 1
		}
		fmt.Fprintf(out, "'%s' has been set to '%s'\n", name, value)
		return 0
	default:
		name := args[0]
		text, ok := store.Display(name)
		if !ok {
			fmt.Fprintf(out, "'%s' is not set\n", name)
			return 0
		}
		fmt.Fprintln(out, text)
		return 0
	}
}

// RunSearch mirrors the local-bucket path of scoop-search.ps1: buckets
// scan in local order, name matches win over bin matches, and rows print
// as Name/Version/Source/Binaries. An empty query lists every manifest.
// Remote search over uninstalled known buckets stays unwired by design: no
// network fallback runs here, so empty local results fail closed.
func RunSearch(env *Env, out io.Writer, args []string) int {
	query := strings.Join(args, " ")
	results, literal, err := search.SearchLocal(env.ScoopDir, env.ListBucketNames(), query)
	if err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if literal {
		fmt.Fprintln(out, search.UnsupportedSyntaxWarning)
	}
	if len(results) == 0 {
		Warnf(out, "No matches found.")
		return 1
	}
	fmt.Fprintln(out, "Results from local buckets...")
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{r.Name, r.Version, r.Source, r.Binaries})
	}
	renderTable(out, []string{"Name", "Version", "Source", "Binaries"}, rows)
	return 0
}

// RunCreate scaffolds a manifest from a download URL, mirroring
// create_manifest with empty interactive answers: the name is the URL file
// stem, the version is empty, and the remaining fields are empty strings.
// The document writes as indented JSON next to the working directory.
func RunCreate(env *Env, out io.Writer, args []string) int {
	_ = env
	if len(args) == 0 {
		Errorf(out, "<url> missing")
		printUsage(out, "create")
		return 1
	}
	rawurl := args[0]
	parsed, err := url.Parse(rawurl)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		Errorf(out, "Error: %s is not a valid URL", rawurl)
		return 1
	}
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	leaf := segments[len(segments)-1]
	if leaf == "" {
		Errorf(out, "Error: %s is not a valid URL", rawurl)
		return 1
	}
	name := leaf
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	doc := [][2]string{
		{"homepage", ""},
		{"license", ""},
		{"version", ""},
		{"url", rawurl},
		{"hash", ""},
		{"extract_dir", ""},
		{"bin", ""},
		{"depends", ""},
	}
	var buf strings.Builder
	buf.WriteString("{\n")
	for i, kv := range doc {
		keyJSON, _ := json.Marshal(kv[0])
		valJSON, _ := json.Marshal(kv[1])
		buf.WriteString("  ")
		buf.Write(keyJSON)
		buf.WriteString(": ")
		buf.Write(valJSON)
		if i+1 < len(doc) {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	cwd, err := os.Getwd()
	if err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	path := filepath.Join(cwd, name+".json")
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	fmt.Fprintf(out, "Created '%s'.\n", path)
	return 0
}
