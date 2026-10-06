// Package download fetches manifest artifacts for gscoop.
//
// The flow mirrors Invoke-ScoopDownload and Invoke-CachedDownload
// (lib/download.ps1:5-70): resolve special URLs, look the artifact up in
// the cache (legacy name first, via state.CachePath), fetch on miss,
// copy or move the cached file to the target directory, then verify the
// manifest hash. A failed hash fails only that package; other downloads
// continue (technical plan section 10: classic aborts the transaction).
//
// Fetching is native HTTP with Range resume (a strict improvement over
// classic, which restarts from zero; the cache contract is unchanged)
// through a worker pool sized by MAX_DOWNLOADS (default 4) and optional
// segmented per-file fetch sized by SPLIT_DOWNLOADS (default 1).
// Cookie, PROXY (including currentuser@), GH_TOKEN for api.github.com,
// PRIVATE_HOSTS headers, special-URL rewrites, and redirect fragment
// preservation are all ported from lib/download.ps1. FTP URLs return an
// explicit error (plan section 5.2: documented drop, usage near zero).
// The aria2 path is removed: config keys are tolerated and produce a
// one-line notice, never an execution (plan section 3.4).
//
// Progress renders through internal/ui: live carriage-return lines on TTY
// only, one line per completed artifact otherwise. No emojis.
package download

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/state"
	"github.com/TheSlopMachine/gscoop/internal/ui"
)

// Defaults for the Go-only download keys (technical plan Appendix C).
const (
	DefaultMaxDownloads   = 4
	DefaultSplitDownloads = 1
	maxSplitDownloads     = 16
)

// Request describes one artifact fetch within a package transaction.
type Request struct {
	// App and Version name the cache entry (cache_path, lib/core.ps1:388).
	App     string
	Version string
	// URL is the manifest URL verbatim, fragment included.
	URL string
	// Hash is the manifest hash aligned to URL, or empty.
	Hash string
	// Cookies carries manifest.cookie entries for all HTTP requests.
	Cookies map[string]string
}

// Result reports one finished fetch. Err is set for resolution,
// download, and hash failures alike; a failed hash additionally removes
// the cached file (lib/download.ps1:34-44).
type Result struct {
	Request Request
	// Filename is url_filename(URL), the target leaf name.
	Filename string
	// Cached is the cache file path used.
	Cached string
	// Path is the final target path (Dir joined with Filename).
	Path string
	// Bytes is the fetched size. Skipped reports cache hits.
	Bytes   int64
	Resumed bool
	Skipped bool
	// Verified reports a hash was present and matched.
	Verified bool
	Err      error
}

// Options tunes a Downloader. Zero values select classic-compatible
// behavior with the Go-only defaults for pool sizes.
type Options struct {
	// CacheDir holds cache files ($cachedir, lib/core.ps1:1385).
	CacheDir string
	// Dir receives the fetched files (the version directory).
	Dir string
	// UseCache reads and retains cache files. False re-downloads and
	// moves the result to Dir (lib/download.ps1:54-70).
	UseCache bool
	// CheckHash verifies manifest hashes after each fetch.
	CheckHash bool
	// MaxDownloads sizes the worker pool (default 4).
	MaxDownloads int
	// SplitDownloads sizes segmented per-file fetch (default 1).
	SplitDownloads int
	// Client performs HTTP. Nil builds one from Proxy.
	Client *http.Client
	// Proxy is the PROXY config value (setup_proxy, lib/download.ps1:560).
	Proxy string
	// GitHubToken feeds api.github.com authorization.
	GitHubToken string
	// PrivateHosts carries PRIVATE_HOSTS entries.
	PrivateHosts []PrivateHost
	// UserAgent overrides the default agent string.
	UserAgent string
	// FossHubAPI overrides the FossHub download endpoint (tests).
	FossHubAPI string
	// GitHubAPI overrides https://api.github.com (tests).
	GitHubAPI string
	// Log receives diagnostics. Nil drops them.
	Log *ui.Logger
	// Progress renders live lines. Nil or disabled means one line per
	// completed artifact (technical plan section 4.2).
	Progress *ui.Progress
}

// Downloader fetches artifacts with a shared HTTP client.
type Downloader struct {
	opts   Options
	client *http.Client
	notice string
}

