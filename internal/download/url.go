// URL handling: filenames, special-URL rewrites, and redirect fragments.
//
// Filename rules mirror lib/download.ps1:691-711 exactly. url_filename
// takes the Split-Path leaf and strips the query, so a #/ fragment
// coerces the local filename. url_remote_filename recovers the original
// name for display, falling back from query tricks to version-like
// checks to the URL fragment.
//
// Special-URL rewrites mirror handle_special_urls (lib/download.ps1:604-641):
// SourceForge reshaping is pure; FossHub needs a JSON POST and private
// GitHub releases need API reads, so those take an HTTP client and are
// split into pure parsers plus network fetchers. Redirect fragment
// preservation mirrors Invoke-Download (lib/download.ps1:140-143).
package download

import (
	"fmt"
	"net/url"
	"regexp"
	"runtime"
	"strings"
)

// sourceforgeRe matches SourceForge download URLs for reshaping
// (lib/download.ps1:624). Matching is case-insensitive like -match.
var sourceforgeRe = regexp.MustCompile(`(?i)(?:downloads\.)?sourceforge\.net/projects?/([^/]+)/(?:files/)?(.*?)(?:$|/download|\?)`)

// fosshubRe matches FossHub URLs for the download API
// (lib/download.ps1:606).
var fosshubRe = regexp.MustCompile(`^(?:.*fosshub\.com/)(.*)(?:/|\?dwl=)(.*)$`)

