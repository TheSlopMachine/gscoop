package commands

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunWhich mirrors libexec/scoop-which.ps1: it resolves a command to the
// shim target or executable path. A missed lookup warns
// "'<command>' not found, not a scoop shim, or a broken shim." with exit 2
// (scoop-which.ps1:15-16, spec/cli-surface.md:118).
func RunWhich(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 || args[0] == "" {
		Errorf(out, "<command> missing")
		printUsage(out, "which")
		return 1
	}
	path := env.ResolveCommand(args[0])
	if path == "" {
		Warnf(out, "'%s' not found, not a scoop shim, or a broken shim.", args[0])
		return 2
	}
	fmt.Fprintln(out, friendlyPath(path))
	return 0
}

// ResolveCommand mirrors Get-CommandPath in lib/core.ps1 for the cases a
// read-only client can answer: scoop shim targets (user then global scope)
// and plain PATH executables.
//
// TODO(shim): resolve alias shims (scoop-<alias>.ps1) and PE-aware targets
// through internal/shim when it lands.
func (e *Env) ResolveCommand(command string) string {
	base := strings.ToLower(command)
	for _, global := range []bool{false, true} {
		dir := e.ShimDir(global)
		if target := shimTarget(filepath.Join(dir, base+".shim")); target != "" {
			return target
		}
		if _, err := os.Stat(filepath.Join(dir, base+".exe")); err == nil {
			if target := shimTarget(filepath.Join(dir, base+".shim")); target != "" {
				return target
			}
			return filepath.Join(dir, base+".exe")
		}
		for _, ext := range []string{".cmd", ".ps1"} {
			wrapper := filepath.Join(dir, base+ext)
			if _, err := os.Stat(wrapper); err == nil {
				if target := wrapperTarget(wrapper); target != "" {
					return target
				}
				return wrapper
			}
		}
	}
	if found, err := exec.LookPath(command); err == nil {
		return found
	}
	return ""
}

// shimTarget mirrors Get-ShimTarget for .shim files: the first path line,
// mapping Sysnative back to System32 on read.
func shimTarget(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if strings.HasPrefix(strings.ToLower(trimmed), "path") {
			rest := strings.TrimSpace(trimmed[len("path"):])
			rest = strings.TrimPrefix(rest, "=")
			target := strings.Trim(strings.TrimSpace(rest), `"'`)
			if target == "" {
				continue
			}
			return strings.ReplaceAll(target, `\Sysnative\`, `\System32\`)
		}
	}
	return ""
}

// wrapperTarget reads the @rem / # target comment from generated wrappers.
func wrapperTarget(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		for _, prefix := range []string{"@rem ", "rem ", "# "} {
			if strings.HasPrefix(strings.ToLower(trimmed), prefix) {
				target := strings.TrimSpace(trimmed[len(prefix):])
				if target != "" {
					return strings.ReplaceAll(target, `\Sysnative\`, `\System32\`)
				}
			}
		}
	}
	return ""
}
