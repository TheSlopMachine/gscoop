// Package hook runs manifest lifecycle scripts through powershell.exe.
//
// Classic executes hooks in-process via scriptblock with dynamic scoping
// (lib/install.ps1:143-168). Go reproduces the observable surface by
// writing a temp file of preamble plus user script and invoking
// powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass
// -File <tmp.ps1> with cwd set to the version dir. Values cross the
// boundary through environment variables, never string interpolation.
// Stdout and stderr stream with the Running <hook> framing classic
// prints. Non-zero exit aborts the transaction. Ctrl-C propagates
// through context cancellation. No emojis.
package hook

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Types mirrors the ValidateSet in Invoke-HookScript
// (lib/install.ps1:147).
const (
	TypeInstaller     = "installer"
	TypePreInstall    = "pre_install"
	TypePostInstall   = "post_install"
	TypeUninstaller   = "uninstaller"
	TypePreUninstall  = "pre_uninstall"
	TypePostUninstall = "post_uninstall"
)

// Valid reports whether hook names the Invoke-HookScript set.
func Valid(hook string) bool {
	switch hook {
	case TypeInstaller, TypePreInstall, TypePostInstall,
		TypeUninstaller, TypePreUninstall, TypePostUninstall:
		return true
	}
	return false
}

// Vars carries the dynamic-scoping surface manifest scripts observe:
// $dir, $original_dir, $persist_dir, $version, $global, $architecture,
// plus $SCOOP and $SCOOP_GLOBAL (plan section 7.2).
type Vars struct {
	Dir          string
	OriginalDir  string
	PersistDir   string
	Version      string
	Architecture string
	Global       bool
	ScoopDir     string
	ScoopGlobal  string
}

// Runner executes hooks. PSHost names the PowerShell host binary
// (powershell.exe by default, PSHOST config may select pwsh).
// Dir is the version dir used as cwd. Out and Err receive framed
// output; nil falls back to os.Stdout and os.Stderr.
type Runner struct {
	PSHost string
	Dir    string
	Out    io.Writer
	Err    io.Writer
}

// Host resolves the effective host binary.
func (r *Runner) Host() string {
	if r.PSHost != "" {
		return r.PSHost
	}
	return "powershell.exe"
}

func (r *Runner) out() io.Writer {
	if r.Out == nil {
		return os.Stdout
	}
	return r.Out
}

func (r *Runner) errOut() io.Writer {
	if r.Err == nil {
		return os.Stderr
	}
	return r.Err
}

// Preamble renders the variable-injection preamble. Values travel
// through environment variables named GSCOOP_HOOK_* so user scripts
// never interpolate. The preamble assigns PowerShell variables from
// those environment entries.
func Preamble(vars Vars) string {
	var b strings.Builder
	b.WriteString("$dir = $env:GSCOOP_HOOK_DIR\n")
	b.WriteString("$original_dir = $env:GSCOOP_HOOK_ORIGINAL_DIR\n")
	b.WriteString("$persist_dir = $env:GSCOOP_HOOK_PERSIST_DIR\n")
	b.WriteString("$version = $env:GSCOOP_HOOK_VERSION\n")
	b.WriteString("$architecture = $env:GSCOOP_HOOK_ARCHITECTURE\n")
	global := "$false"
	if vars.Global {
		global = "$true"
	}
	b.WriteString("$global = " + global + "\n")
	b.WriteString("$SCOOP = $env:GSCOOP_HOOK_SCOOP\n")
	b.WriteString("$SCOOP_GLOBAL = $env:GSCOOP_HOOK_SCOOP_GLOBAL\n")
	return b.String()
}

// Environ converts vars to GSCOOP_HOOK_* environment entries.
func Environ(vars Vars) []string {
	global := "0"
	if vars.Global {
		global = "1"
	}
	return []string{
		"GSCOOP_HOOK_DIR=" + vars.Dir,
		"GSCOOP_HOOK_ORIGINAL_DIR=" + vars.OriginalDir,
		"GSCOOP_HOOK_PERSIST_DIR=" + vars.PersistDir,
		"GSCOOP_HOOK_VERSION=" + vars.Version,
		"GSCOOP_HOOK_ARCHITECTURE=" + vars.Architecture,
		"GSCOOP_HOOK_GLOBAL=" + global,
		"GSCOOP_HOOK_SCOOP=" + vars.ScoopDir,
		"GSCOOP_HOOK_SCOOP_GLOBAL=" + vars.ScoopGlobal,
	}
}

