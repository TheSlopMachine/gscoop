package commands

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/TheSlopMachine/gscoop/internal/gitengine"
	"github.com/TheSlopMachine/gscoop/internal/install"
	"github.com/TheSlopMachine/gscoop/internal/junction"
)

// stubDownloader pretends to fetch artifacts without network.
type stubDownloader struct{}

func (stubDownloader) Download(ctx context.Context, op install.Op, dir string) ([]string, error) {
	return nil, nil
}

// stubEngine answers git calls without network or repositories.
type stubEngine struct {
	pulled []string
	cloned []string
}

func (s *stubEngine) LsRemote(url string) (string, error) { return "abc123", nil }

func (s *stubEngine) Clone(url, dir string, opts gitengine.CloneOptions) error {
	s.cloned = append(s.cloned, dir)
	return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
}

func (s *stubEngine) Pull(repo string, opts gitengine.PullOptions) error {
	s.pulled = append(s.pulled, repo)
	return nil
}

func (s *stubEngine) Fetch(repo, refspec string, force bool) error { return nil }

func (s *stubEngine) CheckoutCreate(repo, branch, track string) error { return nil }

func (s *stubEngine) ResetHard(repo, rev string) error { return nil }

func (s *stubEngine) Head(repo string) (string, error) { return "head", nil }

func (s *stubEngine) ConfigGet(repo, key string) (string, error) { return "", nil }

func (s *stubEngine) ConfigSet(repo, key, value string) error { return nil }

func (s *stubEngine) LogSince(repo, rev string, pathFilter string, invertGrep *regexp.Regexp, limit int) ([]gitengine.LogEntry, error) {
	return nil, nil
}

func (s *stubEngine) DiffNameStatus(repo, a, b string) ([]gitengine.DiffEntry, error) {
	return nil, nil
}

func (s *stubEngine) ShowFile(repo, rev, path string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func (s *stubEngine) Status(repo string) (gitengine.FileStatus, error) {
	return gitengine.FileStatus{}, nil
}

func (s *stubEngine) StashLike(repo string) (gitengine.StashResult, error) {
	return gitengine.StashResult{}, nil
}

func withStubs(t *testing.T) {
	t.Helper()
	prevAPI := gscoopReleaseAPI
	prevVT := vtRequest
	prevOpen := openBrowser
	prevEngine := newUpdateEngine
	prevDownloader, prevExtractor := mutateDownloader, mutateExtractor
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name": "v0.0.0", "prerelease": false, "assets": []}]`))
	}))
	t.Cleanup(func() { server.Close() })
	gscoopReleaseAPI = server.URL
	vtRequest = func(method, rawURL, apiKey string, body io.Reader) (int, []byte, error) {
		return 404, nil, nil
	}
	openBrowser = func(url string) error { return nil }
	newUpdateEngine = func(useExternal bool) gitengine.GitEngine { return &stubEngine{} }
	mutateDownloader = stubDownloader{}
	mutateExtractor = nil
	t.Cleanup(func() {
		gscoopReleaseAPI = prevAPI
		vtRequest = prevVT
		openBrowser = prevOpen
		newUpdateEngine = prevEngine
		mutateDownloader = prevDownloader
		mutateExtractor = prevExtractor
	})
}

// updateEnv builds an isolated root with one updatable app (plain
// 1.0.0 installed, 2.0.0 in the bucket) and one held app.
func updateEnv(t *testing.T) *Env {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() { _ = junction.RemoveAll(root) })
	scoop := filepath.Join(root, "scoop")
	env := &Env{
		ScoopDir:   scoop,
		GlobalDir:  filepath.Join(root, "global"),
		CacheDir:   filepath.Join(scoop, "cache"),
		ConfigPath: filepath.Join(root, "config.json"),
		Arch:       "64bit",
		NoJunction: true,
	}
	writeFixture(t, filepath.Join(scoop, "buckets", "main", "bucket", "plain.json"), `{
    "version": "2.0.0",
    "description": "Plain tool",
    "homepage": "https://example.com/plain",
    "license": "MIT"
}`)
	writeFixture(t, filepath.Join(scoop, "buckets", "main", "bucket", "held.json"), `{
    "version": "2.0.0",
    "description": "Held tool",
    "homepage": "https://example.com/held",
    "license": "MIT"
}`)
	writeFixture(t, filepath.Join(scoop, "apps", "plain", "1.0.0", "scoop-manifest.json"), `{"version": "1.0.0", "description": "old"}`)
	writeFixture(t, filepath.Join(scoop, "apps", "plain", "1.0.0", "scoop-install.json"), `{"architecture": "64bit", "bucket": "main"}`)
	writeFixture(t, filepath.Join(scoop, "apps", "held", "1.0.0", "scoop-manifest.json"), `{"version": "1.0.0", "description": "old"}`)
	writeFixture(t, filepath.Join(scoop, "apps", "held", "1.0.0", "scoop-install.json"), `{"architecture": "64bit", "bucket": "main", "hold": true}`)
	writeFixture(t, filepath.Join(scoop, "cache", "plain#1.0.0#abc1234.zip"), "old-cache")
	writeFixture(t, filepath.Join(scoop, "cache", "plain#1.0.0#abc1234.zip.download"), "partial")
	// A .git marker keeps the bucket sync on the pull path instead of
	// the remove plus re-add conversion.
	writeFixture(t, filepath.Join(scoop, "buckets", "main", ".git", "HEAD"), "ref: refs/heads/master")
	return env
}

