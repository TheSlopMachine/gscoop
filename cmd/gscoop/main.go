// Command gscoop is the gscoop entry point.
//
// It mirrors the bin/scoop.ps1 dispatch shape: help flags list help,
// --version reports the snapshot version, known subcommands resolve help
// when the first argument is a help flag, and unknown subcommands fail with
// exit code 1. Phase 1C read-only commands (list, info, cat, which, prefix,
// depends, status, export, cache, checkup) execute via internal/commands,
// Phase 2B mutations (install, uninstall, reset, download, import, bucket,
// hold, unhold) and Phase 3A commands (update, cleanup, alias, home,
// virustotal, shim) execute through their runners over wired backends,
// misc commands (config, search, create) execute through theirs, and the
// hidden maintenance commands doctor and unswap run before subcommand
// lookup. Remaining subcommands report as unimplemented until later phases.
// No emojis.
package main

import (
	"fmt"
	"os"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/commands"
)

// Version marks the snapshot build. Full version plumbing (CHANGELOG plus
// per-bucket git revisions, mirroring bin/scoop.ps1) arrives with later phases.
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
		fmt.Println(Version)
		return 0
	}
	if args[0] == "doctor" {
		return commands.RunDoctor(commands.DefaultEnv(), os.Stdout, args[1:])
	}
	if args[0] == "unswap" {
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
