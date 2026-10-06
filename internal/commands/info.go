package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/TheSlopMachine/gscoop/internal/cli"
)

var licenseURL = regexp.MustCompile(`^((ht)|f)tps?://`)

// RunInfo mirrors libexec/scoop-info.ps1: manifest fields, install state,
// and the bucket git date when available. Remote download-size lookups need
// the download layer and are omitted in Phase 1C.
func RunInfo(env *Env, out io.Writer, args []string) int {
	cmd := cli.Lookup("info")
	r := cli.GetOpt(args, cmd.ShortOpts, cmd.LongOpts)
	if r.Err != "" {
		Errorf(out, "scoop info: %s", r.Err)
		return 1
	}
	if len(r.Rest) == 0 {
		printUsage(out, "info")
		return 1
	}
	verbose := r.Has("v") || r.Has("verbose")
	return env.infoApp(out, r.Rest[0], verbose)
}

func (e *Env) infoApp(out io.Writer, spec string, verbose bool) int {
	hit := e.FindManifest(spec, out)
	if hit.Manifest == nil {
		fmt.Fprintf(out, "Could not find manifest for '%s' in local buckets.\n", spec)
		return 1
	}
	app := hit.Name
	m := hit.Manifest
	global := e.Installed(app, boolPtr(true))
	st := e.AppStatusFor(app, global)
	var install *InstallInfo
	if info, _ := e.InstallInfoFor(app, st.Version, global); info != nil {
		install = info
	}
	installed := (hit.Bucket != "" && install != nil && install.Bucket == hit.Bucket) || e.Installed(app, nil)
	arch := ""
	if install != nil {
		arch = install.Architecture
	}

	standalone, sameSource, origin := standaloneSource(spec, install)
	versionOutput := m.Version
	manifestFile := hit.Path
	if st.Deprecated != "" {
		manifestFile = st.Deprecated
	} else if installed && install != nil && install.URL != "" && !isURL(install.URL) {
		manifestFile = install.URL
	}
	if installed {
		switch {
		case st.Deprecated != "":
		case standalone && !sameSource:
		case st.Version == m.Version:
			versionOutput = st.Version
		default:
			versionOutput = fmt.Sprintf("%s (Update to %s available)", st.Version, m.Version)
		}
	}

	dir, originalDir, persistDir := "<root>", "<root>", "<root>"
	if verbose {
		dir = e.CurrentDir(app, global)
		originalDir = e.VersionDir(app, m.Version, global)
		persistDir = e.PersistDir(app, global)
	}

	var rows [][2]string
	name := app
	if st.Deprecated != "" {
		name += " (DEPRECATED)"
	}
	rows = append(rows, [2]string{"Name", name})
	if m.Description != "" {
		rows = append(rows, [2]string{"Description", m.Description})
	}
	rows = append(rows, [2]string{"Version", versionOutput})
	source := hit.Bucket
	if standalone {
		source = origin
	} else if install != nil && install.Bucket != "" {
		source = install.Bucket
	} else if install != nil && install.URL != "" {
		source = install.URL
	}
	rows = append(rows, [2]string{"Source", source})
	if m.Homepage != "" {
		rows = append(rows, [2]string{"Website", strings.TrimRight(m.Homepage, "/")})
	}
	if license, ok := licenseString(m.License, verbose); ok {
		rows = append(rows, [2]string{"License", license})
	}
	if deps := m.DependsList(); len(deps) > 0 {
		rows = append(rows, [2]string{"Dependencies", strings.Join(deps, " | ")})
	}
	if manifestFile != "" && !isURL(manifestFile) {
		if fi, err := os.Stat(manifestFile); err == nil && !fi.IsDir() {
			when := formatTime(fi.ModTime())
			by := ""
			if w, a, ok := gitFileInfo(manifestFile); ok {
				when, by = w, a
			}
			rows = append(rows, [2]string{"Updated at", when})
			if by != "" {
				rows = append(rows, [2]string{"Updated by", by})
			}
		}
	}
	if verbose {
		rows = append(rows, [2]string{"Manifest", manifestFile})
	}
	if installed && (!standalone || sameSource) {
		var lines []string
		for _, v := range e.InstalledVersions(app, global) {
			if verbose {
				lines = append(lines, e.VersionDir(app, v, global))
			} else if global {
				lines = append(lines, v+" *global*")
			} else {
				lines = append(lines, v)
			}
		}
		if len(lines) > 0 {
			rows = append(rows, [2]string{"Installed", strings.Join(lines, "\n")})
		}
		if verbose {
			rows = append(rows, [2]string{"Installed size", e.installedSize(app, global)})
		}
	}
	if binaries := binaryDisplay(m.BinList(arch)); binaries != "" {
		rows = append(rows, [2]string{"Binaries", binaries})
	}
	if shortcuts := shortcutDisplay(m.ShortcutsList(arch)); shortcuts != "" {
		rows = append(rows, [2]string{"Shortcuts", shortcuts})
	}
	if envSet := m.EnvSetFor(arch); len(envSet) > 0 {
		keys := make([]string, 0, len(envSet))
		for k := range envSet {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var lines []string
		for _, k := range keys {
			value, ok := envSet[k].(string)
			if !ok {
				value = fmt.Sprintf("%v", envSet[k])
			} else {
				value = substituteVars(value, dir, originalDir, persistDir)
			}
			lines = append(lines, k+" = "+value)
		}
		rows = append(rows, [2]string{"Environment", strings.Join(lines, "\n")})
	}
	if paths := envAddPathDisplay(m.EnvAddPathFor(arch), dir); paths != "" {
		rows = append(rows, [2]string{"Path Added", paths})
	}
	if suggest := suggestDisplay(m.Suggest); len(suggest) > 0 {
		rows = append(rows, [2]string{"Suggestions", strings.Join(suggest, " | ")})
	}
	if notes := notesDisplay(m.Notes, dir, originalDir, persistDir); notes != "" {
		rows = append(rows, [2]string{"Notes", notes})
	}
	for _, row := range rows {
		fmt.Fprintf(out, "%s: %s\n", row[0], row[1])
	}
	return 0
}

func boolPtr(v bool) *bool {
	return &v
}

// standaloneSource mirrors the standalone detection in scoop-info.ps1:
// a spec naming a local file or a URL. It returns whether the spec is
// standalone, whether it matches the install source, and the display path.
func standaloneSource(spec string, install *InstallInfo) (standalone, sameSource bool, origin string) {
	origin = spec
	if fi, err := os.Stat(spec); err == nil && !fi.IsDir() {
		standalone = true
		if abs, err := filepath.Abs(spec); err == nil {
			origin = abs
		}
	} else if isURL(spec) {
		standalone = true
	}
	if standalone && install != nil && install.URL != "" {
		installURL := install.URL
		if fi, err := os.Stat(install.URL); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(install.URL); err == nil {
				installURL = abs
			}
		}
		sameSource = origin == installURL
	}
	return standalone, sameSource, origin
}