func runPhase3A(env *Env, name string, args []string) (string, int) {
	var buf bytes.Buffer
	code, handled := RunPhase3A(env, &buf, name, args)
	if !handled {
		return buf.String(), 99
	}
	return buf.String(), code
}

func TestOwnsPhase3A(t *testing.T) {
	for _, name := range []string{"update", "cleanup", "alias", "home", "virustotal", "shim"} {
		if !OwnsPhase3A(name) {
			t.Errorf("OwnsPhase3A(%q) = false", name)
		}
		if _, ok := RunPhase3A(&Env{}, io.Discard, name, []string{"--help"}); !ok {
			t.Errorf("RunPhase3A(%q) not handled", name)
		}
	}
	if OwnsPhase3A("install") {
		t.Error("OwnsPhase3A(install) must be false")
	}
	if _, ok := RunPhase3A(&Env{}, io.Discard, "install", nil); ok {
		t.Error("RunPhase3A(install) must not handle")
	}
}

func TestRunUpdateFlags(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	if _, code := runPhase3A(env, "update", []string{"--bogus"}); code != 1 {
		t.Errorf("bad flag exit = %d, want 1", code)
	}
	if out, code := runPhase3A(env, "update", []string{"-g"}); code != 1 || !strings.Contains(out, "--global is invalid") {
		t.Errorf("global-only exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "update", []string{"-k"}); code != 1 || !strings.Contains(out, "--no-cache is invalid") {
		t.Errorf("no-cache-only exit = %d, out = %q", code, out)
	}
}

func TestRunUpdateNoArgs(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	out, code := runPhase3A(env, "update", nil)
	if code != 0 {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	for _, want := range []string{"Updating Scoop...", "Updating Buckets...", "Scoop was updated successfully!"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunUpdateHoldSkipsCore(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	writeFixture(t, env.ConfigPath, `{"hold_update_until": "2999-01-01T00:00:00Z"}`)
	out, code := runPhase3A(env, "update", nil)
	if code != 0 {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	if strings.Contains(out, "Updating Scoop...") {
		t.Errorf("held core must skip self-update, got:\n%s", out)
	}
	if !strings.Contains(out, "Updating Buckets...") {
		t.Errorf("buckets still sync under hold, got:\n%s", out)
	}
}

func TestRunUpdateApp(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	out, code := runPhase3A(env, "update", []string{"plain"})
	if code != 0 {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	if !strings.Contains(out, "Updating 'plain' (1.0.0 -> 2.0.0)") {
		t.Errorf("output missing update line:\n%s", out)
	}
	for _, v := range []string{"1.0.0", "2.0.0"} {
		if _, err := os.Stat(filepath.Join(env.ScoopDir, "apps", "plain", v, "scoop-install.json")); err != nil {
			t.Errorf("version %s metadata missing: %v", v, err)
		}
	}
	if got := env.CurrentVersion("plain", false); got != "2.0.0" {
		t.Errorf("current = %q, want 2.0.0", got)
	}
}

func TestRunUpdateHeldApp(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	out, code := runPhase3A(env, "update", []string{"held"})
	if code != 0 {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	if !strings.Contains(out, "'held' is held to version 1.0.0") {
		t.Errorf("output missing hold notice:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "apps", "held", "2.0.0")); !os.IsNotExist(err) {
		t.Error("held app must not gain a new version")
	}
}

func TestRunUpdateUnknownApp(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	out, code := runPhase3A(env, "update", []string{"nosuchapp"})
	if code != 1 {
		t.Errorf("exit = %d, want 1, out = %q", code, out)
	}
	if !strings.Contains(out, "'nosuchapp' isn't installed.") {
		t.Errorf("output missing not-installed notice:\n%s", out)
	}
}

func TestRunCleanup(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	// Seed a second version dir for plain plus an outdated cache entry.
	writeFixture(t, filepath.Join(env.ScoopDir, "apps", "plain", "2.0.0", "scoop-manifest.json"), `{"version": "2.0.0"}`)
	writeFixture(t, filepath.Join(env.ScoopDir, "apps", "plain", "2.0.0", "scoop-install.json"), `{"architecture": "64bit", "bucket": "main"}`)
	writeFixture(t, filepath.Join(env.ScoopDir, "cache", "plain#2.0.0#def5678.zip"), "new-cache")
	out, code := runPhase3A(env, "cleanup", []string{"plain", "-k"})
	if code != 0 {
		t.Fatalf("exit = %d, out = %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "apps", "plain", "1.0.0")); !os.IsNotExist(err) {
		t.Error("old version must be removed")
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "cache", "plain#1.0.0#abc1234.zip")); !os.IsNotExist(err) {
		t.Error("outdated cache must be pruned")
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "cache", "plain#2.0.0#def5678.zip")); err != nil {
		t.Error("current cache must survive")
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "cache", "plain#1.0.0#abc1234.zip.download")); !os.IsNotExist(err) {
		t.Error("in-flight downloads must be pruned")
	}
	_ = out
}

func TestRunCleanupErrors(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	if out, code := runPhase3A(env, "cleanup", nil); code != 1 || !strings.Contains(out, "<app> missing") {
		t.Errorf("missing app exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "cleanup", []string{"--bogus", "plain"}); code != 1 || !strings.Contains(out, "scoop cleanup:") {
		t.Errorf("bad flag exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "cleanup", []string{"nosuchapp"}); code != 1 {
		t.Errorf("unknown app exit = %d, out = %q", code, out)
	}
}

func TestRunAliasRoundTrip(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	if _, code := runPhase3A(env, "alias", []string{"add", "upgrade", "scoop update *", "Update all apps"}); code != 0 {
		t.Fatal("alias add failed")
	}
	script, err := os.ReadFile(filepath.Join(env.ShimDir(false), "scoop-upgrade.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(script) != aliasScriptForTest("Update all apps", "scoop update *") {
		t.Errorf("alias script = %q", script)
	}
	if out, code := runPhase3A(env, "alias", []string{"list"}); code != 0 || !strings.Contains(out, "upgrade") || !strings.Contains(out, "scoop update *") {
		t.Errorf("alias list exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "alias", []string{"list", "-v"}); code != 0 || !strings.Contains(out, "Update all apps") {
		t.Errorf("alias list verbose exit = %d, out = %q", code, out)
	}
	if _, code := runPhase3A(env, "alias", []string{"add", "upgrade", "other"}); code != 1 {
		t.Error("duplicate alias must fail")
	}
	if _, code := runPhase3A(env, "alias", []string{"rm", "upgrade"}); code != 0 {
		t.Error("alias rm failed")
	}
	if out, code := runPhase3A(env, "alias", []string{"list"}); code != 0 || !strings.Contains(out, "No alias found.") {
		t.Errorf("empty list exit = %d, out = %q", code, out)
	}
	if _, code := runPhase3A(env, "alias", []string{"rm", "upgrade"}); code != 1 {
		t.Error("removing a missing alias must fail")
	}
	if _, code := runPhase3A(env, "alias", []string{"frobnicate"}); code != 1 {
		t.Error("unknown subcommand must fail")
	}
}

func TestRunAliasListFixture(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "update", "alias-upgrade.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(env.ShimDir(false), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ShimDir(false), "scoop-upgrade.ps1"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, env.ConfigPath, `{"alias": {"upgrade": "scoop-upgrade"}}`)
	out, code := runPhase3A(env, "alias", []string{"list"})
	if code != 0 || !strings.Contains(out, "upgrade") || !strings.Contains(out, "scoop update *") {
		t.Errorf("fixture list exit = %d, out = %q", code, out)
	}
}

func TestRunHome(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	if _, code := runPhase3A(env, "home", nil); code != 1 {
		t.Error("missing app must fail")
	}
	if out, code := runPhase3A(env, "home", []string{"nosuchapp"}); code != 1 || !strings.Contains(out, "Could not find manifest") {
		t.Errorf("unknown app exit = %d, out = %q", code, out)
	}
	writeFixture(t, filepath.Join(env.ScoopDir, "buckets", "main", "bucket", "bare.json"), `{"version": "1.0.0"}`)
	if out, code := runPhase3A(env, "home", []string{"bare"}); code != 1 || !strings.Contains(out, "Could not find homepage") {
		t.Errorf("bare manifest exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "home", []string{"plain"}); code != 0 || strings.Contains(out, "https://example.com/plain") {
		t.Errorf("home exit = %d, out = %q", code, out)
	}
}

func TestRunVirusTotal(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	if _, code := runPhase3A(env, "virustotal", []string{"--bogus", "plain"}); code != 1 {
		t.Error("bad flag must fail")
	}
	if _, code := runPhase3A(env, "virustotal", nil); code != 1 {
		t.Error("missing apps must fail")
	}
	// Missing API key exits 16.
	if out, code := runPhase3A(env, "virustotal", []string{"plain"}); code != 16 || !strings.Contains(out, "VirusTotal API key") {
		t.Errorf("no key exit = %d, out = %q", code, out)
	}
}

func TestRunVirusTotalReports(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	writeFixture(t, filepath.Join(env.ScoopDir, "buckets", "main", "bucket", "vtapp.json"), `{
    "version": "1.0.0",
    "description": "Scanned tool",
    "homepage": "https://example.com/vtapp",
    "license": "MIT",
    "url": "https://example.com/vtapp-1.0.0.zip",
    "hash": "sha256:abababababababababababababababababababababababababababababababab"
}`)
	writeFixture(t, env.ConfigPath, `{"virustotal_api_key": "testkey", "last_update": "2999-01-01T00:00:00Z"}`)
	fileReport := func(malicious int) (int, []byte, error) {
		body := `{"data": {"attributes": {"last_analysis_stats": {"malicious": ` +
			itoa(malicious) + `, "suspicious": 0, "timeout": 0, "undetected": 70}, "size": 100, "sha256": "abababababababababababababababababababababababababababababababab"}}}`
		return 200, []byte(body), nil
	}
	vtRequest = func(method, rawURL, apiKey string, body io.Reader) (int, []byte, error) {
		if apiKey != "testkey" {
			t.Errorf("api key = %q, want testkey", apiKey)
		}
		return fileReport(0)
	}
	if out, code := runPhase3A(env, "virustotal", []string{"vtapp", "-u"}); code != 0 || !strings.Contains(out, "0/70") {
		t.Errorf("clean report exit = %d, out = %q", code, out)
	}
	vtRequest = func(method, rawURL, apiKey string, body io.Reader) (int, []byte, error) {
		return fileReport(3)
	}
	if out, code := runPhase3A(env, "virustotal", []string{"vtapp", "-u"}); code != 2 || !strings.Contains(out, "3/73") {
		t.Errorf("unsafe report exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "virustotal", []string{"nosuchapp", "-u"}); code != 8 || !strings.Contains(out, "manifest not found") {
		t.Errorf("missing manifest exit = %d, out = %q", code, out)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func TestRunShim(t *testing.T) {
	withStubs(t)
	env := updateEnv(t)
	if _, code := runPhase3A(env, "shim", nil); code != 1 {
		t.Error("missing subcommand must fail")
	}
	if _, code := runPhase3A(env, "shim", []string{"frobnicate"}); code != 1 {
		t.Error("unknown subcommand must fail")
	}
	if out, code := runPhase3A(env, "shim", []string{"add", "myapp", filepath.Join("nope", "missing.exe")}); code != 3 || !strings.Contains(out, "Command path does not exist") {
		t.Errorf("missing target exit = %d, out = %q", code, out)
	}
	target := filepath.Join(env.ScoopDir, "tools", "myapp.ps1")
	writeFixture(t, target, "Write-Output hello")
	if _, code := runPhase3A(env, "shim", []string{"add", "myapp", target}); code != 0 {
		t.Fatal("shim add failed")
	}
	if out, code := runPhase3A(env, "shim", []string{"list"}); code != 0 || !strings.Contains(out, "myapp") {
		t.Errorf("shim list exit = %d, out = %q", code, out)
	}
	if out, code := runPhase3A(env, "shim", []string{"info", "myapp"}); code != 0 || !strings.Contains(out, "myapp") {
		t.Errorf("shim info exit = %d, out = %q", code, out)
	}
	if _, code := runPhase3A(env, "shim", []string{"info", "nosuchshim"}); code != 3 {
		t.Error("missing shim info must exit 3")
	}
	if _, code := runPhase3A(env, "shim", []string{"alter", "myapp"}); code != 2 {
		t.Error("alter without alternatives must exit 2")
	}
	if _, code := runPhase3A(env, "shim", []string{"rm", "myapp"}); code != 0 {
		t.Error("shim rm failed")
	}
	if _, code := runPhase3A(env, "shim", []string{"rm", "myapp"}); code != 3 {
		t.Error("double rm must exit 3")
	}
}