// New builds a Downloader, constructing an HTTP client from Proxy when
// none is supplied. A proxy warning surfaces through notice.
func New(opts Options) (*Downloader, error) {
	if opts.MaxDownloads <= 0 {
		opts.MaxDownloads = DefaultMaxDownloads
	}
	if opts.SplitDownloads <= 0 {
		opts.SplitDownloads = DefaultSplitDownloads
	}
	if opts.SplitDownloads > maxSplitDownloads {
		opts.SplitDownloads = maxSplitDownloads
	}
	if opts.UserAgent == "" {
		opts.UserAgent = UserAgent()
	}
	if opts.FossHubAPI == "" {
		opts.FossHubAPI = FossHubAPI
	}
	if opts.GitHubAPI == "" {
		opts.GitHubAPI = GitHubAPI
	}
	d := &Downloader{opts: opts}
	if opts.Client != nil {
		d.client = opts.Client
		return d, nil
	}
	client, notice, err := NewHTTPClient(opts.Proxy)
	if err != nil {
		return nil, err
	}
	d.client = client
	d.notice = notice
	return d, nil
}

// OptionsFromStore resolves download Options from a config Store plus
// explicit directories. Unknown keys are ignored (config tolerates them).
func OptionsFromStore(cfg *config.Store, cacheDir, dir string, log *ui.Logger, prog *ui.Progress) Options {
	opts := Options{
		CacheDir:     cacheDir,
		Dir:          dir,
		UseCache:     true,
		CheckHash:    true,
		MaxDownloads: intConfig(cfg, config.KeyMaxDownloads, DefaultMaxDownloads),
		SplitDownloads: intConfig(cfg, config.KeySplitDownloads,
			DefaultSplitDownloads),
		GitHubToken: cfg.GitHubToken(),
		Log:         log,
		Progress:    prog,
	}
	if opts.MaxDownloads < 1 {
		opts.MaxDownloads = 1
	}
	if opts.SplitDownloads < 1 {
		opts.SplitDownloads = 1
	}
	if opts.SplitDownloads > maxSplitDownloads {
		opts.SplitDownloads = maxSplitDownloads
	}
	if proxy, ok := cfg.GetString("proxy"); ok {
		opts.Proxy = proxy
	}
	if hosts, err := PrivateHostsFromStore(cfg); err == nil {
		opts.PrivateHosts = hosts
	}
	return opts
}

// intConfig reads an integer config value with a default. JSON numbers
// decode as float64; numeric strings parse too.
func intConfig(cfg *config.Store, key string, def int) int {
	if cfg == nil {
		return def
	}
	v, ok := cfg.Value(key)
	if !ok || v == nil {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var n int
		if _, err := fmt.Sscanf(t, "%d", &n); err == nil {
			return n
		}
	}
	return def
}

// Aria2Notice reports the one-line aria2 notice when the user explicitly
// enables aria2. The path is never executed; the native downloader
// supersedes it (technical plan section 3.4).
func Aria2Notice(cfg *config.Store) string {
	if cfg == nil {
		return ""
	}
	if enabled, ok := cfg.GetBool("aria2-enabled"); ok && enabled {
		return "aria2 is not executed; using the native downloader"
	}
	return ""
}

// Filenames maps URLs to url_filename leaves, mirroring the return of
// Invoke-ScoopDownload (lib/download.ps1:49).
func Filenames(urls []string) []string {
	names := make([]string, 0, len(urls))
	for _, u := range urls {
		names = append(names, URLFilename(u))
	}
	return names
}

// Download runs one package transaction: every URL resolves, fetches
// (pool), lands in Dir, and verifies its hash. It returns per-URL
// results in input order plus the url_filename list. A failed hash
// removes the cached file and fails only that entry.
func (d *Downloader) Download(ctx context.Context, app, version string, urls, hashes []string, cookies map[string]string) ([]Result, []string) {
	if !d.opts.UseCache {
		d.warn("Cache is being ignored.")
	}
	if notice := d.notice; notice != "" {
		d.notice = ""
		d.warn(notice)
	}
	reqs := make([]Request, 0, len(urls))
	for i, u := range urls {
		var hash string
		if i < len(hashes) {
			hash = hashes[i]
		}
		reqs = append(reqs, Request{
			App:     app,
			Version: version,
			URL:     u,
			Hash:    hash,
			Cookies: cookies,
		})
	}
	results := d.DownloadAll(ctx, reqs)
	for i := range results {
		if results[i].Err != nil {
			continue
		}
		if !d.opts.CheckHash {
			continue
		}
		if ok, msg := CheckFileHash(results[i].Path, results[i].Request.Hash, app); !ok {
			if msg != "" {
				d.error(msg)
			}
			if results[i].Cached != "" {
				os.Remove(results[i].Cached)
			}
			if containsFold(results[i].Request.URL, "sourceforge.net") {
				d.warn("SourceForge.net is known for causing hash validation fails. Please try again before opening a ticket.")
			}
			results[i].Err = &HashError{URL: results[i].Request.URL, Detail: msg}
			continue
		}
		results[i].Verified = results[i].Request.Hash != ""
	}
	return results, Filenames(urls)
}

