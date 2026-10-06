package commands

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/TheSlopMachine/gscoop/internal/gitengine"
	"github.com/TheSlopMachine/gscoop/internal/junction"
)

// pinHistoryEngine answers history lookups with one canned manifest,
// standing in for a bucket clone whose HEAD already carries the pin.
type pinHistoryEngine struct {
	head string
	raw  []byte
}

func (e *pinHistoryEngine) LsRemote(url string) (string, error) { return "abc123", nil }

func (e *pinHistoryEngine) Clone(url, dir string, opts gitengine.CloneOptions) error { return nil }

func (e *pinHistoryEngine) Pull(repo string, opts gitengine.PullOptions) error { return nil }

func (e *pinHistoryEngine) Fetch(repo, refspec string, force bool) error { return nil }

func (e *pinHistoryEngine) CheckoutCreate(repo, branch, track string) error { return nil }

func (e *pinHistoryEngine) ResetHard(repo, rev string) error { return nil }

func (e *pinHistoryEngine) Head(repo string) (string, error) { return e.head, nil }

func (e *pinHistoryEngine) ConfigGet(repo, key string) (string, error) { return "", nil }

func (e *pinHistoryEngine) ConfigSet(repo, key, value string) error { return nil }

func (e *pinHistoryEngine) LogSince(repo, rev string, pathFilter string, invertGrep *regexp.Regexp, limit int) ([]gitengine.LogEntry, error) {
	return nil, nil
}

func (e *pinHistoryEngine) DiffNameStatus(repo, a, b string) ([]gitengine.DiffEntry, error) {
	return nil, nil
}

func (e *pinHistoryEngine) ShowFile(repo, rev, path string) ([]byte, error) {
	return e.raw, nil
}

func (e *pinHistoryEngine) Status(repo string) (gitengine.FileStatus, error) {
	return gitengine.FileStatus{}, nil
}

func (e *pinHistoryEngine) StashLike(repo string) (gitengine.StashResult, error) {
	return gitengine.StashResult{}, nil
}

// pinEnv builds an isolated root with plain 1.0.0 installed and plain
// 2.0.0 at bucket HEAD.
func pinEnv(t *testing.T) *Env {
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
	writeFixture(t, filepath.Join(scoop, "apps", "plain", "1.0.0", "scoop-manifest.json"), `{"version": "1.0.0", "description": "old"}`)
	writeFixture(t, filepath.Join(scoop, "apps", "plain", "1.0.0", "scoop-install.json"), `{"architecture": "64bit", "bucket": "main"}`)
	writeFixture(t, filepath.Join(scoop, "buckets", "main", ".git", "HEAD"), "ref: refs/heads/master")
	return env
}

func withPinStubs(t *testing.T, engine gitengine.GitEngine) {
	t.Helper()
	prevEngine := newUpdateEngine
	prevDownloader, prevExtractor := mutateDownloader, mutateExtractor
	newUpdateEngine = func(useExternal bool) gitengine.GitEngine { return engine }
	mutateDownloader = stubDownloader{}
	mutateExtractor = nil
	t.Cleanup(func() {
		newUpdateEngine = prevEngine
		mutateDownloader = prevDownloader
		mutateExtractor = prevExtractor
	})
}

func TestWireDefaultBackends(t *testing.T) {
	prevDownloader, prevExtractor := mutateDownloader, mutateExtractor
	t.Cleanup(func() {
		mutateDownloader = prevDownloader
		mutateExtractor = prevExtractor
	})
	mutateDownloader, mutateExtractor = nil, nil
	WireDefaultBackends(fixtureEnv(t))
	if mutateDownloader == nil {
		t.Error("WireDefaultBackends left download seam nil")
	}
	if mutateExtractor == nil {
		t.Error("WireDefaultBackends left extraction seam nil")
	}
}

func TestManifestFetchArchOverride(t *testing.T) {
	raw := []byte(`{
    "version": "1.0.0",
    "url": "https://example.com/top.zip",
    "hash": "sha256:top",
    "cookie": {"session": "abc"},
    "architecture": {"64bit": {"url": "https://example.com/x64.zip", "hash": "sha256:x64"}}
}`)
	urls, hashes, cookies := manifestFetch(raw, "64bit")
	if len(urls) != 1 || urls[0] != "https://example.com/x64.zip" {
		t.Errorf("arch urls = %v", urls)
	}
	if len(hashes) != 1 || hashes[0] != "sha256:x64" {
		t.Errorf("arch hashes = %v", hashes)
	}
	if cookies["session"] != "abc" {
		t.Errorf("cookies = %v", cookies)
	}
	urls, _, _ = manifestFetch(raw, "arm64")
	if len(urls) != 1 || urls[0] != "https://example.com/top.zip" {
		t.Errorf("fallback urls = %v", urls)
	}
	if urls, _, _ := manifestFetch([]byte(`{"version": "1.0.0"}`), "64bit"); len(urls) != 0 {
		t.Errorf("missing urls = %v", urls)
	}
}

func TestOwnsMisc(t *testing.T) {
	for _, name := range []string{"config", "search", "create"} {
		if !OwnsMisc(name) {
			t.Errorf("OwnsMisc(%q) = false", name)
		}
	}
	if code, ok := RunMisc(fixtureEnv(t), &bytes.Buffer{}, "install", nil); ok || code != 0 {
		t.Errorf("RunMisc(install) = %d, %v", code, ok)
	}
}

