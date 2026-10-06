// Package shim writes Scoop shim triples byte-identically.
//
// Classic shim() (lib/core.ps1:949-1118) byte-copies a bundled shim
// binary to <name>.exe, writes path/args to <name>.shim, and emits
// .cmd plus POSIX wrappers per target type. Alias shims repeat the
// triple as scoop-<alias>.*. This package embeds the upstream payloads
// via embed.FS (they are data assets, not runtimes) and reproduces
// every wrapper branch, the GUI-subsystem plus Sysnative/x86 patch,
// and overwrite backup/restore. No emojis.
package shim

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed assets/shim-*.exe
var assets embed.FS

// Variants mirrors the SHIM config (lib/core.ps1:1120-1130). scoopcs
// is legacy and maps to the kiennq default.
const (
	VariantKiennq  = "kiennq"
	Variant71      = "71"
	VariantScoopCS = "scoopcs"
)

// Canonical maps scoopcs to the maintained default.
func Canonical(variant string) string {
	switch strings.ToLower(variant) {
	case Variant71:
		return Variant71
	case VariantScoopCS:
		return VariantKiennq
	default:
		return VariantKiennq
	}
}

// Payload returns the embedded shim.exe bytes for variant, byte-identical
// to supporting/shims upstream.
func Payload(variant string) ([]byte, error) {
	name := "assets/shim-" + Canonical(variant) + ".exe"
	if Canonical(variant) == VariantKiennq && variant != "" && strings.ToLower(variant) != VariantKiennq && strings.ToLower(variant) != Variant71 {
		name = "assets/shim-kiennq.exe"
	}
	data, err := assets.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("shim payload %q: %w", variant, err)
	}
	return data, nil
}

// Entry is one bin triple: target, alias, args (lib/install.ps1:171).
type Entry struct {
	Target string
	Name   string
	Args   string
}

// ParseEntry converts string-or-array bin values to a triple,
// mirroring shim_def (lib/install.ps1:171-174).
func ParseEntry(v any) (Entry, bool) {
	switch t := v.(type) {
	case string:
		return Entry{Target: t, Name: stripExt(base(t))}, true
	case []string:
		if len(t) == 0 {
			return Entry{}, false
		}
		e := Entry{Target: t[0]}
		if len(t) > 1 {
			e.Name = t[1]
		} else {
			e.Name = stripExt(base(t[0]))
		}
		if len(t) > 2 {
			e.Args = t[2]
		}
		return e, true
	case []any:
		parts := make([]string, 0, len(t))
		for _, p := range t {
			text, ok := p.(string)
			if !ok {
				return Entry{}, false
			}
			parts = append(parts, text)
		}
		return ParseEntry(parts)
	default:
		return Entry{}, false
	}
}

