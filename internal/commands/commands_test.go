package commands

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var updateGoldens = flag.Bool("update", false, "rewrite testdata/commands goldens")

var datePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func installJSON(bucket string, hold bool) string {
	if hold {
		return `{"architecture": "64bit", "bucket": "` + bucket + `", "hold": true}`
	}
	return `{"architecture": "64bit", "bucket": "` + bucket + `"}`
}

// fixtureEnv builds an isolated Scoop root: bucket manifests, installed
// apps in both scopes, cache entries, shims, and a config file.
func fixtureEnv(t *testing.T) *Env {
	t.Helper()
	root := t.TempDir()
	scoop := filepath.Join(root, "scoop")
	global := filepath.Join(root, "global")
	env := &Env{
		ScoopDir:   scoop,
		GlobalDir:  global,
		CacheDir:   filepath.Join(scoop, "cache"),
		ConfigPath: filepath.Join(root, "config.json"),
		Arch:       "64bit",
	}
	bucket := filepath.Join(scoop, "buckets", "main", "bucket")
	writeFixture(t, filepath.Join(bucket, "git.json"), `{
    "version": "2.0.0",
    "description": "Distributed version control",
    "homepage": "https://git-scm.com/",
    "license": "GPL-2.0-only",
    "depends": "dep",
    "bin": ["bin/git.exe", ["bin/gitk.exe", "gitk"]],
    "shortcuts": [["bin/git.exe", "Git"]],
    "suggest": {"extras": "helper"},
    "notes": "See $dir for details.",
    "env_set": {"GIT_DIR": "$dir\\.git"},
    "env_add_path": ["bin", "."]
}`)
	writeFixture(t, filepath.Join(bucket, "dep.json"), `{
    "version": "1.0.0",
    "description": "Support library",
    "homepage": "https://example.com/dep",
    "license": {"identifier": "MIT", "url": "https://example.com/mit"}
}`)
	writeFixture(t, filepath.Join(bucket, "tool.json"), `{
    "version": "3.1.0",
    "description": "Held tool"
}`)
	writeFixture(t, filepath.Join(bucket, "lonely.json"), `{
    "version": "1.0.0",
    "description": "Misses a dependency",
    "depends": "ghost"
}`)
	writeFixture(t, filepath.Join(bucket, "gtool.json"), `{
    "version": "1.0.0",
    "description": "Global tool"
}`)
	writeFixture(t, filepath.Join(scoop, "buckets", "main", "deprecated", "leg.json"), `{
    "version": "1.0.0",
    "description": "Legacy app"
}`)
	install := func(app, version, scope string, hold bool) {
		base := scoop
		if scope == "global" {
			base = global
		}
		dir := filepath.Join(base, "apps", app, version)
		manifestVersion := version
		if app == "git" {
			manifestVersion = "1.0.0"
		}
		writeFixture(t, filepath.Join(dir, "scoop-manifest.json"),
			`{"version": "`+manifestVersion+`", "description": "installed"}`)
		writeFixture(t, filepath.Join(dir, "scoop-install.json"), installJSON("main", hold))
		writeFixture(t, filepath.Join(base, "apps", app, "current", "scoop-manifest.json"),
			`{"version": "`+manifestVersion+`"}`)
	}
	install("git", "1.0.0", "user", false)
	install("dep", "1.0.0", "user", false)
	install("tool", "3.1.0", "user", true)
	install("oldapp", "1.0.0", "user", false)
	install("lonely", "1.0.0", "user", false)
	install("leg", "1.0.0", "user", false)
	install("gtool", "1.0.0", "global", false)
	writeFixture(t, filepath.Join(scoop, "apps", "git", "1.0.0", "bin", "git.exe"), "git-binary")
	writeFixture(t, filepath.Join(scoop, "apps", "broken", "1.0.0", "leftover.txt"), "partial")
	writeFixture(t, filepath.Join(scoop, "cache", "git#1.0.0#abc1234"), "0123456789")
	writeFixture(t, filepath.Join(scoop, "cache", "dep#1.0.0#def5678"), "hello")
	writeFixture(t, filepath.Join(scoop, "shims", "git.shim"),
		"path = \""+filepath.Join(scoop, "apps", "git", "1.0.0", "bin", "git.exe")+"\"\n")
	writeFixture(t, filepath.Join(scoop, "shims", "broken.shim"), "path = \""+filepath.Join(scoop, "apps", "broken", "missing.exe")+"\"\n")
	writeFixture(t, filepath.Join(root, "config.json"), `{
    "debug": true,
    "root_path": "C:\\scoop",
    "global_path": "C:\\ProgramData\\scoop",
    "cache_path": "C:\\scoop\\cache",
    "alias": {"rm": "scoop-uninstall"},
    "last_update": "2024-01-01T00:00:00",
    "default_architecture": "64bit",
    "proxy": "none"
}`)
	return env
}

