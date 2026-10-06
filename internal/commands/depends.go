package commands

import (
	"fmt"
	"io"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/cli"
)

// RunDepends mirrors libexec/scoop-depends.ps1: dependencies in install
// order as Source/Name rows. Getopt errors are ignored (spec edge E1).
// Installation-helper inference is a no-op emitting nothing: extraction is
// built into the binary, so no helper app enters the graph.
func RunDepends(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "a:", []string{"arch="})
	if len(r.Rest) == 0 {
		Errorf(out, "<app> missing")
		printUsage(out, "depends")
		return 1
	}
	arch := env.Arch
	if req := r.Get("a") + r.Get("arch"); req != "" {
		a, err := FormatArch(req)
		if err != nil {
			fmt.Fprintf(out, "ERROR: %s\n", err.Error())
			return 1
		}
		arch = a
	}
	resolved, err := env.ResolveDepends(r.Rest[0], arch, out)
	if err != nil {
		fmt.Fprintln(out, err.Error())
		return 1
	}
	rows := make([][]string, 0, len(resolved))
	for _, dep := range resolved {
		source, name := env.depDisplay(dep)
		rows = append(rows, []string{source, name})
	}
	renderTable(out, []string{"Source", "Name"}, rows)
	return 0
}

// installationHelpers is the Phase 1C seam for Get-InstallationHelper in
// lib/depends.ps1. It returns nil: no helper app is ever inferred because
// every archive backend ships in the binary.
//
// TODO(deps): keep this seam when the mutation engine lands.
func installationHelpers(_ *Manifest, _ string) []string {
	return nil
}

// ResolveDepends implements the electricmonk DFS from Get-Dependency in
// lib/depends.ps1, with the app itself appended at the end.
func (e *Env) ResolveDepends(app, arch string, out io.Writer) ([]string, error) {
	var resolved []string
	var unresolved []string
	contains := func(list []string, s string) bool {
		for _, item := range list {
			if item == s {
				return true
			}
		}
		return false
	}
	var visit func(name string) error
	visit = func(name string) error {
		hit := e.FindManifest(name, out)
		if hit.Manifest == nil {
			if hit.Bucket != "" {
				return fmt.Errorf("Couldn't find manifest for '%s' from '%s' bucket.", hit.Name, hit.Bucket)
			}
			if hit.URL != "" {
				return fmt.Errorf("Couldn't find manifest for '%s' at '%s'.", hit.Name, hit.URL)
			}
			return fmt.Errorf("Couldn't find manifest for '%s'.", hit.Name)
		}
		unresolved = append(unresolved, name)
		deps := append(append([]string{}, installationHelpers(hit.Manifest, arch)...), hit.Manifest.DependsList()...)
		for _, dep := range uniqueStrings(deps) {
			if contains(resolved, dep) {
				continue
			}
			if contains(unresolved, dep) {
				return fmt.Errorf("Circular dependency detected: '%s' -> '%s'.", name, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		kept := unresolved[:0]
		for _, item := range unresolved {
			if item != name {
				kept = append(kept, item)
			}
		}
		unresolved = kept
		switch {
		case hit.Bucket != "":
			resolved = append(resolved, hit.Bucket+"/"+hit.Name)
		case hit.URL != "":
			resolved = append(resolved, hit.URL)
		default:
			resolved = append(resolved, hit.Name)
		}
		return nil
	}
	if err := visit(app); err != nil {
		return nil, err
	}
	return resolved, nil
}

func uniqueStrings(in []string) []string {
	var out []string
	for _, s := range in {
		dup := false
		for _, o := range out {
			if strings.EqualFold(o, s) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, s)
		}
	}
	return out
}

// depDisplay mirrors the Source/Name projection in scoop-depends.ps1: the
// manifest URL when set, else the bucket/app split of the resolved entry.
func (e *Env) depDisplay(dep string) (string, string) {
	hit := e.FindManifest(dep, nil)
	if hit.URL != "" {
		name := hit.Name
		if name == "" {
			name = appNameFromURL(dep)
		}
		return hit.URL, name
	}
	if parts := strings.SplitN(dep, "/", 2); len(parts) == 2 {
		return parts[0], parts[1]
	}
	return dep, ""
}
