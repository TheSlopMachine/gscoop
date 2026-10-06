package commands

import (
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RunCache mirrors libexec/scoop-cache.ps1 for the read-only paths: bare
// `scoop cache` and `scoop cache show [app]` list cache entries as
// Name/Version/Length rows plus a Total line. Removal belongs to the
// mutation engine and reports as unimplemented in Phase 1C.
func RunCache(env *Env, out io.Writer, args []string) int {
	cmd := ""
	var rest []string
	if len(args) > 0 {
		cmd = args[0]
		rest = args[1:]
	}
	switch strings.ToLower(cmd) {
	case "":
		return cacheShow(env, out, nil)
	case "show":
		return cacheShow(env, out, rest)
	case "rm":
		if len(rest) == 0 {
			Errorf(out, "<app(s)> missing")
			printUsage(out, "cache")
			return 1
		}
		Errorf(out, "scoop cache rm is not implemented in Phase 1C.")
		return 1
	default:
		return cacheShow(env, out, args)
	}
}

// cacheShow mirrors cacheshow in lib/cache.ps1: entries matching
// ^(app1|app2)#, or everything for empty and "*" filters.
func cacheShow(env *Env, out io.Writer, apps []string) int {
	pattern := ".*?"
	if len(apps) > 0 && !(len(apps) == 1 && apps[0] == "*") {
		pattern = "(" + strings.Join(apps, "|") + ")"
	}
	re, err := regexp.Compile("^" + pattern + "#")
	if err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	entries, err := os.ReadDir(env.CacheDir)
	if err != nil {
		entries = nil
	}
	type row struct {
		name    string
		version string
		length  int64
	}
	var rows []row
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !re.MatchString(entry.Name()) {
			continue
		}
		parts := strings.SplitN(entry.Name(), "#", 3)
		version := ""
		if len(parts) > 1 {
			version = parts[1]
		}
		var length int64
		if fi, err := entry.Info(); err == nil {
			length = fi.Size()
		}
		rows = append(rows, row{name: parts[0], version: version, length: length})
		total += length
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].name != rows[j].name {
			return rows[i].name < rows[j].name
		}
		return rows[i].version < rows[j].version
	})
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.name, r.version, strconv.FormatInt(r.length, 10)})
	}
	renderTable(out, []string{"Name", "Version", "Length"}, table)
	Successf(out, "Total: %d %s, %s", len(rows), pluralize(len(rows), "file", "files"), filesize(total))
	return 0
}