func TestRunConfigRoundTrip(t *testing.T) {
	env := fixtureEnv(t)
	var buf bytes.Buffer
	if code := RunConfig(env, &buf, []string{"proxy", "none"}); code != 0 {
		t.Fatalf("set exit = %d, out = %q", code, buf.String())
	}
	if !strings.Contains(buf.String(), "'proxy' has been set to 'none'") {
		t.Errorf("set output = %q", buf.String())
	}
	buf.Reset()
	if code := RunConfig(env, &buf, []string{"proxy"}); code != 0 || strings.TrimSpace(buf.String()) != "none" {
		t.Errorf("get exit = %d, out = %q", code, buf.String())
	}
	buf.Reset()
	if code := RunConfig(env, &buf, []string{"missing_key"}); code != 0 || !strings.Contains(buf.String(), "is not set") {
		t.Errorf("unset exit = %d, out = %q", code, buf.String())
	}
	buf.Reset()
	if code := RunConfig(env, &buf, []string{"rm", "proxy"}); code != 0 || !strings.Contains(buf.String(), "has been removed") {
		t.Errorf("rm exit = %d, out = %q", code, buf.String())
	}
	buf.Reset()
	if code := RunConfig(env, &buf, nil); code != 0 {
		t.Errorf("list exit = %d, out = %q", code, buf.String())
	}
	buf.Reset()
	if code := RunConfig(env, &buf, []string{"rm"}); code != 1 {
		t.Errorf("rm without name exit = %d, want 1", code)
	}
}

func TestRunSearchLocal(t *testing.T) {
	env := fixtureEnv(t)
	var buf bytes.Buffer
	if code := RunSearch(env, &buf, []string{"dep"}); code != 0 {
		t.Fatalf("search exit = %d, out = %q", code, buf.String())
	}
	if !strings.Contains(buf.String(), "Results from local buckets...") || !strings.Contains(buf.String(), "dep") {
		t.Errorf("search output = %q", buf.String())
	}
	buf.Reset()
	if code := RunSearch(env, &buf, nil); code != 0 || !strings.Contains(buf.String(), "git") {
		t.Errorf("empty query exit = %d, out = %q", code, buf.String())
	}
	buf.Reset()
	if code := RunSearch(env, &buf, []string{"zzz-no-such-app"}); code != 1 || !strings.Contains(buf.String(), "No matches found.") {
		t.Errorf("no match exit = %d, out = %q", code, buf.String())
	}
}

func TestRunCreateScaffold(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
	env := fixtureEnv(t)
	var buf bytes.Buffer
	if code := RunCreate(env, &buf, []string{"https://example.com/files/tool-1.2.3.zip"}); code != 0 {
		t.Fatalf("create exit = %d, out = %q", code, buf.String())
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tool-1.2.3.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"url": "https://example.com/files/tool-1.2.3.zip"`, `"version": ""`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("scaffold missing %s:\n%s", want, raw)
		}
	}
	if code := RunCreate(env, &buf, nil); code != 1 {
		t.Errorf("missing url exit = %d, want 1", code)
	}
	if code := RunCreate(env, &buf, []string{"not-a-url"}); code != 1 {
		t.Errorf("invalid url exit = %d, want 1", code)
	}
}

func TestUpdatePinResolvesFromHistory(t *testing.T) {
	pinned := []byte(`{
    "version": "1.5.0",
    "description": "Plain tool",
    "homepage": "https://example.com/plain",
    "license": "MIT"
}`)
	withPinStubs(t, &pinHistoryEngine{head: "abc123", raw: pinned})
	env := pinEnv(t)
	var buf bytes.Buffer
	code, handled := RunPhase3A(env, &buf, "update", []string{"plain@1.5.0"})
	if !handled || code != 0 {
		t.Fatalf("exit = %d handled = %v, out = %q", code, handled, buf.String())
	}
	if !strings.Contains(buf.String(), "Updating 'plain' (1.0.0 -> 1.5.0)") {
		t.Errorf("output missing pin update line:\n%s", buf.String())
	}
	if got := env.CurrentVersion("plain", false); got != "1.5.0" {
		t.Errorf("current = %q, want 1.5.0", got)
	}
}

func TestUpdatePinShallowFailsClosed(t *testing.T) {
	withPinStubs(t, &pinHistoryEngine{head: "abc123", raw: []byte(`{"version": "2.0.0"}`)})
	env := pinEnv(t)
	writeFixture(t, filepath.Join(env.ScoopDir, "buckets", "main", ".git", "shallow"), "abc123")
	var buf bytes.Buffer
	code, handled := RunPhase3A(env, &buf, "update", []string{"plain@9.9.9"})
	if !handled {
		t.Fatal("update not handled")
	}
	if !strings.Contains(buf.String(), "plain@9.9.9") {
		t.Errorf("output missing pin context, exit = %d:\n%s", code, buf.String())
	}
	if _, err := os.Stat(filepath.Join(env.ScoopDir, "apps", "plain", "9.9.9")); !os.IsNotExist(err) {
		t.Error("unresolvable pin must not install a version dir")
	}
}
