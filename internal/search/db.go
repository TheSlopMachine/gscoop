package search

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/bucket"
	"github.com/TheSlopMachine/gscoop/internal/manifest"
	"github.com/TheSlopMachine/gscoop/internal/state"
	"github.com/TheSlopMachine/gscoop/internal/version"

	_ "modernc.org/sqlite"
)

// SchemaDDL creates the app table, byte-identical to
// lib/database.ps1 Open-ScoopDB.
const SchemaDDL = `CREATE TABLE IF NOT EXISTS 'app' (
        name TEXT NOT NULL COLLATE NOCASE,
        description TEXT NOT NULL,
        version TEXT NOT NULL,
        bucket VARCHAR NOT NULL,
        manifest JSON NOT NULL,
        binary TEXT,
        shortcut TEXT,
        dependency TEXT,
        suggest TEXT,
        PRIMARY KEY (name, version, bucket)
    )`

// DBPath returns the cache file path, delegating to state.Roots.ScoopDB
// ($scoopdir/scoop.db, lib/database.ps1:87).
func DBPath(scoopDir string) string {
	return state.Roots{Scoop: scoopDir}.ScoopDB()
}

// Open creates the database file and schema on demand and returns a
// handle. It mirrors Open-ScoopDB table setup.
func Open(scoopDir string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", DBPath(scoopDir))
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(SchemaDDL); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Entry is one app row.
type Entry struct {
	Name        string
	Description string
	Version     string
	Bucket      string
	Manifest    string
	Binary      string
	Shortcut    string
	Dependency  string
	Suggest     string
}

// BuildEntry derives a row from a manifest file, mirroring Set-ScoopDB
// (lib/database.ps1:180-242) for arch. Manifests without a version are
// skipped by the caller, as in classic.
func BuildEntry(path, arch string) (Entry, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, false, err
	}
	parsed, err := manifest.Parse(data, EntryName(path), EntryBucket(path), "", path)
	if err != nil {
		return Entry{}, false, err
	}
	if parsed.Version() == "" {
		return Entry{}, false, nil
	}
	description := parsed.Description()
	bins := parsed.Bins(arch)
	shortcuts := parsed.Shortcuts(arch)
	depends := manifest.StringList(topValue(parsed, "depends"))
	return Entry{
		Name:        EntryName(path),
		Description: description,
		Version:     parsed.Version(),
		Bucket:      EntryBucket(path),
		Manifest:    string(data),
		Binary:      dbBinary(bins),
		Shortcut:    dbShortcut(shortcuts),
		Dependency:  strings.Join(depends, " | "),
		Suggest:     strings.Join(parsed.Suggest(), " | "),
	}, true, nil
}

func topValue(m *manifest.Manifest, key string) any {
	value, _ := m.Root.Get(key)
	return value
}

