// Backend wiring for the mutation runners (internal/commands/mutate_backends.go).
//
// WireDefaultBackends connects the install, download, and update runners to
// the real download and extraction layers. The adapters translate the
// install.Downloader and install.Extractor seams onto internal/download and
// internal/extract without duplicating fetch or expansion logic and without
// changing backend APIs. Tests keep injecting fakes through SetMutationSeams.
// No emojis.
package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/TheSlopMachine/gscoop/internal/config"
	"github.com/TheSlopMachine/gscoop/internal/download"
	"github.com/TheSlopMachine/gscoop/internal/extract"
	"github.com/TheSlopMachine/gscoop/internal/install"
	"github.com/TheSlopMachine/gscoop/internal/ui"
)

// WireDefaultBackends wires the real download and extraction backends into
// the mutation seams. The entry point calls it once at startup.
func WireDefaultBackends(env *Env) {
	SetMutationSeams(
		&downloadAdapter{cacheDir: env.CacheDir, configPath: env.ConfigPath},
		&extractAdapter{configPath: env.ConfigPath},
	)
}

// downloadAdapter implements install.Downloader on top of
// internal/download. Manifest URLs, hashes, and cookies come from the op
// manifest payload at the op architecture; pool sizes, proxy, and tokens
// come from the config store.
type downloadAdapter struct {
	cacheDir   string
	configPath string
}

// Download fetches every manifest URL for op into dir and returns the
// url_filename leaves. Per-URL failures abort the op with the first error.
func (b *downloadAdapter) Download(ctx context.Context, op install.Op, dir string) ([]string, error) {
	store, err := config.Load(b.configPath)
	if err != nil {
		return nil, err
	}
	opts := download.OptionsFromStore(store, b.cacheDir, dir, &ui.Logger{}, nil)
	d, err := download.New(opts)
	if err != nil {
		return nil, err
	}
	urls, hashes, cookies := manifestFetch(op.ManifestRaw, op.Architecture)
	if len(urls) == 0 {
		return nil, fmt.Errorf("No download URLs for '%s'.", op.App)
	}
	results, names := d.Download(ctx, op.App, op.Version, urls, hashes, cookies)
	for _, r := range results {
		if r.Err != nil {
			return nil, r.Err
		}
	}
	return names, nil
}

// extractAdapter implements install.Extractor on top of
// internal/extract. USE_EXTERNAL_7ZIP comes from the config store; the
// installer pipeline stages files before extraction, so no removal runs
// here.
type extractAdapter struct {
	configPath string
}

// Extract expands one staged file into destDir. Non-archive payloads
// report success without extraction for the installer stage to handle.
func (b *extractAdapter) Extract(file, destDir string) error {
	useExternal := false
	if store, err := config.Load(b.configPath); err == nil {
		useExternal, _ = store.GetBool("use_external_7zip")
	}
	done, err := extract.Extract(file, destDir, extract.Options{UseExternal7ZIP: useExternal})
	if err != nil {
		return err
	}
	_ = done
	return nil
}

// fetchDoc carries the manifest download fields the adapter reads:
// top-level url/hash/cookie plus per-architecture overrides.
type fetchDoc struct {
	URL          any            `json:"url"`
	Hash         any            `json:"hash"`
	Cookie       map[string]any `json:"cookie"`
	Architecture map[string]struct {
		URL    any            `json:"url"`
		Hash   any            `json:"hash"`
		Cookie map[string]any `json:"cookie"`
	} `json:"architecture"`
}

// manifestFetch extracts download URLs, aligned hashes, and cookies from
// raw manifest bytes at arch. Architecture entries replace top-level ones
// when present, matching arch_specific selection.
func manifestFetch(raw []byte, arch string) (urls, hashes []string, cookies map[string]string) {
	var doc fetchDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil
	}
	urls, hashes = toStringList(doc.URL), toStringList(doc.Hash)
	cookies = stringCookies(doc.Cookie)
	if a, ok := doc.Architecture[arch]; ok {
		if u := toStringList(a.URL); len(u) > 0 {
			urls = u
		}
		if h := toStringList(a.Hash); len(h) > 0 {
			hashes = h
		}
		if c := stringCookies(a.Cookie); len(c) > 0 {
			cookies = c
		}
	}
	return urls, hashes, cookies
}

// stringCookies renders a manifest cookie object as name=value pairs.
func stringCookies(raw map[string]any) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = s
		} else {
			out[k] = fmt.Sprintf("%v", v)
		}
	}
	return out
}
