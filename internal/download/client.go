// HTTP client: request headers, redirect policy, and special-URL
// resolution over the network.
//
// Header rules mirror Invoke-Download (lib/download.ps1:89-112):
// User-Agent always; Referer from strip_filename except SourceForge and
// portableapps; api.github.com/repos URLs carry octet-stream Accept plus
// Bearer token and API version; cookie_header when cookies exist;
// PRIVATE_HOSTS entries add headers on regex match. Redirects mirror the
// manual 301/302/303/307 handling with #/ fragment preservation
// (lib/download.ps1:116-147); other statuses surface as errors.
// A 401 warns about token misconfiguration (lib/download.ps1:81-83).
package download

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/config"
)

// Redirect statuses handled manually, mirroring the handledCodes list
// (lib/download.ps1:118-123).
func isRedirectStatus(code int) bool {
	switch code {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect:
		return true
	}
	return false
}

// maxRedirects caps the manual chain. Classic recursed without a cap;
// the cap fails closed instead of looping forever.
const maxRedirects = 10

// FossHubAPI is the FossHub download endpoint
// (lib/download.ps1:617).
const FossHubAPI = "https://api.fosshub.com/download/"

// GitHubAPI is the default GitHub REST base.
const GitHubAPI = "https://api.github.com"

// noRedirectClient clones the downloader client with automatic
// redirects disabled so the chain stays manual.
func (d *Downloader) noRedirectClient() *http.Client {
	clone := *d.client
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

// BuildRequest assembles one download request with classic headers.
func (d *Downloader) BuildRequest(ctx context.Context, method, rawurl string, cookies map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, WireURL(rawurl), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", d.opts.UserAgent)
	if WantsReferer(rawurl) {
		req.Header.Set("Referer", StripFilename(rawurl))
	}
	if WantsGitHubAPIHeaders(rawurl) {
		req.Header.Set("Accept", "application/octet-stream")
		if d.opts.GitHubToken != "" {
			req.Header.Set("Authorization", "Bearer "+d.opts.GitHubToken)
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		}
	}
	if header := CookieHeader(cookies); header != "" {
		req.Header.Add("Cookie", header)
	}
	for _, host := range d.opts.PrivateHosts {
		matched, err := regexp.MatchString(host.Match, rawurl)
		if err != nil || !matched {
			continue
		}
		for key, value := range host.Headers {
			req.Header.Set(textproto.CanonicalMIMEHeaderKey(key), value)
		}
	}
	return req, nil
}

// doManual follows one redirect hop manually, preserving #/ fragments.
// It returns the final non-redirect response for the caller to consume.
func (d *Downloader) doManual(ctx context.Context, rawurl string, decorate func(*http.Request), cookies map[string]string) (*http.Response, string, error) {
	client := d.noRedirectClient()
	current := rawurl
	fragment := RedirectFragment(rawurl)
	for hop := 0; ; hop++ {
		req, err := d.BuildRequest(ctx, http.MethodGet, current, cookies)
		if err != nil {
			return nil, current, err
		}
		if decorate != nil {
			decorate(req)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, current, err
		}
		if !isRedirectStatus(resp.StatusCode) {
			return resp, current, nil
		}
		location := resp.Header.Get("Location")
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		if location == "" {
			return nil, current, fmt.Errorf("redirect from %s has no Location", WireURL(current))
		}
		if hop+1 > maxRedirects {
			return nil, current, fmt.Errorf("too many redirects following %s", WireURL(rawurl))
		}
		next, err := resolveLocation(WireURL(current), location)
		if err != nil {
			return nil, current, err
		}
		next = JoinRedirectFragment(next, fragment)
		d.infof("Following redirect to %s...", WireURL(next))
		current = next
	}
}

// resolveLocation resolves a Location header against the request URL.
func resolveLocation(base, location string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(location)
	if err != nil {
		return "", err
	}
	return parsed.ResolveReference(ref).String(), nil
}

// resolveURL applies handle_special_urls (lib/download.ps1:604-641):
// SourceForge reshape (pure), FossHub API POST, and the private GitHub
// release asset rewrite when a token exists.
func (d *Downloader) resolveURL(ctx context.Context, rawurl string) (string, error) {
	if rewritten, ok := RewriteSourceForge(rawurl); ok {
		return rewritten, nil
	}
	if projectURI, fileName, ok := FossHubParts(rawurl); ok {
		return d.fetchFossHubURL(ctx, projectURI, fileName)
	}
	if owner, repo, tag, file, rest, ok := GitHubReleaseParts(rawurl); ok && d.opts.GitHubToken != "" {
		if rewritten, err := d.fetchGitHubPrivateAsset(ctx, owner, repo, tag, file); err == nil && rewritten != "" {
			return rewritten + rest, nil
		} else if err != nil {
			return "", err
		}
	}
	return rawurl, nil
}

// fetchFossHubURL POSTs the project identity to the FossHub download
// API and returns the direct URL (lib/download.ps1:607-621).
func (d *Downloader) fetchFossHubURL(ctx context.Context, projectURI, fileName string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"projectUri":      projectURI,
		"fileName":        fileName,
		"source":          "CF",
		"isLatestVersion": true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.opts.FossHubAPI, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", d.opts.UserAgent)
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var doc struct {
		Error any `json:"error"`
		Data  struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", err
	}
	if doc.Data.URL == "" {
		return "", fmt.Errorf("FossHub download API returned no URL")
	}
	return doc.Data.URL, nil
}

// fetchGitHubPrivateAsset rewrites a release asset URL through the API
// when the repository is private (lib/download.ps1:630-638). It returns
// "" when the repository is public, keeping the original URL.
func (d *Downloader) fetchGitHubPrivateAsset(ctx context.Context, owner, repo, tag, file string) (string, error) {
	base := strings.TrimSuffix(d.opts.GitHubAPI, "/")
	get := func(path string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "token "+d.opts.GitHubToken)
		req.Header.Set("User-Agent", d.opts.UserAgent)
		return req, nil
	}
	repoReq, err := get(fmt.Sprintf("/repos/%s/%s", owner, repo))
	if err != nil {
		return "", err
	}
	resp, err := d.client.Do(repoReq)
	if err != nil {
		return "", err
	}
	var repoDoc struct {
		Private bool `json:"private"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoDoc); err != nil {
		resp.Body.Close()
		return "", err
	}
	resp.Body.Close()
	if !repoDoc.Private {
		return "", nil
	}
	tagReq, err := get(fmt.Sprintf("/repos/%s/%s/releases/tags/%s", owner, repo, tag))
	if err != nil {
		return "", err
	}
	tagResp, err := d.client.Do(tagReq)
	if err != nil {
		return "", err
	}
	defer tagResp.Body.Close()
	var tagDoc struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(tagResp.Body).Decode(&tagDoc); err != nil {
		return "", err
	}
	for _, asset := range tagDoc.Assets {
		if asset.Name == file {
			return asset.URL, nil
		}
	}
	return "", fmt.Errorf("GitHub release asset %s not found", file)
}

// GitHubToken resolves the live token from environment and config,
// mirroring Get-GitHubToken (lib/download.ps1:589-591).
func GitHubToken(cfg *config.Store) string {
	if cfg == nil {
		return config.ResolveGitHubToken(envOrEmpty("SCOOP_GH_TOKEN"), "", envOrEmpty("GH_TOKEN"), envOrEmpty("GITHUB_TOKEN"))
	}
	return cfg.GitHubToken()
}

func envOrEmpty(name string) string {
	return os.Getenv(name)
}