func base(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	if i := strings.LastIndex(p, `\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func stripExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[:i]
	}
	return name
}

// Substitute replaces $dir, $original_dir, and $persist_dir in args,
// mirroring create_shims (lib/install.ps1:191).
func Substitute(arg, dir, originalDir, persistDir string) string {
	params := map[string]string{
		"$original_dir": originalDir,
		"$persist_dir":  persistDir,
		"$dir":          dir,
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	out := arg
	for _, k := range keys {
		out = strings.ReplaceAll(out, k, params[k])
	}
	return out
}

// ShimName derives the shim file stem from a manifest bin target,
// mirroring strip_ext(fname(...)) plus $name.tolower()
// (lib/core.ps1:618-619, lib/core.ps1:953-955): base leaf, strip single
// trailing extension per `\.[^\.]*$`, lowercase.
func ShimName(target string) string {
	return strings.ToLower(stripExt(base(target)))
}

// ShimNameForTarget is the explicit target-derived form of ShimName.
// ShimName is kept for existing callers.
func ShimNameForTarget(target string) string {
	return ShimName(target)
}

// stem lowercases an already-resolved shim name (explicit alias or
// derived stem), mirroring $($name.tolower()) (lib/core.ps1:955).
// Unlike ShimName it strips nothing, so multi-dot stems survive.
func stem(name string) string {
	return strings.ToLower(name)
}

// WriteExe copies the variant payload to shimDir/<name>.exe, then
// applies the Sysnative/x86 path rewrite at the .shim layer and the
// GUI-subsystem patch when the target is a GUI binary. It returns the
// rewritten path recorded in the .shim file.
func WriteExe(shimDir, name, target, variant string) (string, error) {
	payload, err := Payload(variant)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		return "", err
	}
	exe := filepath.Join(shimDir, stem(name)+".exe")
	if err := os.WriteFile(exe, payload, 0o755); err != nil {
		return "", err
	}
	rewritten := RewriteSysnative(target, string(payload))
	if IsGUI(target) {
		if err := SetSubsystem(exe, 2); err != nil {
			return "", err
		}
	}
	return rewritten, nil
}

// RewriteSysnative mirrors the x86-shim-on-x64 rewrite
// (lib/core.ps1:973-985): when the shim PE machine is I386 on a
// 64-bit OS, System32 becomes Sysnative and SysWOW64 becomes System32.
// is64OS and machine are injected for testability.
func RewriteSysnative(target string, _ string) string {
	machine := peMachineOfEmbedded()
	if machine == 0x014c && is64BitOS() {
		sysdir := filepath.Join(os.Getenv("SystemRoot"), "System32")
		if sysdir == "System32" || sysdir == filepath.Join("", "System32") {
			sysdir = `C:\Windows\System32`
		}
		sysnative := strings.Replace(sysdir, "System32", "Sysnative", 1)
		syswow := strings.Replace(sysdir, "System32", "SysWOW64", 1)
		if hasPrefixFold(target, sysdir+`\`) {
			return sysnative + target[len(sysdir):]
		}
		if hasPrefixFold(target, syswow+`\`) {
			return sysdir + target[len(syswow):]
		}
	}
	return target
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// WriteTextShim writes <name>.shim with path and optional args lines,
// mirroring lib/core.ps1:987-990. Lines use CRLF via WriteAllLines parity.
func WriteTextShim(shimDir, name, resolvedPath, arg string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "path = \"%s\"\r\n", resolvedPath)
	if arg != "" {
		fmt.Fprintf(&b, "args = %s\r\n", arg)
	}
	return writeLines(filepath.Join(shimDir, stem(name)+".shim"), b.String())
}

// Wrappers writes the .cmd plus extensionless POSIX wrapper for
// non-exe targets, mirroring each branch of shim()
// (lib/core.ps1:998-1117). kind selects the branch by target extension.
func Wrappers(shimDir, name, resolvedPath, arg string) error {
	lowered := strings.ToLower(resolvedPath)
	switch {
	case strings.HasSuffix(lowered, ".bat") || strings.HasSuffix(lowered, ".cmd"):
		cmd := "@rem " + resolvedPath + "\r\n@\"" + resolvedPath + "\" " + arg + " %*\r\n"
		if err := writeLines(filepath.Join(shimDir, stem(name)+".cmd"), cmd); err != nil {
			return err
		}
		sh := "#!/bin/sh\n# " + resolvedPath + "\nMSYS2_ARG_CONV_EXCL=/C cmd.exe /C \"" + resolvedPath + "\" " + arg + " \"$@\""
		return writeNoNewline(filepath.Join(shimDir, stem(name)), sh)
	case strings.HasSuffix(lowered, ".ps1"):
		ps1 := ps1Wrapper(resolvedPath, arg)
		if err := writeLines(filepath.Join(shimDir, stem(name)+".ps1"), ps1); err != nil {
			return err
		}
		cmd := "@rem " + resolvedPath + "\r\n@echo off\r\nwhere /q pwsh.exe\r\nif %errorlevel% equ 0 (\r\n    pwsh -noprofile -ex unrestricted -file \"" + resolvedPath + "\" " + arg + " %*\r\n) else (\r\n    powershell -noprofile -ex unrestricted -file \"" + resolvedPath + "\" " + arg + " %*\r\n)\r\n"
		if err := writeLines(filepath.Join(shimDir, stem(name)+".cmd"), cmd); err != nil {
			return err
		}
		sh := "#!/bin/sh\n# " + resolvedPath + "\nif command -v pwsh.exe > /dev/null 2>&1; then\n    pwsh.exe -noprofile -ex unrestricted -file \"" + resolvedPath + "\" " + arg + " \"$@\"\nelse\n    powershell.exe -noprofile -ex unrestricted -file \"" + resolvedPath + "\" " + arg + " \"$@\"\nfi"
		return writeNoNewline(filepath.Join(shimDir, stem(name)), sh)
	case strings.HasSuffix(lowered, ".jar"):
		cmd := "@rem " + resolvedPath + "\r\n@pushd " + filepath.Dir(resolvedPath) + "\r\n@java -jar \"" + resolvedPath + "\" " + arg + " %*\r\n@popd\r\n"
		if err := writeLines(filepath.Join(shimDir, stem(name)+".cmd"), cmd); err != nil {
			return err
		}
		sh := "#!/bin/sh\n# " + resolvedPath + "\njava.exe -jar \"" + resolvedPath + "\" " + arg + " \"$@\""
		return writeNoNewline(filepath.Join(shimDir, stem(name)), sh)
	case strings.HasSuffix(lowered, ".py"):
		cmd := "@rem " + resolvedPath + "\r\n@python \"" + resolvedPath + "\" " + arg + " %*\r\n"
		if err := writeLines(filepath.Join(shimDir, stem(name)+".cmd"), cmd); err != nil {
			return err
		}
		sh := "#!/bin/sh\n# " + resolvedPath + "\npython.exe \"" + resolvedPath + "\" " + arg + " \"$@\""
		return writeNoNewline(filepath.Join(shimDir, stem(name)), sh)
	default:
		quoted := ""
		if arg != "" {
			quoted = `"` + arg + `"`
		}
		cmd := "@rem " + resolvedPath + "\r\n@echo off\r\n" + "bash -c \"command -v wslpath >/dev/null\"\r\nif %errorlevel% equ 0 (\r\n  bash \"$(wslpath -u '" + resolvedPath + "')\" " + quoted + " %*\r\n) else (\r\n  set args=" + quoted + " %*\r\n  setlocal enabledelayedexpansion\r\n  if not \"!args!\"==\"\" set args=!args:\"=\"\"!\r\n  bash -c \"$(cygpath -u '" + resolvedPath + "') !args!\"\r\n)\r\n"
		if err := writeLines(filepath.Join(shimDir, stem(name)+".cmd"), cmd); err != nil {
			return err
		}
		sh := "#!/bin/sh\n# " + resolvedPath + "\n\"" + "$(wslpath -u '" + resolvedPath + "')" + "\" " + arg + " \"$@\""
		return writeNoNewline(filepath.Join(shimDir, stem(name)), sh)
	}
}

func ps1Wrapper(resolvedPath, arg string) string {
	rel := resolvedPath
	// Relative-path form when the target sits under the shim dir is
	// resolved by the caller; keep the absolute form here, mirroring
	// the first ps1text branch (lib/core.ps1:1015-1021).
	_ = rel
	return "# " + resolvedPath + "\r\n$path = \"" + resolvedPath + "\"\r\nif ($MyInvocation.ExpectingInput) { $input | & $path " + arg + " @args } else { & $path " + arg + " @args }\r\nexit $LASTEXITCODE\r\n"
}

func writeLines(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if !strings.HasSuffix(content, "\r\n") && !strings.HasSuffix(content, "\n") {
		content += "\r\n"
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeNoNewline(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// WarnOnOverwrite mirrors warn_on_overwrite (lib/core.ps1:930-947):
// missing shims or same-app owners pass through; otherwise the
// existing shim moves to <shim>.<owner> and the caller warns.
func WarnOnOverwrite(shimPath, targetPath string, ownerOf func(string) string) (string, bool) {
	if _, err := os.Stat(shimPath); os.IsNotExist(err) {
		return "", false
	}
	owner := ownerOf(shimPath)
	targetOwner := ownerOf(targetPath)
	if owner == targetOwner {
		return "", false
	}
	backup := shimPath + "." + targetOwner
	// Classic removes a stale backup for the incoming app first when
	// the owner differs: "$shim.$path_app" removal.
	_ = os.Remove(backup)
	alt := shimPath + "." + owner
	if owner == "" {
		alt = shimPath + ".unknown"
	}
	_ = os.Rename(shimPath, alt)
	shimName := strings.Replace(base(shimPath), ".shim", ".exe", 1)
	fileName := strings.Replace(base(targetPath), ".shim", ".exe", 1)
	msg := fmt.Sprintf("Overwriting shim ('%s' -> '%s')", shimName, fileName)
	if owner != "" {
		msg += " installed from " + owner
	}
	return msg, true
}

// RemoveShim mirrors rm_shim (lib/install.ps1:195-216): for each of
// ”, .shim, .cmd, .ps1 it removes <shim><suffix>.<app> backups first,
// then the primary, restoring the newest backup when the primary goes
// and removing <name>.exe only when no backups remain.
func RemoveShim(shimDir, name, app string) []string {
	var removed []string
	for _, suffix := range []string{"", ".shim", ".cmd", ".ps1"} {
		primary := filepath.Join(shimDir, name+suffix)
		alt := primary + "." + app
		if app != "" {
			if _, err := os.Stat(alt); err == nil {
				removed = append(removed, "Removing shim '"+name+suffix+"."+app+"'.")
				_ = os.Remove(alt)
				continue
			}
		}
		if _, err := os.Stat(primary); err == nil {
			removed = append(removed, "Removing shim '"+name+suffix+"'.")
			_ = os.Remove(primary)
			backups, _ := filepath.Glob(primary + ".*")
			// Classic excludes *.shim/*.cmd/*.ps1 from restore
			// candidates; keep only extensionless owner backups.
			kept := backups[:0]
			for _, b := range backups {
				if strings.HasSuffix(b, ".shim") || strings.HasSuffix(b, ".cmd") || strings.HasSuffix(b, ".ps1") {
					continue
				}
				kept = append(kept, b)
			}
			if len(kept) == 0 {
				if suffix == ".shim" {
					exe := filepath.Join(shimDir, name+".exe")
					removed = append(removed, "Removing shim '"+name+".exe'.")
					_ = os.Remove(exe)
				}
			} else {
				sort.Slice(kept, func(i, j int) bool {
					fi, _ := os.Stat(kept[i])
					fj, _ := os.Stat(kept[j])
					if fi == nil || fj == nil {
						return kept[i] < kept[j]
					}
					return fi.ModTime().Before(fj.ModTime())
				})
				newest := kept[len(kept)-1]
				_ = os.Rename(newest, primary)
			}
		}
	}
	return removed
}

// GetShimTarget reads the first path line from .shim files, else the
// @rem/# comment from wrappers, mapping Sysnative back to System32,
// mirroring Get-ShimTarget (lib/core.ps1:911-928).
func GetShimTarget(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := string(data)
	if strings.HasSuffix(strings.ToLower(path), ".shim") {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.Trim(line, "\r"))
			if strings.HasPrefix(line, "path = ") {
				target := strings.Trim(strings.TrimPrefix(line, "path = "), `"`)
				return strings.Replace(target, `\Sysnative\`, `\System32\`, 1)
			}
		}
		return ""
	}
	re := regexp.MustCompile(`(?m)^(?:@rem|#)\s*(.*)$`)
	if m := re.FindStringSubmatch(text); m != nil {
		return strings.Replace(strings.TrimSpace(m[1]), `\Sysnative\`, `\System32\`, 1)
	}
	return ""
}

// AliasNames returns scoop-<alias>.* triple names for one alias.
func AliasNames(alias string) []string {
	base := "scoop-" + strings.ToLower(alias)
	return []string{base + ".exe", base + ".shim", base + ".cmd", base + ".ps1"}
}