// EnvSetFor resolves env_set for arch, preferring
// architecture.<arch>.env_set over the top-level value.
func (m *Manifest) EnvSetFor(arch string) map[string]any {
	if m == nil {
		return nil
	}
	if a, ok := m.Architecture[arch]; ok && a.EnvSet != nil {
		return a.EnvSet
	}
	return m.EnvSet
}

// EnvAddPathFor resolves env_add_path for arch, preferring
// architecture.<arch>.env_add_path over the top-level value.
func (m *Manifest) EnvAddPathFor(arch string) any {
	if m == nil {
		return nil
	}
	if a, ok := m.Architecture[arch]; ok && a.EnvAddPath != nil {
		return a.EnvAddPath
	}
	return m.EnvAddPath
}

// licenseString renders the license field. String licenses print as-is,
// extended with SPDX links in verbose mode; identifier/url objects print
// the identifier, extended with the URL in verbose mode.
func licenseString(v any, verbose bool) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		if t == "" {
			return "", false
		}
		return expandLicense(t, verbose), true
	case map[string]any:
		id, _ := t["identifier"].(string)
		url, _ := t["url"].(string)
		if id != "" && url != "" {
			if verbose {
				return id + " (" + url + ")", true
			}
			return id, true
		}
		if id != "" {
			return expandLicense(id, verbose), true
		}
		return "", false
	default:
		return "", false
	}
}

func expandLicense(s string, verbose bool) string {
	if licenseURL.MatchString(s) {
		return s
	}
	if strings.ContainsAny(s, "|,") {
		if !verbose {
			return s
		}
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == '|' || r == ',' })
		var urls []string
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				urls = append(urls, "https://spdx.org/licenses/"+p+".html")
			}
		}
		return s + " (" + strings.Join(urls, ", ") + ")"
	}
	if verbose {
		return s + " (https://spdx.org/licenses/" + s + ".html)"
	}
	return s
}

