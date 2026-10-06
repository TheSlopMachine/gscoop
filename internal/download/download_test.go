// End-to-end fetch tests over httptest: cache hits, fresh fetch with
// hash, Range resume, segmented assembly, and per-package hash failure
// isolation. No external network. Classic basis: lib/download.ps1:5-70.
// No emojis.
package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/state"
	"github.com/TheSlopMachine/gscoop/internal/ui"
)

func testTime() time.Time {
	return time.Unix(1700000000, 0).UTC()
}

func silentLog() *ui.Logger {
	return &ui.Logger{Out: io.Discard, Err: io.Discard}
}

func contentServer(content []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "payload.bin", testTime(), bytes.NewReader(content))
	}))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func TestCacheHitSkipsNetwork(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rawurl := "https://example.com/app-1.0.zip"
	cached := state.CachePath(cacheDir, "app", "1.0", rawurl)
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cached, []byte("cached-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Any network fetch fails the test.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("network fetch on cache hit")
	}))
	defer server.Close()
	_ = server
	d, err := New(Options{CacheDir: cacheDir, Dir: filepath.Join(dir, "v"), UseCache: true, Log: silentLog(), Client: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	results, names := d.Download(context.Background(), "app", "1.0", []string{rawurl}, []string{""}, nil)
	if names[0] != "app-1.0.zip" {
		t.Errorf("filename = %q", names[0])
	}
	if results[0].Err != nil {
		t.Fatal(results[0].Err)
	}
	if !results[0].Skipped {
		t.Error("expected cache hit")
	}
	got, err := os.ReadFile(filepath.Join(dir, "v", "app-1.0.zip"))
	if err != nil || string(got) != "cached-bytes" {
		t.Errorf("target = %q, %v", got, err)
	}
}

func TestFetchVerifyAndCache(t *testing.T) {
	content := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	server := contentServer(content)
	defer server.Close()
	dir := t.TempDir()
	rawurl := server.URL + "/tool-2.0.zip"
	d, err := New(Options{
		CacheDir: filepath.Join(dir, "cache"),
		Dir:      filepath.Join(dir, "v"),
		UseCache: true, CheckHash: true,
		Log: silentLog(), Client: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _ := d.Download(context.Background(), "tool", "2.0", []string{rawurl}, []string{sha256Hex(content)}, map[string]string{"s": "1"})
	if results[0].Err != nil {
		t.Fatal(results[0].Err)
	}
	if !results[0].Verified || results[0].Bytes != int64(len(content)) {
		t.Errorf("result = %+v", results[0])
	}
	if _, err := os.Stat(results[0].Cached); err != nil {
		t.Errorf("cache file missing: %v", err)
	}
	got, err := os.ReadFile(results[0].Path)
	if err != nil || !bytes.Equal(got, content) {
		t.Error("target content mismatch")
	}
}

func TestResumePartial(t *testing.T) {
	content := bytes.Repeat([]byte("zxcv"), 8192)
	server := contentServer(content)
	defer server.Close()
	dir := t.TempDir()
	rawurl := server.URL + "/big.zip"
	cached := state.CachePath(filepath.Join(dir, "cache"), "big", "1", rawurl)
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cached+".download", content[:len(content)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := New(Options{CacheDir: filepath.Join(dir, "cache"), Dir: filepath.Join(dir, "v"), UseCache: true, Log: silentLog(), Client: &http.Client{}})
	if err != nil {
		t.Fatal(err)
	}
	results, _ := d.Download(context.Background(), "big", "1", []string{rawurl}, []string{sha256Hex(content)}, nil)
	if results[0].Err != nil {
		t.Fatal(results[0].Err)
	}
	if !results[0].Resumed {
		t.Error("expected Range resume")
	}
	got, err := os.ReadFile(results[0].Path)
	if err != nil || !bytes.Equal(got, content) {
		t.Error("resumed content mismatch")
	}
}

func TestSegmentedFetch(t *testing.T) {
	content := make([]byte, 1<<20)
	for i := range content {
		content[i] = byte(i * 31)
	}
	server := contentServer(content)
	defer server.Close()
	dir := t.TempDir()
	rawurl := server.URL + "/seg.7z"
	d, err := New(Options{
		CacheDir: filepath.Join(dir, "cache"), Dir: filepath.Join(dir, "v"),
		UseCache: true, CheckHash: true, SplitDownloads: 4,
		Log: silentLog(), Client: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _ := d.Download(context.Background(), "seg", "1", []string{rawurl}, []string{sha256Hex(content)}, nil)
	if results[0].Err != nil {
		t.Fatal(results[0].Err)
	}
	if results[0].Resumed {
		t.Error("segmented fetch must not report resume")
	}
	got, err := os.ReadFile(results[0].Path)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("segmented content mismatch")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "cache", "*.part*"))
	if len(leftovers) != 0 {
		t.Errorf("part files remain: %v", leftovers)
	}
}

func TestHashFailureIsolatesPackage(t *testing.T) {
	content := []byte("good-bytes")
	server := contentServer(content)
	defer server.Close()
	dir := t.TempDir()
	good := server.URL + "/good.zip"
	bad := server.URL + "/bad.zip"
	d, err := New(Options{
		CacheDir: filepath.Join(dir, "cache"), Dir: filepath.Join(dir, "v"),
		UseCache: true, CheckHash: true, MaxDownloads: 2,
		Log: silentLog(), Client: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _ := d.Download(context.Background(), "mix", "1",
		[]string{bad, good},
		[]string{"0000000000000000000000000000000000000000000000000000000000000000", sha256Hex(content)}, nil)
	if results[0].Err == nil {
		t.Fatal("bad hash must fail")
	}
	if _, ok := results[0].Err.(*HashError); !ok {
		t.Errorf("bad hash error type = %T", results[0].Err)
	}
	if results[1].Err != nil || !results[1].Verified {
		t.Errorf("good package must verify: %+v", results[1])
	}
	if _, err := os.Stat(results[0].Cached); !os.IsNotExist(err) {
		t.Error("failed hash must remove the cached file")
	}
}

func TestNoCacheMoves(t *testing.T) {
	content := []byte("fresh")
	server := contentServer(content)
	defer server.Close()
	dir := t.TempDir()
	rawurl := server.URL + "/nocache.zip"
	d, err := New(Options{
		CacheDir: filepath.Join(dir, "cache"), Dir: filepath.Join(dir, "v"),
		UseCache: false, CheckHash: false, Log: silentLog(), Client: &http.Client{},
	})
	if err != nil {
		t.Fatal(err)
	}
	results, _ := d.Download(context.Background(), "nc", "1", []string{rawurl}, nil, nil)
	if results[0].Err != nil {
		t.Fatal(results[0].Err)
	}
	if _, err := os.Stat(results[0].Cached); !os.IsNotExist(err) {
		t.Error("no-cache mode must move the file out of the cache")
	}
	got, err := os.ReadFile(results[0].Path)
	if err != nil || string(got) != "fresh" {
		t.Errorf("target = %q, %v", got, err)
	}
}

func TestAria2Notice(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Aria2Notice(cfg); got != "" {
		t.Errorf("unset aria2 = %q", got)
	}
	if err := cfg.Set("aria2-enabled", true); err != nil {
		t.Fatal(err)
	}
	if got := Aria2Notice(cfg); got == "" {
		t.Error("enabled aria2 must produce a notice")
	}
}

func TestOptionsFromStore(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	opts := OptionsFromStore(cfg, "c", "d", silentLog(), nil)
	if opts.MaxDownloads != DefaultMaxDownloads || opts.SplitDownloads != DefaultSplitDownloads {
		t.Errorf("defaults = %+v", opts)
	}
	if err := cfg.Set(config.KeyMaxDownloads, 2); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Set(config.KeySplitDownloads, 8); err != nil {
		t.Fatal(err)
	}
	opts = OptionsFromStore(cfg, "c", "d", silentLog(), nil)
	if opts.MaxDownloads != 2 || opts.SplitDownloads != 8 {
		t.Errorf("overrides = %+v", opts)
	}
}