func runCommand(env *Env, name string, args []string) (string, int) {
	var buf bytes.Buffer
	code, handled := Run(env, &buf, name, args)
	if !handled {
		return buf.String(), 99
	}
	return buf.String(), code
}

// normalize scrubs machine-specific output: separators, the fixture root
// (absolute and home-relative friendly forms), and timestamps.
func normalize(t *testing.T, env *Env, s string) string {
	t.Helper()
	// JSON output escapes backslashes; fold pairs first so JSON paths
	// normalize like raw paths.
	s = strings.ReplaceAll(s, `\\`, "/")
	s = strings.ReplaceAll(s, "\\", "/")
	root := strings.ReplaceAll(env.ScoopDir, "\\", "/")
	root = strings.TrimSuffix(root, "/scoop")
	s = strings.ReplaceAll(s, root, "<ROOT>")
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		home = strings.ReplaceAll(home, "\\", "/")
		if rel, err := filepath.Rel(home, root); err == nil && !strings.HasPrefix(rel, "..") {
			rel = strings.ReplaceAll(rel, "\\", "/")
			s = strings.ReplaceAll(s, "~"+"/"+rel, "<ROOT>")
		}
	}
	s = datePattern.ReplaceAllString(s, "<WHEN>")
	return s
}

func goldenPath(name string) string {
	return filepath.Join("..", "..", "testdata", "commands", name+".txt")
}

func assertGolden(t *testing.T, env *Env, golden string, out string, code, wantCode int) {
	t.Helper()
	if code != wantCode {
		t.Fatalf("exit code = %d, want %d\noutput:\n%s", code, wantCode, out)
	}
	actual := normalize(t, env, out)
	if *updateGoldens {
		if err := os.MkdirAll(filepath.Dir(goldenPath(golden)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath(golden), []byte(actual+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", golden)
		return
	}
	raw, err := os.ReadFile(goldenPath(golden))
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update)", golden, err)
	}
	if string(raw) != actual+"\n" {
		t.Fatalf("golden %s mismatch.\nGot:\n%s\nWant:\n%s", golden, actual, strings.TrimSuffix(string(raw), "\n"))
	}
}

func TestGoldens(t *testing.T) {
	cases := []struct {
		name     string
		cmd      string
		args     []string
		golden   string
		wantCode int
	}{
		{"list", "list", nil, "list", 0},
		{"list query", "list", []string{"git"}, "list-query", 0},
		{"info", "info", []string{"git"}, "info", 0},
		{"info verbose", "info", []string{"-v", "git"}, "info-verbose", 0},
		{"cat", "cat", []string{"git"}, "cat", 0},
		{"which", "which", []string{"git"}, "which", 0},
		{"prefix", "prefix", []string{"git"}, "prefix", 0},
		{"depends", "depends", []string{"git"}, "depends", 0},
		{"status", "status", nil, "status", 0},
		{"export", "export", nil, "export", 0},
		{"export config", "export", []string{"--config"}, "export-config", 0},
		{"cache show", "cache", []string{"show"}, "cache-show", 0},
		{"checkup", "checkup", nil, "checkup", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := fixtureEnv(t)
			out, code := runCommand(env, tc.cmd, tc.args)
			assertGolden(t, env, tc.golden, out, code, tc.wantCode)
		})
	}
}

