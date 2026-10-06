package commands

import (
	"io"
)

// ReadOnly reports whether name is a Phase 1C read-only command wired in
// this package: list, info, cat, which, prefix, depends, status, export,
// cache, and checkup.
func ReadOnly(name string) bool {
	switch name {
	case "list", "info", "cat", "which", "prefix", "depends", "status", "export", "cache", "checkup":
		return true
	default:
		return false
	}
}

// Run dispatches one read-only command. It returns the exit code and true
// when name is wired here, or 0 and false for commands owned by later
// phases.
func Run(env *Env, out io.Writer, name string, args []string) (int, bool) {
	switch name {
	case "list":
		return RunList(env, out, args), true
	case "info":
		return RunInfo(env, out, args), true
	case "cat":
		return RunCat(env, out, args), true
	case "which":
		return RunWhich(env, out, args), true
	case "prefix":
		return RunPrefix(env, out, args), true
	case "depends":
		return RunDepends(env, out, args), true
	case "status":
		return RunStatus(env, out, args), true
	case "export":
		return RunExport(env, out, args), true
	case "cache":
		return RunCache(env, out, args), true
	case "checkup":
		return RunCheckup(env, out, args), true
	default:
		return 0, false
	}
}
