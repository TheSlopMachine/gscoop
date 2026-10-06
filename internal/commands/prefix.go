package commands

import (
	"fmt"
	"io"
	"os"
)

// RunPrefix mirrors libexec/scoop-prefix.ps1: it prints the current path of
// an installed app, checking user scope before global scope.
func RunPrefix(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 || args[0] == "" {
		printUsage(out, "prefix")
		return 1
	}
	app := args[0]
	if hasWildcard(app) {
		fmt.Fprintf(out, "Couldn't find manifest for '%s'.\n", app)
		return 1
	}
	path := env.CurrentDir(app, false)
	if _, err := os.Stat(path); err != nil {
		path = env.CurrentDir(app, true)
	}
	if _, err := os.Stat(path); err != nil {
		// Classic aborts with a bare message (no ERROR prefix).
		fmt.Fprintf(out, "Could not find app path for '%s'.\n", app)
		return 1
	}
	fmt.Fprintln(out, path)
	return 0
}
