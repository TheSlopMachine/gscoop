package bucket

import (
	"fmt"
	"os"

	"github.com/TheSlopMachine/gscoop/internal/manifest"
)

// Registry is the known-bucket registry from buckets.json with file order
// preserved (lib/buckets.ps1 known_bucket_repos).
type Registry struct {
	// Names holds bucket names in registry order.
	Names []string
	// Repos maps bucket name to repository URL.
	Repos map[string]string
}

// LoadRegistry reads buckets.json at path.
func LoadRegistry(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseRegistry(data)
}

// ParseRegistry decodes buckets.json bytes, preserving name order.
func ParseRegistry(data []byte) (*Registry, error) {
	obj, err := manifest.ParseBytes(data)
	if err != nil {
		return nil, err
	}
	registry := &Registry{Repos: make(map[string]string, len(obj.Keys))}
	for _, name := range obj.Keys {
		value, _ := obj.Get(name)
		repo, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("bucket %q repository must be a string", name)
		}
		registry.Names = append(registry.Names, name)
		registry.Repos[name] = repo
	}
	return registry, nil
}

// Repo returns the repository URL for a known bucket, or "".
func (r *Registry) Repo(name string) string {
	if r == nil {
		return ""
	}
	return r.Repos[name]
}

// Known reports whether name is a known bucket.
func (r *Registry) Known(name string) bool {
	if r == nil {
		return false
	}
	_, ok := r.Repos[name]
	return ok
}
