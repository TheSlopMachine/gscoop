// Package bucket implements local bucket enumeration, the known-bucket
// registry, and manifest file discovery.
//
// Directory layout mirrors lib/buckets.ps1 Find-BucketDirectory: bucket
// manifests live under buckets/<name>, or under buckets/<name>/bucket
// when that subdirectory exists. Version selection delegates to
// internal/version Compare-Version. No emojis.
package bucket

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/manifest"
	"github.com/TheSlopMachine/gscoop/internal/state"
	"github.com/TheSlopMachine/gscoop/internal/version"
)

// Dir returns the buckets directory for a Scoop root.
func Dir(scoopDir string) string {
	return state.Roots{Scoop: scoopDir}.Buckets()
}

// Root returns the repository root for bucket, mirroring
// Find-BucketDirectory -Root. Empty names default to main
// (lib/buckets.ps1:18-21).
func Root(scoopDir, name string) string {
	return state.Roots{Scoop: scoopDir}.Bucket(name, true)
}

// ManifestDir returns the manifest directory for bucket, descending
// into the bucket/ subdirectory when present. It mirrors
// Find-BucketDirectory without -Root (lib/buckets.ps1:22-26).
func ManifestDir(scoopDir, name string) string {
	return state.Roots{Scoop: scoopDir}.Bucket(name, false)
}

// Local lists local buckets with known buckets first, mirroring
// lib/buckets.ps1 Get-LocalBucket.
func Local(scoopDir string, known []string) []string {
	entries, err := os.ReadDir(Dir(scoopDir))
	if err != nil {
		return []string{}
	}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	ordered := []string{}
	seen := map[string]bool{}
	for _, name := range known {
		for _, local := range names {
			if strings.EqualFold(local, name) && !seen[local] {
				ordered = append(ordered, local)
				seen[local] = true
			}
		}
	}
	for _, local := range names {
		if !seen[local] {
			ordered = append(ordered, local)
		}
	}
	return ordered
}

// ManifestPath locates sanitary(app).json under the manifest directory
// for bucket, searching recursively. It mirrors lib/manifest.ps1
// manifest_path.
func ManifestPath(scoopDir, app, bucketName string) string {
	return manifest.FindManifestFile(ManifestDir(scoopDir, bucketName), app)
}

// AppsInBucket returns the base names of all manifests under dir,
// mirroring lib/buckets.ps1 apps_in_bucket.
func AppsInBucket(dir string) []string {
	names := []string{}
	_ = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if strings.EqualFold(entry.Name(), ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			names = append(names, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		}
		return nil
	})
	sort.Strings(names)
	return names
}

// FindAppBucket returns the first manifest match across local buckets in
// order, mirroring lib/manifest.ps1 Find-AppBucket.
func FindAppBucket(scoopDir, app string, known []string) (path, bucketName string) {
	for _, name := range Local(scoopDir, known) {
		path = ManifestPath(scoopDir, app, name)
		if path == "" {
			continue
		}
		return path, name
	}
	return "", ""
}

// LatestManifest returns the manifest with the highest version across
// all local buckets holding app. Ties keep bucket order. Version
// comparison uses internal/version Compare-Version.
func LatestManifest(scoopDir, app string, known []string) (path, bucketName, manifestVersion string, err error) {
	type candidate struct {
		path, bucket, ver string
	}
	matches := []candidate{}
	for _, name := range Local(scoopDir, known) {
		candidatePath := ManifestPath(scoopDir, app, name)
		if candidatePath == "" {
			continue
		}
		ver, ok := ProbeVersion(candidatePath)
		if !ok {
			continue
		}
		matches = append(matches, candidate{candidatePath, name, ver})
	}
	if len(matches) == 0 {
		return "", "", "", fmt.Errorf("manifest %q not found in local buckets", app)
	}
	best := matches[0]
	for _, next := range matches[1:] {
		if version.Compare(best.ver, next.ver) > 0 {
			best = next
		}
	}
	return best.path, best.bucket, best.ver, nil
}

// ProbeVersion reads only the version field of a manifest file,
// delegating to state.ManifestVersion.
func ProbeVersion(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return state.ManifestVersion(data)
}

// Store adapts bucket layout to the manifest.Buckets interface.
type Store struct {
	scoopDir string
	known    []string
}

// NewStore builds a Store for scoopDir with known-bucket order.
func NewStore(scoopDir string, known []string) *Store {
	return &Store{scoopDir: scoopDir, known: known}
}

// Dir returns the manifest directory for bucket.
func (s *Store) Dir(name string) string {
	return ManifestDir(s.scoopDir, name)
}

// Local returns local bucket names with known buckets first.
func (s *Store) Local() []string {
	return Local(s.scoopDir, s.known)
}
