// Package deps resolves install order with the electricmonk DFS from
// Get-Dependency (lib/depends.ps1:1-72) and keeps the
// Get-InstallationHelper inference as a no-op emitting nothing:
// extraction ships in the binary, so no helper app enters the graph.
// The resolver keeps the code path for manifest semantics. No emojis.
package deps

import (
	"fmt"
	"strings"
)

// Fetcher loads dependency names and helper markers for one app.
type Fetcher interface {
	// Depends returns manifest.depends for app at arch.
	Depends(app, arch string) ([]string, error)
	// Helpers returns Get-InstallationHelper results; the production
	// implementation returns nil.
	Helpers(app, arch string) []string
	// Exists reports whether a manifest exists for app.
	Exists(app string) bool
	// BucketOf returns the bucket owning app, or "".
	BucketOf(app string) string
}

// InstallationHelpers is the Get-InstallationHelper seam: it always
// returns nil because 7zip, lessmsi, innounp, and dark ship in the
// binary (plan section 5.1).
func InstallationHelpers(_ []string, _ []string, _ string) []string {
	return nil
}

// Resolve returns apps in dependency order with the requested app
// last, mirroring Get-Dependency. Names keep bucket/ prefixes when
// the fetcher reports a bucket. Circular chains abort.
func Resolve(f Fetcher, app, arch string) ([]string, error) {
	var resolved []string
	var unresolved []string
	contains := func(list []string, s string) bool {
		for _, item := range list {
			if strings.EqualFold(item, s) {
				return true
			}
		}
		return false
	}
	var visit func(name string) error
	visit = func(name string) error {
		if !f.Exists(name) {
			return fmt.Errorf("Couldn't find manifest for '%s'.", name)
		}
		unresolved = append(unresolved, name)
		deps, err := f.Depends(name, arch)
		if err != nil {
			return err
		}
		helpers := f.Helpers(name, arch)
		combined := append(append([]string{}, helpers...), deps...)
		unique := uniqueFold(combined)
		for _, dep := range unique {
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
			if !strings.EqualFold(item, name) {
				kept = append(kept, item)
			}
		}
		unresolved = kept
		if bucket := f.BucketOf(name); bucket != "" {
			resolved = append(resolved, bucket+"/"+name)
		} else {
			resolved = append(resolved, name)
		}
		return nil
	}
	if err := visit(app); err != nil {
		return nil, err
	}
	return resolved, nil
}

// ResolveAll resolves several roots in order, deduplicating with
// case-insensitive match while preserving first-seen order.
func ResolveAll(f Fetcher, apps []string, arch string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, app := range apps {
		ordered, err := Resolve(f, app, arch)
		if err != nil {
			return nil, err
		}
		for _, item := range ordered {
			key := strings.ToLower(item)
			if !seen[key] {
				seen[key] = true
				out = append(out, item)
			}
		}
	}
	return out, nil
}

func uniqueFold(in []string) []string {
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
