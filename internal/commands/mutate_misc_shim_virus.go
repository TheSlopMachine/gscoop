// Shim and virustotal verbs for Phase 3A (internal/commands/mutate_misc_*.go).
//
// RunShim mirrors libexec/scoop-shim.ps1: subcommands add, rm, list,
// info, alter with -g/--global and the --/--% terminator rule.
// RunVirusTotal mirrors libexec/scoop-virustotal.ps1: hash and URL
// reports through the VirusTotal v3 API with the classic combined
// exit codes. No emojis.
package commands

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/TheSlopMachine/gscoop/internal/cli"
	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/shim"
	"github.com/TheSlopMachine/gscoop/internal/update"
)

// RunShim mirrors libexec/scoop-shim.ps1 argument handling.
func RunShim(env *Env, out io.Writer, args []string) int {
	if len(args) == 0 {
		Errorf(out, "<subcommand> missing")
		printUsage(out, "shim")
		return 1
	}
	sub, rest := args[0], args[1:]
	if sub != "add" && sub != "rm" && sub != "list" && sub != "info" && sub != "alter" {
		Errorf(out, "'%s' is not one of available subcommands: add, rm, list, info, alter", sub)
		printUsage(out, "shim")
		return 1
	}
	r := cli.GetOpt(rest, "g", []string{"global"})
	if r.Err != "" {
		Errorf(out, "scoop shim: %s", r.Err)
		return 1
	}
	global := r.Has("g") || r.Has("global")
	other := r.Rest
	if sub != "list" && len(other) == 0 {
		Errorf(out, "<shim_name> must be specified for subcommand '%s'", sub)
		printUsage(out, "shim")
		return 1
	}
	switch sub {
	case "add":
		return shimAdd(env, out, other, global)
	case "rm":
		return shimRemove(env, out, other, global)
	case "list":
		return shimList(env, out, other, global)
	case "info":
		return shimInfo(env, out, other[0], global)
	default:
		return shimAlter(env, out, other, global)
	}
}