// JoinScript joins string-or-array script values with CRLF, mirroring
// $script -join "`r`n" (lib/install.ps1:165).
func JoinScript(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case []string:
		return strings.Join(s, "\r\n")
	case []any:
		parts := make([]string, 0, len(s))
		for _, item := range s {
			if text, ok := item.(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\r\n")
	default:
		return ""
	}
}

// Substitute replaces $dir, $original_dir, $persist_dir, $version, and
// $global tokens in installer.args, mirroring substitute()
// (lib/core.ps1:1286, lib/install.ps1:117-122). Keys sort by length
// descending so longer tokens win.
func Substitute(entity string, vars Vars) string {
	params := map[string]string{
		"$original_dir": vars.OriginalDir,
		"$persist_dir":  vars.PersistDir,
		"$dir":          vars.Dir,
		"$version":      vars.Version,
		"$global":       globalToken(vars.Global),
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	out := entity
	for _, k := range keys {
		out = strings.ReplaceAll(out, k, params[k])
	}
	return out
}

// SubstituteAll applies Substitute to every arg.
func SubstituteAll(args []string, vars Vars) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, Substitute(a, vars))
	}
	return out
}

func globalToken(global bool) string {
	if global {
		return "True"
	}
	return "False"
}

// RunScript executes an inline hook script. Empty scripts are a no-op
// returning nil. Otherwise it prints Running <hook> script... framing,
// invokes the host, and returns an error on non-zero exit.
func (r *Runner) RunScript(ctx context.Context, hook string, vars Vars, script string) error {
	if strings.TrimSpace(script) == "" {
		return nil
	}
	if !Valid(hook) {
		return fmt.Errorf("unknown hook %q", hook)
	}
	tmp, err := os.CreateTemp("", "gscoop-hook-*.ps1")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	content := Preamble(vars) + script + "\n"
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	tmp.Close()
	defer os.Remove(tmpName)
	fmt.Fprintf(r.out(), "Running %s script... ", hook)
	err = r.runFile(ctx, vars, tmpName, nil)
	if err != nil {
		fmt.Fprintln(r.out(), "")
		return err
	}
	fmt.Fprintln(r.out(), "Done.")
	return nil
}

// RunPS1File executes a .ps1 installer file with param-safe argv.
// Args pass through substitute() first, then travel as -File argv,
// matching & $progName @fnArgs semantics.
func (r *Runner) RunPS1File(ctx context.Context, hook string, vars Vars, file string, args []string) error {
	sub := SubstituteAll(args, vars)
	fmt.Fprintf(r.out(), "Running %s ... ", labelFor(hook))
	err := r.runFile(ctx, vars, file, sub)
	if err != nil {
		fmt.Fprintln(r.out(), "")
		return err
	}
	fmt.Fprintln(r.out(), "Done.")
	return nil
}

// RunExecutable runs a non-ps1 installer file directly via os/exec,
// mirroring Invoke-ExternalCommand for the payload (not a runtime).
// Activity framing matches Running <type> ... / Done. framing.
func RunExecutable(ctx context.Context, out io.Writer, activity, file string, args []string) error {
	if out == nil {
		out = os.Stdout
	}
	if activity != "" {
		fmt.Fprintf(out, "%s ", activity)
	}
	cmd := exec.CommandContext(ctx, file, args...)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(out, "")
		return fmt.Errorf("%s failed: %w", activity, err)
	}
	fmt.Fprintln(out, "Done.")
	return nil
}

func labelFor(hook string) string {
	if hook == "" {
		return "installer"
	}
	return hook
}

func (r *Runner) runFile(ctx context.Context, vars Vars, file string, args []string) error {
	argv := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", file}
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, r.Host(), argv...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), Environ(vars)...)
	cmd.Stdout = r.out()
	cmd.Stderr = r.errOut()
	if cmd.Dir == "" {
		if cwd, err := os.Getwd(); err == nil {
			cmd.Dir = cwd
		}
	}
	// Ensure the version dir exists before using it as cwd.
	if cmd.Dir != "" {
		if _, err := os.Stat(cmd.Dir); err != nil {
			cmd.Dir = ""
		}
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hook script failed: %w", err)
	}
	return nil
}

// ScriptPathForTest writes preamble plus script to dir for golden
// fixtures without executing a host.
func ScriptPathForTest(dir string, vars Vars, script string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "hook.ps1")
	if err := os.WriteFile(path, []byte(Preamble(vars)+script+"\n"), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
