// Command gscoop is the gscoop entry point.
//
// It mirrors the bin/scoop.ps1 dispatch shape: help flags list help,
// --version reports the snapshot version, known subcommands resolve help
// when the first argument is a help flag, and unknown subcommands fail with
// exit code 1. Subcommand lookup is case-insensitive. Phase 1C read-only commands (list, info, cat, which, prefix,
// depends, status, export, cache, checkup) execute via internal/commands,
// Phase 2B mutations (install, uninstall, reset, download, import, bucket,
// hold, unhold) and Phase 3A commands (update, cleanup, alias, home,
// virustotal, shim) execute through their runners over wired backends,
// misc commands (config, search, create) execute through theirs, and the
// hidden maintenance commands doctor and unswap run before subcommand
// lookup as intentional extras beyond the 28 classic subcommands.
// Remaining subcommands report as unimplemented until later phases.
// No emojis.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/commands"
)

// Version is the fallback snapshot build. The --version output prefers the
// newest "## [<version>]" line in CHANGELOG.md (bin/scoop.ps1:26) and falls
// back to this constant when no changelog entry is found.
const Version = "v0.0.0-phase0c"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 || cli.IsHelpFlag(args[0]) {
		fmt.Print(cli.TopHelp())
		return 0
	}
	if args[0] == "-v" || args[0] == "--version" {
		fmt.Println("Current Scoop version:")
		fmt.Println(versionLine())
		// TODO(buckets): print one "'<bucket>' bucket:" plus git log HEAD -1
		// --oneline block per git-backed local bucket (bin/scoop.ps1:32-39).
		return 0
	}
	if strings.EqualFold(args[0], "doctor") {
		return commands.RunDoctor(commands.DefaultEnv(), os.Stdout, args[1:])
	}
	if strings.EqualFold(args[0], "unswap") {
		return commands.RunUnswap(commands.DefaultEnv(), os.Stdout, args[1:])
	}
	cmd := cli.Lookup(args[0])
	if cmd == nil {
		fmt.Printf("WARN  scoop: '%s' isn't a scoop command. See 'scoop help'.\n", args[0])
		return 1
	}
	if len(args) > 1 && cli.IsHelpFlag(args[1]) {
		text, _ := cli.Help(cmd.Name)
		fmt.Print(text)
		return 0
	}
	if cmd.Name == "help" {
		text, code := cli.RunHelp(args[1:])
		fmt.Print(text)
		return code
	}
	env := commands.DefaultEnv()
	commands.WireDefaultBackends(env)
	if commands.ReadOnly(cmd.Name) {
		code, _ := commands.Run(env, os.Stdout, cmd.Name, args[1:])
		return code
	}
	// Phase 3A commands (update, cleanup, alias, home, virustotal,
	// shim) execute through the mutate runners.
	if code, ok := commands.RunPhase3A(env, os.Stdout, cmd.Name, args[1:]); ok {
		return code
	}
	// Phase 2B commands (install, uninstall, reset, download, import,
	// bucket, hold, unhold) execute through their runners over the wired
	// download and extraction backends.
	if code, ok := commands.RunPhase2B(env, os.Stdout, cmd.Name, args[1:]); ok {
		return code
	}
	// Misc commands (config, search, create) execute through their runners.
	if code, ok := commands.RunMisc(env, os.Stdout, cmd.Name, args[1:]); ok {
		return code
	}
	text, _ := cli.Help(cmd.Name)
	fmt.Print(text)
	fmt.Printf("INFO  command '%s' is recognized; execution is not implemented in Phase 0C.\n", cmd.Name)
	return 1
}

// versionLine prefers the newest versioned "## [...]" entry in CHANGELOG.md
// and falls back to the Version constant when no entry is found.
func versionLine() string {
	if v := newestChangelogVersion(); v != "" {
		return v
	}
	return Version
}

// newestChangelogVersion returns the first versioned changelog heading,
// skipping "## [Unreleased]". The trailing suffix after "]" is kept verbatim
// when present.
func newestChangelogVersion() string {
	path := findChangelog()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if !strings.HasPrefix(trimmed, "## [") {
			continue
		}
		end := strings.Index(trimmed, "]")
		if end < 0 {
			continue
		}
		token := trimmed[len("## ["):end]
		if strings.EqualFold(token, "Unreleased") {
			continue
		}
		rest := strings.TrimSpace(trimmed[end+1:])
		rest = strings.TrimPrefix(rest, "-")
		rest = strings.TrimSpace(rest)
		if rest != "" {
			return token + " - " + rest
		}
		return token
	}
	return ""
}

// findChangelog locates CHANGELOG.md next to the working directory or the
// executable directory.
func findChangelog() string {
	candidates := []string{"CHANGELOG.md"}
	if exe, err := os.Executable(); err == nil && exe != "" {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "CHANGELOG.md"),
			filepath.Join(dir, "..", "CHANGELOG.md"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}