// shimFile finds name.shim or name.ps1 under the scope shim dir.
func shimFile(env *Env, name string, global bool) string {
	dir := env.ShimDir(global)
	for _, suffix := range []string{".shim", ".ps1"} {
		p := filepath.Join(dir, strings.ToLower(name)+suffix)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// shimScopeLabel names the scope for messages.
func shimScopeLabel(global bool) string {
	if global {
		return "Global"
	}
	return "Local"
}

// shimAdd mirrors the add branch (scoop-shim.ps1:97-125).
func shimAdd(env *Env, out io.Writer, other []string, global bool) int {
	if len(other) < 2 || other[1] == "" {
		Errorf(out, "<command_path> must be specified for subcommand 'add'")
		printUsage(out, "shim")
		return 1
	}
	name, commandPath := other[0], other[1]
	var commandArgs string
	if len(other) > 2 {
		commandArgs = strings.Join(other[2:], " ")
	}
	if !strings.ContainsAny(commandPath, `/\`) {
		if resolved := shim.GetShimTarget(shimFile(env, commandPath, global)); resolved != "" {
			commandPath = resolved
		} else if found, err := exec.LookPath(commandPath); err == nil {
			commandPath = found
		}
	}
	if commandPath == "" {
		Errorf(out, "Command path does not exist: %s", other[1])
		return 3
	}
	if _, err := os.Stat(commandPath); err != nil {
		Errorf(out, "Command path does not exist: %s", other[1])
		return 3
	}
	if global {
		fmt.Fprintf(out, "Adding global shim %s...\n", name)
	} else {
		fmt.Fprintf(out, "Adding local shim %s...\n", name)
	}
	shimDir := env.ShimDir(global)
	lowered := strings.ToLower(commandPath)
	if strings.HasSuffix(lowered, ".exe") || strings.HasSuffix(lowered, ".com") {
		rewritten, err := shim.WriteExe(shimDir, name, commandPath, shimVariant(env))
		if err != nil {
			Errorf(out, "%s", err.Error())
			return 1
		}
		if err := shim.WriteTextShim(shimDir, name, rewritten, commandArgs); err != nil {
			Errorf(out, "%s", err.Error())
			return 1
		}
		return 0
	}
	if err := shim.Wrappers(shimDir, name, commandPath, commandArgs); err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	return 0
}

func shimVariant(env *Env) string {
	if store, err := config.Load(env.ConfigPath); err == nil {
		if v, ok := store.GetString("shim"); ok && v != "" {
			return v
		}
	}
	return "kiennq"
}

// shimRemove mirrors the rm branch (scoop-shim.ps1:126-141).
func shimRemove(env *Env, out io.Writer, other []string, global bool) int {
	var failed []string
	for _, name := range other {
		if shimFile(env, name, global) != "" {
			for _, line := range shim.RemoveShim(env.ShimDir(global), strings.ToLower(name), "") {
				fmt.Fprintln(out, line)
			}
		} else {
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		for _, name := range failed {
			Errorf(out, "%s shim not found: %s", shimScopeLabel(global), name)
		}
		return 3
	}
	return 0
}

// shimDescribe builds the info row for one shim path, mirroring
// Get-ShimInfo (scoop-shim.ps1:72-85).
type shimDescribe struct {
	name         string
	path         string
	source       string
	kind         string
	alternatives []string
	global       bool
}

func describeShim(env *Env, shimPath string, global bool) shimDescribe {
	base := filepath.Base(shimPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	exe := shimPath
	if strings.HasSuffix(strings.ToLower(shimPath), ".shim") {
		exe = strings.TrimSuffix(shimPath, filepath.Ext(shimPath)) + ".exe"
	}
	source := ownerFromPath(env, shim.GetShimTarget(shimPath))
	if source == "" {
		source = "External"
	}
	kind := "Application"
	if strings.HasSuffix(strings.ToLower(shimPath), ".ps1") {
		kind = "ExternalScript"
	}
	var alternatives []string
	matches, _ := filepath.Glob(shimPath + ".*")
	for _, m := range matches {
		ext := strings.TrimPrefix(filepath.Ext(m), ".")
		if ext == "shim" || ext == "cmd" || ext == "ps1" {
			continue
		}
		alternatives = append(alternatives, ext)
	}
	sort.Strings(alternatives)
	alternatives = append([]string{source}, alternatives...)
	return shimDescribe{name: name, path: exe, source: source, kind: kind, alternatives: alternatives, global: global}
}

// ownerFromPath mirrors get_app_name (lib/core.ps1:892-901): the app
// segment under either scope apps dir, lowercased.
func ownerFromPath(env *Env, target string) string {
	for _, dir := range []string{env.AppsDir(false), env.AppsDir(true)} {
		rel, err := filepath.Rel(dir, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if first := strings.Split(rel, string(filepath.Separator))[0]; first != "" && first != "." {
			return strings.ToLower(first)
		}
	}
	return ""
}

// shimList mirrors the list branch (scoop-shim.ps1:142-167).
func shimList(env *Env, out io.Writer, other []string, global bool) int {
	patterns := []string{}
	for _, p := range other {
		if p == "*" {
			continue
		}
		patterns = append(patterns, p)
	}
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			Errorf(out, "Invalid pattern: %s", p)
			return 1
		}
	}
	joined := strings.Join(patterns, "|")
	var paths []string
	var scopes []bool
	locals, _ := filepath.Glob(filepath.Join(env.ShimDir(false), "*.shim"))
	localsPS, _ := filepath.Glob(filepath.Join(env.ShimDir(false), "*.ps1"))
	for _, p := range append(locals, localsPS...) {
		if joined == "" || regexp.MustCompile(joined).MatchString(shimBase(p)) {
			paths = append(paths, p)
			scopes = append(scopes, false)
		}
	}
	if fi, err := os.Stat(env.ShimDir(true)); err == nil && fi.IsDir() {
		globals, _ := filepath.Glob(filepath.Join(env.ShimDir(true), "*.shim"))
		globalsPS, _ := filepath.Glob(filepath.Join(env.ShimDir(true), "*.ps1"))
		for _, p := range append(globals, globalsPS...) {
			if joined == "" || regexp.MustCompile(joined).MatchString(shimBase(p)) {
				paths = append(paths, p)
				scopes = append(scopes, true)
			}
		}
	}
	table := make([][]string, 0, len(paths))
	for i, p := range paths {
		d := describeShim(env, p, scopes[i])
		scope := "user"
		if d.global {
			scope = "global"
		}
		table = append(table, []string{d.name, d.source, d.kind, scope})
	}
	sort.Slice(table, func(i, j int) bool { return table[i][0] < table[j][0] })
	renderTable(out, []string{"Name", "Source", "Type", "Scope"}, table)
	return 0
}

func shimBase(p string) string {
	base := filepath.Base(p)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// shimInfo mirrors the info branch (scoop-shim.ps1:168-181).
func shimInfo(env *Env, out io.Writer, name string, global bool) int {
	path := shimFile(env, name, global)
	if path == "" {
		Errorf(out, "%s shim not found: %s", shimScopeLabel(global), name)
		if shimFile(env, name, !global) != "" {
			other := ""
			if !global {
				other = " --global"
			}
			fmt.Fprintf(out, "But a %s shim exists, run 'scoop shim info %s%s' to show its info.\n", oppositeScope(global), name, other)
			return 2
		}
		return 3
	}
	d := describeShim(env, path, global)
	fmt.Fprintf(out, "Name: %s\nPath: %s\nSource: %s\nType: %s\nAlternatives: %s\n", d.name, d.path, d.source, d.kind, strings.Join(d.alternatives, " "))
	return 0
}

func oppositeScope(global bool) string {
	if global {
		return "local"
	}
	return "global"
}

// shimAlter mirrors the alter branch (scoop-shim.ps1:182-223). With an
// extra source argument it swaps the primary to that alternative;
// without it the alternatives print for selection.
func shimAlter(env *Env, out io.Writer, other []string, global bool) int {
	name := other[0]
	path := shimFile(env, name, global)
	if path == "" {
		Errorf(out, "%s shim not found: %s", shimScopeLabel(global), name)
		if shimFile(env, name, !global) != "" {
			other := ""
			if !global {
				other = " --global"
			}
			fmt.Fprintf(out, "But a %s shim exists, run 'scoop shim alter %s%s' to alternate its source.\n", oppositeScope(global), name, other)
			return 2
		}
		return 3
	}
	d := describeShim(env, path, global)
	if len(d.alternatives) < 2 {
		Errorf(out, "No alternatives of %s found.", name)
		return 2
	}
	if len(other) < 2 {
		fmt.Fprintf(out, "Alternatives of '%s': %s\n", name, strings.Join(d.alternatives, " "))
		return 0
	}
	target := other[1]
	found := false
	for _, alt := range d.alternatives {
		if alt == target {
			found = true
		}
	}
	if !found {
		Errorf(out, "No alternative '%s' for shim '%s'.", target, name)
		return 2
	}
	if target == d.source {
		fmt.Fprintf(out, "%s is already from %s, nothing changed.\n", name, d.source)
		return 0
	}
	fmt.Fprintf(out, "Use %s from %s as default... ", name, target)
	stripExt := strings.TrimSuffix(path, filepath.Ext(path))
	for _, suffix := range []string{"", ".shim", ".cmd", ".ps1"} {
		oldPrimary := stripExt + suffix
		newPrimary := oldPrimary + "." + target
		if _, err := os.Stat(oldPrimary); err != nil {
			continue
		}
		_ = os.Rename(oldPrimary, oldPrimary+"."+d.source)
		if _, err := os.Stat(newPrimary); err == nil {
			_ = os.Rename(newPrimary, oldPrimary)
		}
	}
	fmt.Fprintln(out, "Done.")
	return 0
}

// VirusTotal exit bits (libexec/scoop-virustotal.ps1:60-63).
const (
	vtUnsafe     = 2
	vtException  = 4
	vtNoInfo     = 8
	vtNoAPIKey   = 16
	vtErrTooMany = 429
)

// vtTarget is one manifest URL plus its manifest hash.
type vtTarget struct {
	url  string
	hash string
}

// vtRequest performs one VirusTotal API call. Tests override it.
var vtRequest = func(method, rawURL, apiKey string, body io.Reader) (int, []byte, error) {
	var req *http.Request
	var err error
	if body == nil {
		req, err = http.NewRequest(method, rawURL, nil)
	} else {
		req, err = http.NewRequest(method, rawURL, body)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-apikey", apiKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// RunVirusTotal mirrors libexec/scoop-virustotal.ps1: -a/--all,
// -s/--scan, -n/--no-depends, -u/--no-update-scoop, -p/--passthru.
func RunVirusTotal(env *Env, out io.Writer, args []string) int {
	r := cli.GetOpt(args, "asnup", []string{"all", "scan", "no-depends", "no-update-scoop", "passthru"})
	if r.Err != "" {
		Errorf(out, "scoop virustotal: %s", r.Err)
		return 1
	}
	all := false
	for _, a := range r.Rest {
		if a == "*" {
			all = true
		}
	}
	all = all || r.Has("a") || r.Has("all")
	apps := r.Rest
	if len(apps) == 0 && !all {
		printUsage(out, "virustotal")
		return 1
	}
	store, err := config.Load(env.ConfigPath)
	if err != nil {
		Errorf(out, "%s", err.Error())
		return 1
	}
	if outdated := update.ScoopOutdated(store, time.Now()); outdated {
		if r.Has("u") || r.Has("no-update-scoop") {
			Warnf(out, "Scoop is out of date.")
		} else if code := RunUpdate(env, out, []string{}); code != 0 {
			Warnf(out, "Scoop is out of date.")
		}
	}
	if all {
		apps = append(env.InstalledApps(false), env.InstalledApps(true)...)
	}
	if !(r.Has("n") || r.Has("no-depends")) {
		var expanded []string
		for _, app := range apps {
			resolved, err := env.ResolveDepends(update.StripBucket(app), env.Arch, out)
			if err != nil {
				expanded = append(expanded, app)
				continue
			}
			for _, dep := range resolved {
				expanded = append(expanded, update.StripBucket(dep))
			}
		}
		apps = uniqueArgs(expanded)
	}
	apiKey, _ := store.GetString("virustotal_api_key")
	if strings.TrimSpace(apiKey) == "" {
		Errorf(out, "VirusTotal API key is not configured\n  You could get one from https://www.virustotal.com/gui/my-apikey and set with\n  scoop config virustotal_api_key <API key>")
		return vtNoAPIKey
	}
	code := 0
	var reports []string
	for _, app := range uniqueArgs(apps) {
		name := update.StripBucket(app)
		hit := env.FindManifest(name, out)
		if hit.Manifest == nil {
			code |= vtNoInfo
			Warnf(out, "%s: manifest not found", name)
			continue
		}
		targets := vtTargets(hit.Raw, env.Arch)
		for i, target := range targets {
			if len(targets) > 1 {
				Infof(out, "%s: url %d", name, i+1)
			}
			line, bits, fatal := checkVirusTotal(out, name, target, apiKey, r.Has("s") || r.Has("scan"))
			code |= bits
			if line != "" {
				reports = append(reports, line)
			}
			if fatal {
				return code
			}
		}
	}
	if r.Has("p") || r.Has("passthru") {
		for _, line := range reports {
			fmt.Fprintln(out, line)
		}
	}
	return code
}

// vtTargets extracts manifest download URLs plus aligned hashes for
// arch, mirroring script:url plus hash_for_url selection.
func vtTargets(raw []byte, arch string) []vtTarget {
	var doc struct {
		URL  any `json:"url"`
		Hash any `json:"hash"`
		Arch map[string]struct {
			URL  any `json:"url"`
			Hash any `json:"hash"`
		} `json:"architecture"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	urls, hashes := toStringList(doc.URL), toStringList(doc.Hash)
	if a, ok := doc.Arch[arch]; ok {
		if u := toStringList(a.URL); len(u) > 0 {
			urls = u
		}
		if h := toStringList(a.Hash); len(h) > 0 {
			hashes = h
		}
	}
	var out []vtTarget
	for i, u := range urls {
		t := vtTarget{url: u}
		if i < len(hashes) {
			t.hash = hashes[i]
		}
		out = append(out, t)
	}
	return out
}

func toStringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// vtURLID encodes a URL for the urls/:id endpoint.
func vtURLID(rawURL string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(rawURL))
	encoded = strings.ReplaceAll(encoded, "+", "-")
	encoded = strings.ReplaceAll(encoded, "/", "_")
	return strings.TrimRight(encoded, "=")
}

// checkVirusTotal runs the hash, URL, and optional scan steps for one
// artifact, mirroring the per-URL loop in scoop-virustotal.ps1. fatal
// mirrors abort on rate limiting (204/429).
func checkVirusTotal(out io.Writer, app string, target vtTarget, apiKey string, doScan bool) (string, int, bool) {
	hash, algo := splitHash(target.hash)
	unsupported := false
	if hash != "" {
		a := strings.ToLower(algo)
		if a != "md5" && a != "sha1" && a != "sha256" && algo != "" {
			Warnf(out, "%s: Unsupported hash %s. Will search by url instead.", app, algo)
			hash = ""
			unsupported = true
		}
		if algo == "" {
			algo = "sha256"
		}
	}
	var report string
	if hash != "" {
		line, bits, action := vtFileReport(out, app, target.url, hash, algo, apiKey)
		if action == "done" {
			return line, bits, false
		}
		if action == "abort" {
			return "", bits, true
		}
		report = line
	} else if !unsupported {
		Warnf(out, "%s: Hash not found. Will search by url instead.", app)
	}
	line, bits, fatal := vtURLReport(out, app, target, hash, algo, apiKey, doScan)
	if report != "" && line == "" {
		line = report
	}
	return line, bits, fatal
}

func splitHash(hash string) (string, string) {
	if i := strings.Index(hash, ":"); i >= 0 {
		return hash[i+1:], hash[:i]
	}
	if hash != "" {
		return hash, ""
	}
	return "", ""
}

// vtFileReport queries files/:hash and prints the vendor verdict.
func vtFileReport(out io.Writer, app, rawURL, hash, algo, apiKey string) (string, int, string) {
	status, body, err := vtRequest("GET", "https://www.virustotal.com/api/v3/files/"+strings.ToLower(hash), apiKey, nil)
	if err != nil {
		return "", vtException, "next"
	}
	if status == 404 {
		Warnf(out, "%s: File report not found. Will search by url instead.", app)
		return "", 0, "next"
	}
	if status == 204 || status == vtErrTooMany {
		Errorf(out, "%s: VirusTotal request failed: rate limited (%d)", app, status)
		return "", vtException, "abort"
	}
	if status != 200 {
		Warnf(out, "%s: VirusTotal request failed: status %d", app, status)
		return "", vtException, "next"
	}
	var doc struct {
		Data struct {
			Attributes struct {
				Stats struct {
					Malicious  int `json:"malicious"`
					Suspicious int `json:"suspicious"`
					Timeout    int `json:"timeout"`
					Undetected int `json:"undetected"`
				} `json:"last_analysis_stats"`
				Size   int64  `json:"size"`
				SHA256 string `json:"sha256"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		Warnf(out, "%s: VirusTotal request failed: %s", app, err.Error())
		return "", vtException, "next"
	}
	unsafe := doc.Data.Attributes.Stats.Malicious + doc.Data.Attributes.Stats.Suspicious
	total := unsafe + doc.Data.Attributes.Stats.Undetected
	reportURL := "https://www.virustotal.com/gui/file/" + doc.Data.Attributes.SHA256
	line := fmt.Sprintf("%s: %d/%d, see %s", app, unsafe, total, reportURL)
	bits := 0
	if unsafe > 0 {
		bits = vtUnsafe
	}
	if total == 0 {
		Infof(out, "%s: Analysis in progress.", app)
	} else if unsafe == 0 {
		Successf(out, "%s", line)
	} else {
		Warnf(out, "%s", line)
	}
	return line, bits, "done"
}

// vtURLReport queries urls/:id and optionally submits the URL for
// analysis, mirroring Get-VirusTotalResultByUrl plus Submit-ToVirusTotal.
func vtURLReport(out io.Writer, app string, target vtTarget, hash, algo, apiKey string, doScan bool) (string, int, bool) {
	id := vtURLID(target.url)
	status, body, err := vtRequest("GET", "https://www.virustotal.com/api/v3/urls/"+id, apiKey, nil)
	if err != nil {
		Warnf(out, "%s: VirusTotal request failed: %s", app, err.Error())
		return "", vtException, false
	}
	if status == 204 || status == vtErrTooMany {
		Errorf(out, "%s: VirusTotal request failed: rate limited (%d)", app, status)
		return "", vtException, true
	}
	if status == 404 {
		Warnf(out, "%s: Url report not found. Will submit %s", app, target.url)
		return vtSubmitURL(out, app, target.url, apiKey, doScan), 0, false
	}
	if status != 200 {
		Warnf(out, "%s: VirusTotal request failed: status %d", app, status)
		return "", vtException, false
	}
	var doc struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				FileSHA256 *string `json:"last_http_response_content_sha256"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		Warnf(out, "%s: VirusTotal request failed: %s", app, err.Error())
		return "", vtException, false
	}
	reportURL := "https://www.virustotal.com/gui/url/" + doc.Data.ID
	Infof(out, "%s: Url report found.", app)
	line := fmt.Sprintf("%s: url report %s", app, reportURL)
	if doc.Data.Attributes.FileSHA256 == nil {
		Infof(out, "%s: Analysis in progress.", app)
		return line, 0, false
	}
	Infof(out, "%s: Related file report found.", app)
	if hash != "" && strings.EqualFold(algo, "sha256") && !strings.EqualFold(*doc.Data.Attributes.FileSHA256, hash) {
		Errorf(out, "%s: Hash not matched for %s", app, target.url)
		return line, vtUnsafe, false
	}
	nested, bits, action := vtFileReport(out, app, target.url, *doc.Data.Attributes.FileSHA256, "sha256", apiKey)
	if action == "abort" {
		return "", bits, true
	}
	if nested != "" {
		return nested, bits, false
	}
	return line, bits, false
}

// vtSubmitURL posts the URL for analysis when --scan is set.
func vtSubmitURL(out io.Writer, app, rawURL, apiKey string, doScan bool) string {
	if !doScan {
		Warnf(out, "%s: not found: you can manually submit %s", app, rawURL)
		return ""
	}
	status, body, err := vtRequest("POST", "https://www.virustotal.com/api/v3/urls", apiKey, strings.NewReader(url.Values{"url": {rawURL}}.Encode()))
	if err != nil {
		Warnf(out, "%s: VirusTotal submission failed: %s", app, err.Error())
		return ""
	}
	if status != 200 {
		Warnf(out, "%s: VirusTotal submission of %s failed: API returned %d after retrying", app, rawURL, status)
		return ""
	}
	var doc struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		Warnf(out, "%s: VirusTotal submission failed: %s", app, err.Error())
		return ""
	}
	id := doc.Data.ID
	if i := strings.Index(id, "-"); i >= 0 {
		parts := strings.Split(id, "-")
		id = parts[1]
	}
	line := fmt.Sprintf("%s: analysis in progress: https://www.virustotal.com/gui/url/%s", app, id)
	Infof(out, "%s: Analysis in progress.", app)
	return line
}