// EntryName derives the app name from the file base name
// (lib/database.ps1:202).
func EntryName(path string) string {
	base := filepath.Base(path)
	if strings.EqualFold(filepath.Ext(base), ".json") {
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	return base
}

// EntryBucket derives the bucket from the first path segment after a
// buckets directory (lib/database.ps1:204). Paths outside a buckets
// tree yield "".
func EntryBucket(path string) string {
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' })
	for i, segment := range segments {
		if strings.EqualFold(segment, "buckets") && i+1 < len(segments) {
			return segments[i+1]
		}
	}
	return ""
}

// dbBinary ports the binary column derivation: triples contribute
// alias plus the exe extension, plain strings pass through, then each
// item is reduced to its leaf without a known executable extension.
func dbBinary(bins []manifest.BinEntry) string {
	items := make([]string, 0, len(bins))
	for _, bin := range bins {
		candidate := bin.Exe
		if bin.Alias != "" {
			candidate = bin.Alias + "." + extName(bin.Exe)
		}
		items = append(items, stripKnownExt(fileLeaf(candidate)))
	}
	return strings.Join(items, " | ")
}

// dbShortcut ports the shortcut column derivation: display names
// reduced to their leaf.
func dbShortcut(shortcuts []manifest.Shortcut) string {
	items := make([]string, 0, len(shortcuts))
	for _, shortcut := range shortcuts {
		if shortcut.Name == "" {
			continue
		}
		items = append(items, fileLeaf(shortcut.Name))
	}
	return strings.Join(items, " | ")
}

var knownExts = []string{".exe", ".bat", ".cmd", ".ps1", ".jar", ".py"}

func stripKnownExt(leaf string) string {
	lower := strings.ToLower(leaf)
	for _, ext := range knownExts {
		if strings.HasSuffix(lower, ext) {
			return leaf[:len(leaf)-len(ext)]
		}
	}
	return leaf
}

func extName(path string) string {
	leaf := fileLeaf(path)
	if index := strings.LastIndex(leaf, "."); index >= 0 {
		return leaf[index+1:]
	}
	return ""
}

// Upsert writes entries with INSERT OR REPLACE inside one transaction,
// mirroring Set-ScoopDBItem (lib/database.ps1:124-162).
func Upsert(db *sql.DB, entries []Entry) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	statement, err := tx.Prepare(`INSERT OR REPLACE INTO app
        (name, description, version, bucket, manifest, binary, shortcut, dependency, suggest)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer statement.Close()
	for _, entry := range entries {
		if _, err := statement.Exec(entry.Name, entry.Description, entry.Version,
			entry.Bucket, entry.Manifest, entry.Binary, entry.Shortcut,
			entry.Dependency, entry.Suggest); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// RefreshPaths rebuilds rows for manifest files, mirroring
// Set-ScoopDB -Path.
func RefreshPaths(db *sql.DB, paths []string, arch string) error {
	entries := []Entry{}
	for _, path := range paths {
		entry, ok, err := BuildEntry(path, arch)
		if err != nil {
			continue
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return nil
	}
	return Upsert(db, entries)
}

// RefreshAll rebuilds rows for every local bucket manifest, mirroring
// Set-ScoopDB without -Path.
func RefreshAll(db *sql.DB, scoopDir string, known []string, arch string) error {
	paths := []string{}
	for _, name := range bucket.Local(scoopDir, known) {
		dir := bucket.ManifestDir(scoopDir, name)
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
				paths = append(paths, path)
			}
			return nil
		})
	}
	return RefreshPaths(db, paths, arch)
}

// Find matches pattern against columns with LIKE semantics and keeps the
// latest row per (name, bucket) via Compare-Version. It mirrors
// Find-ScoopDBItem (lib/database.ps1:262-295). Allowed columns: name,
// binary, shortcut.
func Find(db *sql.DB, pattern string, from []string) ([]Result, error) {
	for _, column := range from {
		switch column {
		case "name", "binary", "shortcut":
		default:
			return nil, fmt.Errorf("unknown search column %q", column)
		}
	}
	like := "%" + pattern + "%"
	if pattern == "" {
		like = "%"
	}
	conditions := make([]string, 0, len(from))
	for _, column := range from {
		conditions = append(conditions, column+" LIKE ?")
	}
	args := make([]any, 0, len(from))
	for range from {
		args = append(args, like)
	}
	query := "SELECT name, version, bucket, binary FROM app WHERE " + strings.Join(conditions, " OR ")
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type key struct {
		name, bucket string
	}
	latest := map[key]Entry{}
	order := []key{}
	for rows.Next() {
		var entry Entry
		var binary sql.NullString
		if err := rows.Scan(&entry.Name, &entry.Version, &entry.Bucket, &binary); err != nil {
			return nil, err
		}
		entry.Binary = binary.String
		group := key{strings.ToLower(entry.Name), strings.ToLower(entry.Bucket)}
		current, ok := latest[group]
		if !ok {
			order = append(order, group)
			latest[group] = entry
			continue
		}
		if version.Compare(current.Version, entry.Version) > 0 {
			latest[group] = entry
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(latest))
	for _, group := range order {
		entry := latest[group]
		results = append(results, Result{Name: entry.Name, Version: entry.Version, Source: entry.Bucket, Binaries: entry.Binary})
	}
	return results, nil
}

// GetRow returns rows for name and bucket, optionally pinned to version.
// Without a version it returns the single latest row. It mirrors
// Get-ScoopDBItem (lib/database.ps1:317-364).
func GetRow(db *sql.DB, name, bucketName, manifestVersion string) ([]Entry, error) {
	query := "SELECT name, description, version, bucket, manifest, binary, shortcut, dependency, suggest FROM app WHERE name = ? AND bucket = ?"
	args := []any{name, bucketName}
	if manifestVersion != "" {
		query += " AND version = ?"
		args = append(args, manifestVersion)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var entry Entry
		var binary, shortcut, dependency, suggest sql.NullString
		if err := rows.Scan(&entry.Name, &entry.Description, &entry.Version, &entry.Bucket,
			&entry.Manifest, &binary, &shortcut, &dependency, &suggest); err != nil {
			return nil, err
		}
		entry.Binary = binary.String
		entry.Shortcut = shortcut.String
		entry.Dependency = dependency.String
		entry.Suggest = suggest.String
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if manifestVersion != "" || len(entries) <= 1 {
		return entries, nil
	}
	best := entries[0]
	for _, next := range entries[1:] {
		if version.Compare(best.Version, next.Version) > 0 {
			best = next
		}
	}
	return []Entry{best}, nil
}

// Remove deletes rows for bucket, optionally limited to name. It mirrors
// Remove-ScoopDBItem (lib/database.ps1:468-507).
func Remove(db *sql.DB, bucketName, name string) error {
	query := "DELETE FROM app WHERE bucket = ?"
	args := []any{bucketName}
	if name != "" {
		query += " AND name = ?"
		args = append(args, name)
	}
	_, err := db.Exec(query, args...)
	return err
}