func TestErrorPaths(t *testing.T) {
	env := fixtureEnv(t)
	cases := []struct {
		name     string
		cmd      string
		args     []string
		contains []string
		wantCode int
	}{
		{"cat missing arg", "cat", nil, []string{"<app> missing", "Usage:"}, 1},
		{"cat unknown app", "cat", []string{"nosuchapp"}, []string{"Couldn't find manifest for 'nosuchapp'."}, 1},
		{"info missing arg", "info", nil, []string{"Usage:"}, 1},
		{"info bad flag", "info", []string{"--nope"}, []string{"ERROR scoop info:"}, 1},
		{"which missing arg", "which", nil, []string{"<command> missing"}, 1},
		{"prefix missing arg", "prefix", nil, []string{"Usage:"}, 1},
		{"prefix unknown app", "prefix", []string{"nosuchapp"}, []string{"Could not find app path"}, 1},
		{"depends missing arg", "depends", nil, []string{"<app> missing"}, 1},
		{"depends bad arch", "depends", []string{"-a", "bogus", "git"}, []string{"ERROR:"}, 1},
		{"depends missing dep", "depends", []string{"lonely"}, []string{"Couldn't find manifest for 'ghost'."}, 1},
		{"which unknown", "which", []string{"nosuchapp"}, []string{"not found"}, 2},
		{"list empty", "list", nil, []string{"aren't any apps"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			use := env
			if tc.name == "list empty" {
				use = &Env{ScoopDir: t.TempDir(), GlobalDir: t.TempDir(), CacheDir: t.TempDir(), Arch: "64bit"}
			}
			out, code := runCommand(use, tc.cmd, tc.args)
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\noutput:\n%s", code, tc.wantCode, out)
			}
			for _, want := range tc.contains {
				if !strings.Contains(out, want) {
					t.Fatalf("output missing %q\noutput:\n%s", want, out)
				}
			}
		})
	}
}

func TestExportSingleAppCollapses(t *testing.T) {
	root := t.TempDir()
	scoop := filepath.Join(root, "scoop")
	env := &Env{ScoopDir: scoop, GlobalDir: filepath.Join(root, "global"), CacheDir: filepath.Join(scoop, "cache"), Arch: "64bit"}
	writeFixture(t, filepath.Join(scoop, "buckets", "main", "bucket", "solo.json"), `{"version": "1.0.0"}`)
	writeFixture(t, filepath.Join(scoop, "apps", "solo", "1.0.0", "scoop-manifest.json"), `{"version": "1.0.0"}`)
	writeFixture(t, filepath.Join(scoop, "apps", "solo", "1.0.0", "scoop-install.json"), installJSON("main", false))
	writeFixture(t, filepath.Join(scoop, "apps", "solo", "current", "scoop-manifest.json"), `{"version": "1.0.0"}`)
	out, code := runCommand(env, "export", nil)
	if code != 0 {
		t.Fatalf("exit code = %d\noutput:\n%s", code, out)
	}
	if !strings.Contains(out, `"apps": {`) {
		t.Fatalf("single app should collapse to an object, got:\n%s", out)
	}
}

func TestCheckupDanglingJunction(t *testing.T) {
	env := fixtureEnv(t)
	link := filepath.Join(env.ScoopDir, "apps", "dep", "current")
	os.RemoveAll(link)
	target := filepath.Join(env.ScoopDir, "apps", "dep", "gone")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var buf bytes.Buffer
	if code := RunCheckup(env, &buf, nil); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(buf.String(), "points at missing") {
		t.Fatalf("expected dangling junction warning, got:\n%s", buf.String())
	}
}