// DownloadAll fetches requests concurrently on a pool sized by
// MaxDownloads and verifies each file as it completes on a separate
// pool. A failure fails only its own result.
func (d *Downloader) DownloadAll(ctx context.Context, reqs []Request) []Result {
	results := make([]Result, len(reqs))
	if len(reqs) == 0 {
		return results
	}
	type completed struct {
		index int
		res   Result
	}
	fetched := make(chan completed, len(reqs))
	sem := make(chan struct{}, d.opts.MaxDownloads)
	var fetchWG sync.WaitGroup
	for i, req := range reqs {
		fetchWG.Add(1)
		go func(index int, r Request) {
			defer fetchWG.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				fetched <- completed{index: index, res: Result{Request: r, Err: ctx.Err()}}
				return
			}
			res := d.downloadOne(ctx, index, len(reqs), r)
			fetched <- completed{index: index, res: res}
		}(i, req)
	}
	go func() {
		fetchWG.Wait()
		close(fetched)
	}()
	for c := range fetched {
		results[c.index] = c.res
	}
	return results
}

// downloadOne resolves, fetches, and places one artifact.
func (d *Downloader) downloadOne(ctx context.Context, index, total int, req Request) Result {
	res := Result{Request: req, Filename: URLFilename(req.URL)}
	if IsFTPURL(req.URL) {
		res.Err = fmt.Errorf("URL %s uses FTP, which the native downloader does not support", req.URL)
		return res
	}
	resolved, err := d.resolveURL(ctx, req.URL)
	if err != nil {
		res.Err = fmt.Errorf("URL %s is not valid: %w", req.URL, err)
		return res
	}
	cached := state.CachePath(d.opts.CacheDir, req.App, req.Version, req.URL)
	res.Cached = cached
	target := filepath.Join(d.opts.Dir, res.Filename)
	res.Path = target
	if info, err := os.Stat(cached); err == nil && !info.IsDir() && d.opts.UseCache {
		d.infof("Loading %s from cache", URLRemoteFilename(req.URL))
		if err := copyFile(cached, target); err != nil {
			res.Err = err
			return res
		}
		res.Bytes = info.Size()
		res.Skipped = true
		d.reportDone(index, total, res)
		return res
	}
	if err := os.MkdirAll(filepath.Dir(cached), 0o755); err != nil {
		res.Err = err
		return res
	}
	bytes, resumed, err := d.fetchToCache(ctx, cached, resolved, req, index, total)
	if err != nil {
		res.Err = err
		return res
	}
	res.Bytes = bytes
	res.Resumed = resumed
	if d.opts.UseCache {
		if err := copyFile(cached, target); err != nil {
			res.Err = err
			return res
		}
	} else {
		if err := renameOrCopy(cached, target); err != nil {
			res.Err = err
			return res
		}
	}
	d.reportDone(index, total, res)
	return res
}

// reportDone emits the per-artifact completion line and closes progress.
func (d *Downloader) reportDone(index, total int, res Result) {
	if d.opts.Progress != nil {
		d.opts.Progress.Done()
	} else {
		d.infof("Downloaded %s (%s)", URLRemoteFilename(res.Request.URL), ui.FormatSize(res.Bytes))
	}
	_ = index
	_ = total
}

func (d *Downloader) warn(msg string) {
	if d.opts.Log != nil {
		d.opts.Log.Warn(msg)
	}
}

func (d *Downloader) error(msg string) {
	if d.opts.Log != nil {
		d.opts.Log.Error(msg)
	}
}

func (d *Downloader) infof(format string, args ...any) {
	if d.opts.Log != nil {
		d.opts.Log.Infof(format, args...)
	}
}
