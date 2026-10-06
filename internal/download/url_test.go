// Tests for URL handling: filenames, special-URL rewrites, redirect
// fragments, proxies, and cookies. Table inputs live in
// testdata/download/url-cases.json so cases grow without code edits.
// Classic basis: lib/download.ps1:91-147, 538-641, 691-711. No emojis.
package download

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type urlCases struct {
	Filenames []struct {
		URL  string `json:"url"`
		Want string `json:"want"`
	} `json:"filenames"`
	Remote []struct {
		URL  string `json:"url"`
		Want string `json:"want"`
	} `json:"remote"`
	SourceForge []struct {
		URL  string `json:"url"`
		Want string `json:"want"`
	} `json:"sourceforge"`
	FossHub []struct {
		URL     string `json:"url"`
		Project string `json:"project"`
		File    string `json:"file"`
	} `json:"fosshub"`
	GitHub []struct {
		URL   string `json:"url"`
		Owner string `json:"owner"`
		Repo  string `json:"repo"`
		Tag   string `json:"tag"`
		File  string `json:"file"`
		Rest  string `json:"rest"`
	} `json:"github"`
	Redirects []struct {
		URL      string `json:"url"`
		Location string `json:"location"`
		Want     string `json:"want"`
	} `json:"redirects"`
	Proxies []struct {
		Setting      string `json:"setting"`
		Mode         string `json:"mode"`
		Address      string `json:"address"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		DefaultCreds bool   `json:"defaultCreds"`
	} `json:"proxies"`
	Cookies []struct {
		Cookies map[string]string `json:"cookies"`
		Want    string            `json:"want"`
	} `json:"cookies"`
}

func loadURLCases(t *testing.T) urlCases {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "download", "url-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases urlCases
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestURLFilename(t *testing.T) {
	for _, c := range loadURLCases(t).Filenames {
		if got := URLFilename(c.URL); got != c.Want {
			t.Errorf("URLFilename(%q) = %q, want %q", c.URL, got, c.Want)
		}
	}
}

func TestURLRemoteFilename(t *testing.T) {
	for _, c := range loadURLCases(t).Remote {
		if got := URLRemoteFilename(c.URL); got != c.Want {
			t.Errorf("URLRemoteFilename(%q) = %q, want %q", c.URL, got, c.Want)
		}
	}
}

func TestRewriteSourceForge(t *testing.T) {
	for _, c := range loadURLCases(t).SourceForge {
		got, ok := RewriteSourceForge(c.URL)
		if c.Want == "" {
			if ok {
				t.Errorf("RewriteSourceForge(%q) matched, want no match", c.URL)
			}
			continue
		}
		if !ok || got != c.Want {
			t.Errorf("RewriteSourceForge(%q) = %q, %v; want %q", c.URL, got, ok, c.Want)
		}
	}
}

func TestFossHubParts(t *testing.T) {
	for _, c := range loadURLCases(t).FossHub {
		project, file, ok := FossHubParts(c.URL)
		if c.Project == "" {
			if ok {
				t.Errorf("FossHubParts(%q) matched, want no match", c.URL)
			}
			continue
		}
		if !ok || project != c.Project || file != c.File {
			t.Errorf("FossHubParts(%q) = %q, %q; want %q, %q", c.URL, project, file, c.Project, c.File)
		}
	}
}

func TestGitHubReleaseParts(t *testing.T) {
	for _, c := range loadURLCases(t).GitHub {
		owner, repo, tag, file, rest, ok := GitHubReleaseParts(c.URL)
		if c.Owner == "" {
			if ok {
				t.Errorf("GitHubReleaseParts(%q) matched, want no match", c.URL)
			}
			continue
		}
		if !ok || owner != c.Owner || repo != c.Repo || tag != c.Tag || file != c.File || rest != c.Rest {
			t.Errorf("GitHubReleaseParts(%q) = %q %q %q %q %q", c.URL, owner, repo, tag, file, rest)
		}
	}
}

func TestJoinRedirectFragment(t *testing.T) {
	for _, c := range loadURLCases(t).Redirects {
		fragment := RedirectFragment(c.URL)
		if got := JoinRedirectFragment(c.Location, fragment); got != c.Want {
			t.Errorf("JoinRedirectFragment(%q, %q) = %q, want %q", c.Location, fragment, got, c.Want)
		}
	}
}

func TestParseProxySetting(t *testing.T) {
	for _, c := range loadURLCases(t).Proxies {
		cfg, err := ParseProxySetting(c.Setting)
		if c.Mode == "error" {
			if err == nil {
				t.Errorf("ParseProxySetting(%q) succeeded, want error", c.Setting)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseProxySetting(%q) error: %v", c.Setting, err)
			continue
		}
		var mode string
		switch cfg.Mode {
		case ProxySystem:
			mode = "system"
		case ProxyDirect:
			mode = "direct"
		case ProxyCustom:
			mode = "custom"
		}
		if mode != c.Mode || cfg.Address != c.Address || cfg.Username != c.Username || cfg.Password != c.Password || cfg.UseDefaultCredentials != c.DefaultCreds {
			t.Errorf("ParseProxySetting(%q) = %+v, want mode=%s addr=%q user=%q pass=%q creds=%v",
				c.Setting, cfg, c.Mode, c.Address, c.Username, c.Password, c.DefaultCreds)
		}
	}
}

func TestCookieHeader(t *testing.T) {
	for _, c := range loadURLCases(t).Cookies {
		if got := CookieHeader(c.Cookies); got != c.Want {
			t.Errorf("CookieHeader(%v) = %q, want %q", c.Cookies, got, c.Want)
		}
	}
}

func TestStripHelpers(t *testing.T) {
	if got := StripFilename("https://example.com/dir/app.zip"); got != "https://example.com/dir/" {
		t.Errorf("StripFilename = %q", got)
	}
	if got := StripExt("archive.tar.gz"); got != "archive.tar" {
		t.Errorf("StripExt = %q", got)
	}
	if got := StripFragment("https://h/a.zip#/dl.7z"); got != "https://h/a.zip" {
		t.Errorf("StripFragment = %q", got)
	}
	if IsFTPURL("ftp://h/f.zip") != true || IsFTPURL("https://h/f.zip") != false {
		t.Error("IsFTPURL mismatch")
	}
	if !WantsGitHubAPIHeaders("https://api.github.com/repos/o/r") || WantsGitHubAPIHeaders("https://github.com/o/r") {
		t.Error("WantsGitHubAPIHeaders mismatch")
	}
	if WantsReferer("https://downloads.sourceforge.net/project/a/b") || !WantsReferer("https://example.com/a.zip") {
		t.Error("WantsReferer mismatch")
	}
}
