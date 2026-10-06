package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TheSlopMachine/gscoop/internal/state"
)

func testRoots(t *testing.T) (Options, string) {
	t.Helper()
	root := t.TempDir()
	scoop := filepath.Join(root, "scoop")
	opts := Options{
		Roots: state.Roots{
			Scoop:  scoop,
			Global: filepath.Join(root, "global"),
			Cache:  filepath.Join(scoop, "cache"),
		},
		NoJunction: true,
	}
	return opts, root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func issuesFor(opts Options, check string) []Finding {
	var out []Finding
	for _, f := range Check(opts).Findings {
		if f.Issue && (check == "" || f.Check == check) {
			out = append(out, f)
		}
	}
	return out
}

func TestCheckCleanTree(t *testing.T) {
	opts, _ := testRoots(t)
	writeFile(t, filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0", "scoop-install.json"),
		`{"architecture": "64bit", "bucket": "main"}`)
	writeFile(t, filepath.Join(opts.Roots.Scoop, "buckets", "main", "bucket", "tool.json"),
		`{"version": "1.0.0"}`)
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "tool.shim"), "path = \"x\"\r\n")
	// Point the shim target at a live file and give the trio its .exe.
	writeFile(t, filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0", "tool.exe"), "x")
	target := filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0", "tool.exe")
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "tool.shim"), "path = \""+target+"\"\r\n")
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "tool.exe"), "x")
	if err := os.MkdirAll(opts.Roots.Shims(true), 0o755); err != nil {
		t.Fatal(err)
	}
	opts.PathEnv = opts.Roots.Shims(false) + string(os.PathListSeparator) + opts.Roots.Shims(true)
	if got := Check(opts).Issues(); got != 0 {
		t.Fatalf("Issues() = %d, findings: %+v", got, Check(opts).Findings)
	}
}

func TestCheckMetadataInvalid(t *testing.T) {
	opts, _ := testRoots(t)
	writeFile(t, filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0", "scoop-install.json"), "{nope")
	if got := len(issuesFor(opts, "metadata")); got != 1 {
		t.Fatalf("metadata issues = %d", got)
	}
}

func TestCheckMetadataMissing(t *testing.T) {
	opts, _ := testRoots(t)
	if err := os.MkdirAll(filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range issuesFor(opts, "metadata") {
		if strings.Contains(f.Message, "failed install marker") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing failed install marker finding")
	}
}

func TestCheckShimTrio(t *testing.T) {
	opts, _ := testRoots(t)
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "lonely.shim"), "path = \"x\"\r\n")
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "orphan.exe"), "x")
	if got := len(issuesFor(opts, "shim")); got < 2 {
		t.Fatalf("shim issues = %d", got)
	}
}

func TestCheckShimBadTarget(t *testing.T) {
	opts, _ := testRoots(t)
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "bad.shim"),
		"path = \""+filepath.Join(opts.Roots.Scoop, "missing.exe")+"\"\r\n")
	writeFile(t, filepath.Join(opts.Roots.Shims(false), "bad.exe"), "x")
	found := false
	for _, f := range issuesFor(opts, "shim") {
		if strings.Contains(f.Message, "points at missing") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing bad target finding")
	}
}

func TestCheckJunctionMissing(t *testing.T) {
	opts, _ := testRoots(t)
	opts.NoJunction = false
	writeFile(t, filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0", "scoop-install.json"),
		`{"architecture": "64bit"}`)
	if got := len(issuesFor(opts, "junction")); got != 1 {
		t.Fatalf("junction issues = %d", got)
	}
}

func TestCheckSweepOrphan(t *testing.T) {
	opts, _ := testRoots(t)
	if err := os.MkdirAll(filepath.Join(opts.Roots.Scoop, "apps", "tool", "1.0.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(opts.Roots.Cache, "tool#1.0#abcdef.tmp"), "x")
	if got := len(issuesFor(opts, "sweep")); got != 2 {
		t.Fatalf("sweep issues = %d", got)
	}
}

func TestCheckPathMissing(t *testing.T) {
	opts, _ := testRoots(t)
	if err := os.MkdirAll(opts.Roots.Shims(false), 0o755); err != nil {
		t.Fatal(err)
	}
	opts.PathEnv = string(os.PathListSeparator)
	if got := len(issuesFor(opts, "path")); got != 1 {
		t.Fatalf("path issues = %d", got)
	}
}

func TestCheckBucketPlainDir(t *testing.T) {
	opts, _ := testRoots(t)
	if err := os.MkdirAll(filepath.Join(opts.Roots.Scoop, "buckets", "local"), 0o755); err != nil {
		t.Fatal(err)
	}
	rep := Check(opts)
	if rep.Issues() != 0 {
		t.Fatalf("Issues() = %d", rep.Issues())
	}
	found := false
	for _, f := range rep.Findings {
		if f.Check == "bucket" && strings.Contains(f.Message, "plain directory") {
			found = true
		}
	}
	if !found {
		t.Fatal("missing plain directory finding")
	}
}