// binaryDisplay mirrors the Binaries rendering in scoop-info.ps1: plain
// strings print as-is, [exe, alias, args] triples print as alias plus the
// executable extension.
func binaryDisplay(entries []any) string {
	var out []string
	for _, entry := range entries {
		switch t := entry.(type) {
		case string:
			out = append(out, t)
		case []any:
			if len(t) < 2 {
				if len(t) == 1 {
					if s, ok := t[0].(string); ok {
						out = append(out, s)
					}
				}
				continue
			}
			exe, _ := t[0].(string)
			alias, _ := t[1].(string)
			if exe == "" || alias == "" {
				if exe != "" {
					out = append(out, exe)
				}
				continue
			}
			ext := exe
			if i := strings.LastIndex(exe, "."); i >= 0 {
				ext = exe[i+1:]
			}
			out = append(out, alias+"."+ext)
		}
	}
	return strings.Join(out, " | ")
}

// shortcutDisplay mirrors the Shortcuts rendering: the display name of
// each [target, name, args?, icon?] entry.
func shortcutDisplay(entries []any) string {
	var out []string
	for _, entry := range entries {
		parts, ok := entry.([]any)
		if !ok || len(parts) < 2 {
			continue
		}
		if name, ok := parts[1].(string); ok && name != "" {
			out = append(out, name)
		}
	}
	return strings.Join(out, " | ")
}

// suggestDisplay flattens suggest values in sorted key order. Key order
// follows the manifest document in classic; sorting keeps output
// deterministic without an ordered map in this seam.
//
// TODO(manifest): preserve document order via internal/manifest.
func suggestDisplay(suggest map[string]any) []string {
	if len(suggest) == 0 {
		return nil
	}
	keys := make([]string, 0, len(suggest))
	for k := range suggest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, anyToStrings(suggest[k])...)
	}
	return out
}

func notesDisplay(notes any, dir, originalDir, persistDir string) string {
	strs := anyToStrings(notes)
	for i, s := range strs {
		strs[i] = substituteVars(s, dir, originalDir, persistDir)
	}
	return strings.Join(strs, "\n")
}

// substituteVars replaces $dir, $original_dir, and $persist_dir tokens,
// longest first, mirroring substitute in lib/core.ps1.
func substituteVars(s, dir, originalDir, persistDir string) string {
	s = strings.ReplaceAll(s, "$original_dir", originalDir)
	s = strings.ReplaceAll(s, "$persist_dir", persistDir)
	s = strings.ReplaceAll(s, "$dir", dir)
	return s
}

// envAddPathDisplay mirrors the Path Added rendering: "." means the app
// directory itself, other entries hang below it.
func envAddPathDisplay(v any, dir string) string {
	var out []string
	for _, entry := range anyToSlice(v) {
		s, ok := entry.(string)
		if !ok || s == "" {
			continue
		}
		if s == "." {
			out = append(out, dir)
		} else {
			out = append(out, dir+string(filepath.Separator)+s)
		}
	}
	return strings.Join(out, "\n")
}

// installedSize mirrors the Installed size accounting in scoop-info.ps1.
func (e *Env) installedSize(app string, global bool) string {
	appTotal := dirSize(e.AppDir(app, global))
	current := dirSize(e.CurrentDir(app, global))
	persist := dirSize(e.PersistDir(app, global))
	cached := e.cacheSize(app)
	old := appTotal - current
	if persist+cached+old == 0 {
		return filesize(current)
	}
	var lines []string
	parts := [][2]string{
		{"Current version:  ", filesize(current)},
		{"Old versions:     ", filesize(old)},
		{"Persisted data:   ", filesize(persist)},
		{"Cached downloads: ", filesize(cached)},
		{"Total:            ", filesize(appTotal + persist + cached)},
	}
	for _, p := range parts {
		if p[1] == filesize(0) {
			continue
		}
		lines = append(lines, p[0]+p[1])
	}
	return strings.Join(lines, "\n")
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}

// cacheSize sums cache files for one app prefix.
func (e *Env) cacheSize(app string) int64 {
	entries, err := os.ReadDir(e.CacheDir)
	if err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), app+"#") {
			continue
		}
		if fi, err := entry.Info(); err == nil {
			total += fi.Size()
		}
	}
	return total
}
