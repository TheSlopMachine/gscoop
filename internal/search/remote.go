package search

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"gscoop/internal/config"
)

// GitHubAPIBase is the API root, overridable in tests.
var GitHubAPIBase = "https://api.github.com"

// RemoteResult is one remote-only match: app name and source bucket.
type RemoteResult struct {
	Name   string
	Source string
}

// GitHubToken resolves the API token with classic precedence, delegating
// to config.ResolveGitHubToken (lib/download.ps1:589-591). configToken
// carries the GH_TOKEN config value.
func GitHubToken(configToken string) string {
	return config.ResolveGitHubToken(os.Getenv("SCOOP_GH_TOKEN"), configToken, os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
}

// RateLimitMessage is printed when the GitHub API rate limit is exhausted
// (lib/download.ps1:593-600).
const RateLimitMessage = "GitHub API rate limit reached.\nPlease try again later or configure your API token using 'scoop config gh_token <your token>'."

// RateLimitReached reports whether the GitHub API rate limit is exhausted.
func RateLimitReached(client *http.Client, token string) (bool, error) {
	request, err := http.NewRequest(http.MethodGet, GitHubAPIBase+"/rate_limit", nil)
	if err != nil {
		return false, err
	}
	applyToken(request, token)
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	var payload struct {
		Rate struct {
			Remaining int `json:"remaining"`
		} `json:"rate"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return false, err
	}
	return payload.Rate.Remaining == 0, nil
}

// SearchRemote queries one known bucket repository through the GitHub
// git-trees API and returns matching manifest names. It mirrors
// libexec/scoop-search.ps1 search_remote.
func SearchRemote(client *http.Client, repoURL, query string, token string) ([]string, error) {
	_, literal := CompileQuery(query)
	owner, repo, ok := splitRepoPath(repoURL)
	if !ok {
		return nil, nil
	}
	link := fmt.Sprintf("%s/repos/%s/%s/git/trees/HEAD?recursive=1", GitHubAPIBase, owner, repo)
	request, err := http.NewRequest(http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	applyToken(request, token)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return nil, nil
	}
	var payload struct {
		Tree []struct {
			Path string `json:"path"`
		} `json:"tree"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	names := []string{}
	for _, node := range payload.Tree {
		if name, ok := matchTreePath(node.Path, query, literal); ok {
			names = append(names, name)
		}
	}
	return names, nil
}

// SearchRemotes queries every known bucket that is not installed locally,
// mirroring search_remotes.
func SearchRemotes(client *http.Client, registry map[string]string, local []string, query string, token string) []RemoteResult {
	installed := map[string]bool{}
	for _, name := range local {
		installed[strings.ToLower(name)] = true
	}
	out := []RemoteResult{}
	for name, repo := range registry {
		if installed[strings.ToLower(name)] {
			continue
		}
		matches, err := SearchRemote(client, repo, query, token)
		if err != nil || len(matches) == 0 {
			continue
		}
		for _, app := range matches {
			out = append(out, RemoteResult{Name: app, Source: name})
		}
	}
	return out
}

// matchTreePath applies the ^bucket/(.*query.*)\.json$ selection. Regex
// queries keep classic capture semantics; literal queries use
// case-insensitive containment.
func matchTreePath(path, query string, literal bool) (string, bool) {
	if literal {
		if !strings.HasPrefix(path, "bucket/") || !strings.HasSuffix(path, ".json") {
			return "", false
		}
		name := strings.TrimSuffix(strings.TrimPrefix(path, "bucket/"), ".json")
		if strings.Contains(name, "/") {
			return "", false
		}
		if !strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			return "", false
		}
		return name, true
	}
	expr, err := regexp.Compile(`(?i)^bucket/(.*` + query + `.*)\.json$`)
	if err != nil {
		return "", false
	}
	parts := expr.FindStringSubmatch(path)
	if parts == nil || strings.Contains(parts[1], "/") {
		return "", false
	}
	return parts[1], true
}

// splitRepoPath extracts owner and repository from a known-bucket URL,
// mirroring the search_remote path match.
func splitRepoPath(repoURL string) (owner, repo string, ok bool) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(repoURL), "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	slash := strings.LastIndex(trimmed, "/")
	if slash < 0 {
		return "", "", false
	}
	repo = trimmed[slash+1:]
	rest := trimmed[:slash]
	slash = strings.LastIndex(rest, "/")
	if slash < 0 {
		return "", "", false
	}
	owner = rest[slash+1:]
	if owner == "" || repo == "" || strings.ContainsAny(owner+repo, " /") {
		return "", "", false
	}
	return owner, repo, true
}

func applyToken(request *http.Request, token string) {
	if token != "" {
		request.Header.Set("Authorization", "token "+token)
	}
}

// DefaultClient is the HTTP client for remote search.
func DefaultClient() *http.Client {
	return &http.Client{Timeout: 60 * time.Second}
}
