// Alias and home verbs for Phase 3A (internal/commands/mutate_misc_*.go).
//
// RunAlias mirrors libexec/scoop-alias.ps1 over lib/commands.ps1
// add_alias, rm_alias, and list_aliases: alias shims are
// shims/scoop-<name>.ps1 files whose first line carries the summary
// and whose body carries the command, registered in the ALIAS config.
// RunHome mirrors libexec/scoop-home.ps1 through a browser opener
// seam. No emojis.
package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gscoop/internal/cli"
	"gscoop/internal/config"
)

// RunAlias mirrors libexec/scoop-alias.ps1: subcommands add, rm,
// list with -v/--verbose for list.
func RunAlias(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 {
		Errorf(out, "<subcommand> missing")
		printUsage(out, "alias")
		return 1
	}
	sub, rest := args[0], args[1:]
	if sub != "add" && sub != "rm" && sub != "list" {
		Errorf(out, "'%s' is not one of available subcommands: add, rm, list", sub)
		printUsage(out, "alias")
		return 1
	}
	r := cli.GetOpt(rest, "v", []string{"verbose"})
	if r.Err != "" {
		Errorf(out, "scoop alias: %s", r.Err)
		return 1
	}
	verbose := r.Has("v") || r.Has("verbose")
	name, command, description := "", "", ""
	if len(r.Rest) > 0 {
		name = r.Rest[0]
	}
	if len(r.Rest) > 1 {
		command = r.Rest[1]
	}
	if len(r.Rest) > 2 {
		description = r.Rest[2]
	}
	switch sub {
	case "add":
		if name == "" || command == "" {
			Errorf(out, "<name> and <command> must be specified for subcommand 'add'")
			return 1
		}
		return aliasAdd(env, out, name, command, description)
	case "rm":
		if name == "" {
			Errorf(out, "<name> must be specified for subcommand 'rm'")
			return 1
		}
		return aliasRemove(env, out, name)
	default:
		aliasList(env, out, verbose)
		return 0
	}
}

// aliasMap loads the ALIAS config as name to script pairs.
func aliasMap(env *Env) (map[string]string, *config.Store, error) {
	store, err := config.Load(env.ConfigPath)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	if v, ok := store.Value("alias"); ok && v != nil {
		if m, ok := v.(map[string]any); ok {
			for k, item := range m {
				if s, ok := item.(string); ok {
					out[k] = s
				}
			}
		}
	}
	return out, store, nil
}

func saveAliasMap(store *config.Store, aliases map[string]string) error {
	return store.Set("alias", aliases)
}

// aliasAdd mirrors add_alias (lib/commands.ps1:43-75).
func aliasAdd(env *Env, out io.Writer, name, command, description string) int {
	aliases, store, err := aliasMap(env)
	if err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if _, exists := aliases[name]; exists {
		Errorf(out, "Alias '%s' already exists.", name)
		return 1
	}
	script := "scoop-" + name
	if _, err := os.Stat(filepath.Join(env.ShimDir(false), script+".ps1")); err == nil {
		Errorf(out, "File '%s.ps1' already exists in shims directory.", script)
		return 1
	}
	content := "# Summary: " + description + "\n" + command + "\n"
	if err := os.MkdirAll(env.ShimDir(false), 0o755); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if err := os.WriteFile(filepath.Join(env.ShimDir(false), script+".ps1"), []byte(content), 0o644); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	aliases[name] = script
	if err := saveAliasMap(store, aliases); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if err := store.Save(); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	Successf(out, "Alias '%s' added.", name)
	return 0
}

// aliasRemove mirrors rm_alias (lib/commands.ps1:77-94).
func aliasRemove(env *Env, out io.Writer, name string) int {
	aliases, store, err := aliasMap(env)
	if err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if _, exists := aliases[name]; !exists {
		Errorf(out, "Alias '%s' doesn't exist.", name)
		return 1
	}
	Infof(out, "Removing alias '%s'...", name)
	_ = os.Remove(filepath.Join(env.ShimDir(false), "scoop-"+name+".ps1"))
	delete(aliases, name)
	if err := saveAliasMap(store, aliases); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if err := store.Save(); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	return 0
}

// aliasRow is one list_aliases row.
type aliasRow struct {
	name    string
	command string
	summary string
}

// aliasList mirrors list_aliases (lib/commands.ps1:96-128): rows
// sorted by name, missing scripts marked <BROKEN>.
func aliasList(env *Env, out io.Writer, verbose bool) {
	aliases, _, err := aliasMap(env)
	if err != nil {
		Errorf(out, "%s", err.Error())
		return
	}
	var rows []aliasRow
	for name := range aliases {
		if name == "" {
			continue
		}
		path := filepath.Join(env.ShimDir(false), "scoop-"+name+".ps1")
		data, err := os.ReadFile(path)
		if err != nil {
			rows = append(rows, aliasRow{name: name, command: "<BROKEN>"})
			continue
		}
		lines := strings.Split(string(data), "\n")
		summary := ""
		if len(lines) > 0 {
			summary = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[0]), "# Summary:"))
			summary = strings.TrimPrefix(summary, "Summary:")
			summary = strings.TrimSpace(summary)
		}
		command := ""
		if len(lines) > 1 {
			command = strings.TrimSpace(strings.Join(lines[1:], "\n"))
		}
		rows = append(rows, aliasRow{name: name, command: command, summary: summary})
	}
	if len(rows) == 0 {
		Infof(out, "No alias found.")
		return
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	if verbose {
		table := make([][]string, 0, len(rows))
		for _, r := range rows {
			table = append(table, []string{r.name, r.command, r.summary})
		}
		renderTable(out, []string{"Name", "Command", "Summary"}, table)
		return
	}
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.name, r.command})
	}
	renderTable(out, []string{"Name", "Command"}, table)
}

// openBrowser launches the system browser for a URL. Tests override it.
var openBrowser = func(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// RunHome mirrors libexec/scoop-home.ps1: positional app, manifest
// lookup, homepage open.
func RunHome(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 {
		printUsage(out, "home")
		return 1
	}
	app := args[0]
	hit := env.FindManifest(app, out)
	if hit.Manifest == nil {
		Errorf(out, "Could not find manifest for '%s'.", app)
		return 1
	}
	homepage := strings.TrimSpace(hit.Manifest.Homepage)
	if homepage == "" {
		Errorf(out, "Could not find homepage in manifest for '%s'.", app)
		return 1
	}
	fmt.Fprintf(out, "Opening %s\n", homepage)
	if err := openBrowser(homepage); err != nil {
		Errorf(out, "Could not open homepage: %s", err.Error())
		return 1
	}
	return 0
}

// aliasScriptForTest renders the alias shim body shared with fixtures.
func aliasScriptForTest(description, command string) string {
	return "# Summary: " + description + "\n" + command + "\n"
}
