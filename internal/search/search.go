// Package search implements in-process manifest search, replacing
// libexec/scoop-search.ps1 and the third-party scoop-search.exe.
//
// Local scan walks bucket directories concurrently, applies a raw-bytes
// regex prefilter, and parses JSON only for candidates. Name matches
// yield empty Binaries; bin matches list matched names; shortcut-only
// matches list matched shortcut names. SQLite caching uses the same
// file and schema as classic ($scoopdir/scoop.db, table app).
// Unsupported regex syntax falls back to case-insensitive substring
// matching with a warning. No emojis.
package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/TheSlopMachine/gscoop/internal/bucket"
	"github.com/TheSlopMachine/gscoop/internal/manifest"
)

// Result is one search row: columns Name/Version/Source/Binaries.
type Result struct {
	Name     string
	Version  string
	Source   string
	Binaries string
}

// Matcher tests candidate strings.
type Matcher interface {
	MatchString(candidate string) bool
}

type regexMatcher struct {
	expr *regexp.Regexp
}

func (m regexMatcher) MatchString(candidate string) bool {
	return m.expr.MatchString(candidate)
}

type literalMatcher struct {
	needle string
}

func (m literalMatcher) MatchString(candidate string) bool {
	return strings.Contains(strings.ToLower(candidate), m.needle)
}

// CompileQuery compiles query as a case-insensitive regex, mirroring
// New-Object Regex $query, 'IgnoreCase'. Invalid patterns fall back to
// case-insensitive substring matching and report literal=true, for the
// WARN unsupported regex syntax message.
func CompileQuery(query string) (Matcher, bool) {
	expr, err := regexp.Compile("(?i)" + query)
	if err != nil {
		return literalMatcher{needle: strings.ToLower(query)}, true
	}
	return regexMatcher{expr: expr}, false
}

// UnsupportedSyntaxWarning is printed when a query falls back to
// literal matching.
const UnsupportedSyntaxWarning = "WARN  unsupported regex syntax; using literal match"

// SearchLocal scans local buckets for query, returning rows in bucket
// order then name order. An empty query matches every manifest.
func SearchLocal(scoopDir string, known []string, query string) ([]Result, bool, error) {
	matcher, literal := CompileQuery(query)
	locals := bucket.Local(scoopDir, known)
	type bucketOrder struct {
		index int
		name  string
		dir   string
	}
	buckets := []bucketOrder{}
	for i, name := range locals {
		buckets = append(buckets, bucketOrder{i, name, bucket.ManifestDir(scoopDir, name)})
	}

	var mutex sync.Mutex
	results := []Result{}
	semaphore := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, b := range buckets {
		wg.Add(1)
		go func(b bucketOrder) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			for _, row := range scanBucket(b.dir, b.name, matcher) {
				mutex.Lock()
				results = append(results, row)
				mutex.Unlock()
			}
		}(b)
	}
	wg.Wait()

	indexOf := map[string]int{}
	for _, b := range buckets {
		indexOf[b.name] = b.index
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Source != results[j].Source {
			return indexOf[results[i].Source] < indexOf[results[j].Source]
		}
		if results[i].Name != results[j].Name {
			return results[i].Name < results[j].Name
		}
		return results[i].Binaries < results[j].Binaries
	})
	return results, literal, nil
}

func scanBucket(dir, bucketName string, matcher Matcher) []Result {
	rows := []Result{}
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
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			return nil
		}
		row, ok := matchFile(path, bucketName, matcher)
		if ok {
			rows = append(rows, row)
		}
		return nil
	})
	return rows
}

func matchFile(path, bucketName string, matcher Matcher) (Result, bool) {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	content, err := os.ReadFile(path)
	if err != nil {
		return Result{}, false
	}
	if !matcher.MatchString(name) && !matcher.MatchString(string(content)) {
		return Result{}, false
	}
	var doc scanDoc
	if err := json.Unmarshal(content, &doc); err != nil {
		return Result{}, false
	}
	ver, _ := doc.Version.(string)
	if matcher.MatchString(name) {
		return Result{Name: name, Version: ver, Source: bucketName}, true
	}
	if binaries := matchBins(doc.Bin, matcher); len(binaries) > 0 {
		return Result{Name: name, Version: ver, Source: bucketName, Binaries: strings.Join(binaries, " | ")}, true
	}
	if shortcuts := matchShortcuts(doc.Shortcuts, matcher); len(shortcuts) > 0 {
		return Result{Name: name, Version: ver, Source: bucketName, Binaries: strings.Join(shortcuts, " | ")}, true
	}
	return Result{}, false
}

type scanDoc struct {
	Version   any `json:"version"`
	Bin       any `json:"bin"`
	Shortcuts any `json:"shortcuts"`
}

// matchBins ports bin_match_json: string bins match on the stem and yield
// the leaf; array triples match the exe stem or the alias.
func matchBins(raw any, matcher Matcher) []string {
	matched := []string{}
	for _, entry := range manifest.BinEntries(raw) {
		if entry.Exe == "" {
			continue
		}
		if matcher.MatchString(manifest.StripExtension(fileLeaf(entry.Exe))) {
			matched = append(matched, fileLeaf(entry.Exe))
		} else if entry.Alias != "" && matcher.MatchString(entry.Alias) {
			matched = append(matched, entry.Alias)
		}
	}
	return matched
}

// matchShortcuts collects display names of shortcuts whose target stem or
// display name matches.
func matchShortcuts(raw any, matcher Matcher) []string {
	matched := []string{}
	for _, entry := range manifest.ShortcutEntries(raw) {
		if entry.Name != "" && matcher.MatchString(entry.Name) {
			matched = append(matched, manifest.SanitaryPath(entry.Name))
		} else if entry.Target != "" && matcher.MatchString(manifest.StripExtension(fileLeaf(entry.Target))) {
			matched = append(matched, entry.Name)
		}
	}
	return matched
}

func fileLeaf(path string) string {
	leaf := path
	if index := strings.LastIndexAny(leaf, "/\\"); index >= 0 {
		leaf = leaf[index+1:]
	}
	return leaf
}