// githubReleaseRe matches public GitHub release asset URLs that may need
// the private-asset rewrite when a token exists (lib/download.ps1:630).
var githubReleaseRe = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/([^/#]+)(.*)`)

// apiGitHubRe gates api.github.com authorization headers
// (lib/download.ps1:98).
var apiGitHubRe = regexp.MustCompile(`(?i)^https://api\.github\.com/repos`)

// refererExemptRe lists hosts that receive no Referer header
// (lib/download.ps1:95).
var refererExemptRe = regexp.MustCompile(`(?i)sourceforge\.net|portableapps\.com`)

// remoteQueryRe extracts the trailing token after ? or =
// (lib/download.ps1:701).
var remoteQueryRe = regexp.MustCompile(`.*[?=]+([\w._-]+)`)

// versionLikeRe detects bare version strings (lib/download.ps1:704).
var versionLikeRe = regexp.MustCompile(`^[v.\d]+$`)

// URLFilename returns the local filename for a URL: the Split-Path leaf
// with the query stripped (lib/download.ps1:691-693).
func URLFilename(raw string) string {
	leaf := raw
	if i := strings.LastIndexAny(leaf, `/\`); i >= 0 {
		leaf = leaf[i+1:]
	}
	if i := strings.Index(leaf, "?"); i >= 0 {
		leaf = leaf[:i]
	}
	return leaf
}

// URLRemoteFilename returns the original filename for display, immune to
// #/ coercion tricks (lib/download.ps1:695-711).
func URLRemoteFilename(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return URLFilename(raw)
	}
	pathAndQuery := parsed.Path
	if parsed.RawQuery != "" {
		pathAndQuery += "?" + parsed.RawQuery
	}
	basename := pathAndQuery
	if i := strings.LastIndexAny(basename, `/\`); i >= 0 {
		basename = basename[i+1:]
	}
	if m := remoteQueryRe.FindStringSubmatch(basename); m != nil {
		basename = m[1]
	}
	if !strings.Contains(basename, ".") || versionLikeRe.MatchString(basename) {
		absolute := parsed.EscapedPath()
		leaf := absolute
		if i := strings.LastIndexAny(leaf, `/\`); i >= 0 {
			leaf = leaf[i+1:]
		}
		basename = leaf
	}
	if !strings.Contains(basename, ".") && parsed.Fragment != "" {
		basename = strings.Trim(parsed.Fragment, "/#")
	}
	return basename
}

// Base returns the Split-Path leaf (fname, lib/core.ps1:618).
func Base(path string) string {
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		return path[i+1:]
	}
	return path
}

// StripFilename removes the trailing filename, leaving the base URL
// (strip_filename, lib/core.ps1:620).
func StripFilename(path string) string {
	return strings.ReplaceAll(path, Base(path), "")
}

// StripExt removes the last extension (strip_ext, lib/core.ps1:619).
func StripExt(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[:i]
	}
	return name
}

// StripFragment removes the URL fragment (strip_fragment,
// lib/core.ps1:621).
func StripFragment(raw string) string {
	if i := strings.Index(raw, "#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// WireURL returns the request URL: everything before the first #
// (Invoke-Download, lib/download.ps1:91).
func WireURL(raw string) string {
	return StripFragment(raw)
}

// RedirectFragment extracts the #/ postfix preserved across redirects:
// the segment after the first #/ (lib/download.ps1:140-143).
func RedirectFragment(raw string) string {
	idx := strings.Index(raw, "#/")
	if idx < 0 {
		return ""
	}
	postfix := strings.Split(raw, "#/")
	if len(postfix) < 2 {
		return ""
	}
	return postfix[1]
}

// JoinRedirectFragment appends a preserved #/ postfix to a redirect
// target (lib/download.ps1:140-143).
func JoinRedirectFragment(location, fragment string) string {
	if fragment == "" {
		return location
	}
	return location + "#/" + fragment
}

// IsFTPURL reports ftp-scheme URLs, which the native downloader rejects
// with an explicit error (plan section 5.2).
func IsFTPURL(raw string) bool {
	return strings.HasPrefix(strings.ToLower(raw), "ftp:")
}

// IsHTTPURL reports http/https-scheme URLs.
func IsHTTPURL(raw string) bool {
	lowered := strings.ToLower(raw)
	return strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://")
}

// CookieHeader joins cookies as name=value pairs (cookie_header,
// lib/download.ps1:538-546). Keys sort for determinism.
func CookieHeader(cookies map[string]string) string {
	if len(cookies) == 0 {
		return ""
	}
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, ";")
}

// UserAgent identifies requests like classic (Get-UserAgent,
// lib/download.ps1:556-558), with the Go runtime in place of the
// PowerShell version.
func UserAgent() string {
	return fmt.Sprintf("Scoop/1.0 (+http://scoop.sh/) gscoop (%s %s/%s)", runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

// RewriteSourceForge reshapes a SourceForge URL to the direct mirror
// form, avoiding redirections (lib/download.ps1:623-627). It reports
// whether the URL matched.
func RewriteSourceForge(raw string) (string, bool) {
	m := sourceforgeRe.FindStringSubmatch(raw)
	if m == nil {
		return raw, false
	}
	return "https://downloads.sourceforge.net/project/" + m[1] + "/" + m[2], true
}

// FossHubParts parses a FossHub URL into project URI and filename
// (lib/download.ps1:606-616). The caller POSTs them to the download API.
func FossHubParts(raw string) (projectURI, fileName string, ok bool) {
	m := fosshubRe.FindStringSubmatch(raw)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// GitHubReleaseParts parses a public release asset URL into owner, repo,
// tag, file, and trailing fragment text (lib/download.ps1:630).
func GitHubReleaseParts(raw string) (owner, repo, tag, file, rest string, ok bool) {
	m := githubReleaseRe.FindStringSubmatch(raw)
	if m == nil {
		return "", "", "", "", "", false
	}
	return m[1], m[2], m[3], m[4], m[5], true
}

// WantsGitHubAPIHeaders reports api.github.com/repos URLs, which carry
// octet-stream Accept plus token authorization (lib/download.ps1:98-102).
func WantsGitHubAPIHeaders(raw string) bool {
	return apiGitHubRe.MatchString(raw)
}

// WantsReferer reports whether a Referer header applies: all HTTP URLs
// except SourceForge and portableapps (lib/download.ps1:95-97).
func WantsReferer(raw string) bool {
	return !refererExemptRe.MatchString(raw)
}
