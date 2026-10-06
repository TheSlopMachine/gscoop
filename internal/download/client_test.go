// Tests for request headers and network special-URL rewrites using
// httptest servers. No external network. Classic basis:
// lib/download.ps1:89-112, 604-641. No emojis.
package download

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testDownloader(t *testing.T, opts Options) *Downloader {
	t.Helper()
	if opts.Client == nil {
		opts.Client = &http.Client{}
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "test-agent"
	}
	if opts.FossHubAPI == "" {
		opts.FossHubAPI = FossHubAPI
	}
	if opts.GitHubAPI == "" {
		opts.GitHubAPI = GitHubAPI
	}
	d, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestBuildRequestHeaders(t *testing.T) {
	d := testDownloader(t, Options{GitHubToken: "tok123"})
	req, err := d.BuildRequest(context.Background(), http.MethodGet, "https://api.github.com/repos/o/r", map[string]string{"b": "2", "a": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Accept"); got != "application/octet-stream" {
		t.Errorf("Accept = %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok123" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
		t.Errorf("API version = %q", got)
	}
	if got := req.Header.Get("Cookie"); got != "a=1;b=2" {
		t.Errorf("Cookie = %q", got)
	}
	// strip_filename replaces every occurrence of the leaf, exactly
	// like the classic -replace (lib/core.ps1:620).
	if got := req.Header.Get("Referer"); got != "https://api.github.com/epos/o/" {
		t.Errorf("Referer = %q", got)
	}
	plain, err := d.BuildRequest(context.Background(), http.MethodGet, "https://downloads.sourceforge.net/project/a/b.exe", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := plain.Header.Get("Referer"); got != "" {
		t.Errorf("SourceForge must not send Referer, got %q", got)
	}
}

func TestBuildRequestPrivateHosts(t *testing.T) {
	d := testDownloader(t, Options{
		PrivateHosts: []PrivateHost{{Match: `example\.com`, Headers: map[string]string{"X-Token": "s3cret"}}},
	})
	req, err := d.BuildRequest(context.Background(), http.MethodGet, "https://example.com/f.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("X-Token"); got != "s3cret" {
		t.Errorf("X-Token = %q", got)
	}
	other, err := d.BuildRequest(context.Background(), http.MethodGet, "https://other.org/f.zip", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := other.Header.Get("X-Token"); got != "" {
		t.Errorf("unmatched host must not send header, got %q", got)
	}
}

func TestFetchFossHubURL(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"error": null, "data": {"url": "https://cdn.example/direct.exe"}}`)
	}))
	defer server.Close()
	d := testDownloader(t, Options{FossHubAPI: server.URL})
	got, err := d.fetchFossHubURL(context.Background(), "Audacity.html", "audacity.exe")
	if err != nil || got != "https://cdn.example/direct.exe" {
		t.Fatalf("fetchFossHubURL = %q, %v", got, err)
	}
	if gotBody["projectUri"] != "Audacity.html" || gotBody["source"] != "CF" {
		t.Errorf("FossHub body = %v", gotBody)
	}
}

func TestFetchGitHubPrivateAsset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token sekrit" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/o/r":
			io.WriteString(w, `{"private": true}`)
		case "/repos/o/r/releases/tags/v1.0":
			io.WriteString(w, `{"assets": [{"name": "tool.zip", "url": "https://api.example/assets/9"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := testDownloader(t, Options{GitHubAPI: server.URL, GitHubToken: "sekrit"})
	got, err := d.fetchGitHubPrivateAsset(context.Background(), "o", "r", "v1.0", "tool.zip")
	if err != nil || got != "https://api.example/assets/9" {
		t.Fatalf("private asset = %q, %v", got, err)
	}
}

func TestFetchGitHubPublicKeepsURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"private": false}`)
	}))
	defer server.Close()
	d := testDownloader(t, Options{GitHubAPI: server.URL, GitHubToken: "sekrit"})
	raw := "https://github.com/o/r/releases/download/v1.0/tool.zip"
	got, err := d.resolveURL(context.Background(), raw)
	if err != nil || got != raw {
		t.Fatalf("public URL = %q, %v", got, err)
	}
}

func TestManualRedirectPreservesFragment(t *testing.T) {
	var secondPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			secondPath = r.URL.Path
			io.WriteString(w, "payload")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	d := testDownloader(t, Options{})
	resp, current, err := d.doManual(context.Background(), server.URL+"/a#/dl.bin", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "payload" || secondPath != "/b" {
		t.Fatalf("redirect chain body=%q path=%q", body, secondPath)
	}
	if !containsFold(current, "#/dl.bin") {
		t.Errorf("fragment lost: %q", current)
	}
}
